// Command conduitd is the Conduit server: a workflow control-plane engine with a
// REST + SSE API and an embedded single-page UI (Next.js static export).
//
// Configuration (environment):
//
//	CONDUIT_ADDR          listen address (default ":8080")
//	CONDUIT_DATABASE_URL  Postgres DSN; if unset, state is in-memory
//
// It ships as a single binary: the UI is embedded, so no Node runtime is needed.
package main

import (
	"context"
	"embed"
	"errors"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"conduit/internal/api"
	"conduit/internal/auth"
	"conduit/internal/components"
	"conduit/internal/engine"
	"conduit/internal/model"
	"conduit/internal/scheduler"
	"conduit/internal/secrets"
	"conduit/internal/seed"
	"conduit/internal/store"
	"conduit/internal/worker"
)

// The Next.js app is statically exported to web/out at build time and embedded
// here, so the server ships as a single binary. The all: prefix is required to
// include Next's _next/ asset directory (names beginning with _ are otherwise
// skipped by go:embed).
//
//go:embed all:web/out
var webFS embed.FS

// The interactive Technical Design Document is embedded and served at /docs so it
// is viewable from a running instance without a repo checkout.
//
//go:embed docs/tdd.html
var tddHTML []byte

func main() {
	// `conduitd healthcheck` performs an HTTP self-check and exits 0/1. Used by
	// the container healthcheck since the distroless image has no shell or curl.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func healthcheck() int {
	addr := envOr("CONDUIT_ADDR", ":8080")
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/api/health")
	if err != nil {
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return 0
	}
	return 1
}

