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
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"conduit/internal/api"
	"conduit/internal/engine"
	"conduit/internal/seed"
	"conduit/internal/store"
)

// The Next.js app is statically exported to web/out at build time and embedded
// here, so the server ships as a single binary. The all: prefix is required to
// include Next's _next/ asset directory (names beginning with _ are otherwise
// skipped by go:embed).
//
//go:embed all:web/out
var webFS embed.FS

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	addr := envOr("CONDUIT_ADDR", ":8080")

	st, closeStore, err := openStore()
	if err != nil {
		return err
	}
	defer closeStore()

	// Seed the default workflow if it is not already stored.
	if _, ok, _ := st.GetWorkflow(seed.Workflow().ID); !ok {
		if err := st.PutWorkflow(seed.Workflow()); err != nil {
			return err
		}
	}

	ui, err := fs.Sub(webFS, "web/out")
	if err != nil {
		return err
	}

	srv := &api.Server{Store: st, Engine: engine.New(st), Web: ui}

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
		log.Printf("Conduit listening on http://localhost%s", addr)
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