func run() error {
	// Structured JSON logging for the whole process.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	addr := envOr("CONDUIT_ADDR", ":8080")

	st, closeStore, err := openStore()
	if err != nil {
		return err
	}
	defer closeStore()

	// Ensure the projects that hold the built-in workflows exist.
	if _, ok, _ := st.GetProject(model.DefaultProjectID); !ok {
		if err := st.CreateProject(model.Project{ID: model.DefaultProjectID, Name: "Default", CreatedAt: time.Now()}); err != nil {
			return err
		}
	}
	if _, ok, _ := st.GetProject(seed.FactoryProjectID); !ok {
		_ = st.CreateProject(model.Project{ID: seed.FactoryProjectID, Name: "AI Factory", CreatedAt: time.Now()})
	}
	if _, ok, _ := st.GetProject(seed.GitHubProjectID); !ok {
		_ = st.CreateProject(model.Project{ID: seed.GitHubProjectID, Name: "GitHub", CreatedAt: time.Now()})
	}
	// Install or upgrade the built-in workflows. Unlike a plain "seed if absent",
	// this refreshes an out-of-date built-in (e.g. an older, simulated AI Factory)
	// to the current shipped definition — but never overwrites one the user edited.
	if err := seed.SyncBuiltins(st, nil); err != nil {
		return err
	}

	ui, err := fs.Sub(webFS, "web/out")
	if err != nil {
		return err
	}

	eng := engine.New(st)

	// Secrets: encrypted credential store; wire the resolver so components can
	// expand ${secret:NAME} references in their config.
	secretMgr, persistentKey, err := secrets.NewManager(st, os.Getenv("CONDUIT_SECRET_KEY"))
	if err != nil {
		return err
	}
	if !persistentKey {
		log.Printf("secrets: CONDUIT_SECRET_KEY not set — using an ephemeral key (secrets won't survive restart)")
	}
	components.ResolveSecret = secretMgr.Lookup
	// ${var:NAME} resolves to project config variables (non-secret, set once per project).
	components.ResolveVar = func(projectID, name string) (string, bool) {
		v, ok, err := st.GetVar(projectID, name)
		if err != nil {
			return "", false
		}
		return v, ok
	}

	// SSRF guard: outbound executors (http/slack/agent) may reach only public
	// addresses unless explicitly allowed (e.g. local testing / trusted network).
	if strings.EqualFold(os.Getenv("CONDUIT_ALLOW_PRIVATE_EGRESS"), "true") {
		components.AllowPrivateEgress = true
		log.Printf("egress: private/loopback egress ALLOWED (CONDUIT_ALLOW_PRIVATE_EGRESS=true)")
	}

	// Authentication: password login (JWT) + API tokens, with RBAC.
	authSvc := auth.New(st, os.Getenv("CONDUIT_JWT_SECRET"))
	if os.Getenv("CONDUIT_JWT_SECRET") == "" {
		log.Printf("auth: CONDUIT_JWT_SECRET not set — using an ephemeral key (sessions reset on restart)")
	}
	adminEmail := envOr("CONDUIT_ADMIN_EMAIL", "admin@conduit.local")
	adminPass := envOr("CONDUIT_ADMIN_PASSWORD", "conduit")
	if created, err := authSvc.EnsureBootstrapAdmin(adminEmail, adminPass); err != nil {
		return err
	} else if created {
		log.Printf("auth: bootstrapped admin %q (change the password after first login)", adminEmail)
		// Make the admin an owner of the default project (admins override anyway).
		if u, ok, _ := st.GetUserByEmail(adminEmail); ok {
			_ = st.AddProjectMember(model.ProjectMember{ProjectID: model.DefaultProjectID, UserID: u.ID, Role: model.RoleOwner, CreatedAt: time.Now()})
		}
	}

	// Background context tied to the process lifetime (scheduler + event relay).
	bgCtx, bgCancel := context.WithCancel(context.Background())
	defer bgCancel()

	// Multi-instance live events: with Postgres, fan events out via LISTEN/NOTIFY.
	if pg, ok := st.(*store.Pg); ok {
		pg.StartEventRelay(bgCtx)
		log.Printf("events: Postgres LISTEN/NOTIFY relay enabled (multi-instance)")
	}

	// Start the worker pool that drains the run queue.
	workers := 4
	if v := os.Getenv("CONDUIT_WORKERS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			workers = n
		}
	}
	worker.New(st, eng, workers).Start(bgCtx)

	// Start the cron scheduler.
	sched := scheduler.New(st, eng)
	sched.Start(bgCtx)

	// Rate limiting (per client IP). CONDUIT_RATE_LIMIT is requests/sec; 0 disables.
	rate := 20.0
	if v := os.Getenv("CONDUIT_RATE_LIMIT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			rate = f
		}
	}
	var limiter = api.NewRateLimiter(rate, int(rate*2)+10)
	if rate <= 0 {
		limiter = nil
	}

	// Self-serve signup is on by default so the landing page's "create account"
	// works out of the box; set CONDUIT_ALLOW_SIGNUP=false to lock it down.
	allowSignup := !strings.EqualFold(os.Getenv("CONDUIT_ALLOW_SIGNUP"), "false")

	// External sign-in (optional). Providers appear in the UI only when their
	// client id + secret are set. Redirect URIs are {BASE_URL}/api/auth/oauth/<p>/callback.
	oauthProviders := api.NewOAuthProviders(
		os.Getenv("CONDUIT_GOOGLE_CLIENT_ID"), os.Getenv("CONDUIT_GOOGLE_CLIENT_SECRET"),
		os.Getenv("CONDUIT_GITHUB_CLIENT_ID"), os.Getenv("CONDUIT_GITHUB_CLIENT_SECRET"),
	)
	for name := range oauthProviders {
		log.Printf("auth: %s sign-in enabled", name)
	}

	srv := &api.Server{
		Store: st, Engine: eng, Scheduler: sched, Secrets: secretMgr, Auth: authSvc,
		Limiter: limiter, Web: ui, Docs: tddHTML, AllowSignup: allowSignup,
		OAuth: oauthProviders, BaseURL: os.Getenv("CONDUIT_BASE_URL"),
	}

	httpSrv := &http.Server{
		Addr:    addr,
		Handler: srv.Routes(),
		// No WriteTimeout: the SSE event stream is long-lived. ReadHeaderTimeout
		// guards against slow-header (slowloris) clients.
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Run the server; report a startup failure through errCh.
	errCh := make(chan error, 1)
	go func() {
		log.Printf("Conduit Agent listening on http://localhost%s", addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Wait for a shutdown signal or a server error.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errCh:
		return err
	case sig := <-stop:
		log.Printf("received %s, shutting down", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		return httpSrv.Close()
	}
	log.Printf("shutdown complete")
	return nil
}

// openStore selects Postgres when CONDUIT_DATABASE_URL is set, otherwise an
// in-memory store. The returned close func releases any resources.
func openStore() (store.Store, func(), error) {
	if dsn := os.Getenv("CONDUIT_DATABASE_URL"); dsn != "" {
		pg, err := store.NewPostgres(dsn)
		if err != nil {
			return nil, func() {}, err
		}
		log.Printf("persistence: postgres")
		return pg, func() { _ = pg.Close() }, nil
	}
	log.Printf("persistence: in-memory (set CONDUIT_DATABASE_URL for Postgres)")
	return store.NewMem(), func() {}, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
