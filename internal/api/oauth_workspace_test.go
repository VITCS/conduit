package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"conduit/internal/api"
	"conduit/internal/auth"
	"conduit/internal/engine"
	"conduit/internal/model"
	"conduit/internal/secrets"
	"conduit/internal/seed"
	"conduit/internal/store"
)

// oauthEnv is a server wired to a fake Google: the token endpoint is a local test
// server and the profile is whatever the test sets in `who`.
type oauthEnv struct {
	t   *testing.T
	st  *store.Mem
	ts  *httptest.Server
	who struct{ sub, email, name string }
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	env := &oauthEnv{t: t, st: store.NewMem()}
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fake"}`)
	}))
	t.Cleanup(tokenSrv.Close)

	providers := api.NewOAuthProviders("gid", "gsecret", "", "")
	providers["google"].TokenURL = tokenSrv.URL
	providers["google"].Profile = func(string) (string, string, string, error) {
		return env.who.sub, env.who.email, env.who.name, nil
	}
	mgr, _, err := secrets.NewManager(env.st, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	authSvc := auth.New(env.st, "jwt")
	if _, err := authSvc.EnsureBootstrapAdmin("admin@conduit.local", "conduit"); err != nil {
		t.Fatal(err)
	}
	srv := &api.Server{Store: env.st, Engine: engine.New(env.st), Auth: authSvc, Secrets: mgr,
		Web: fstest.MapFS{}, AllowSignup: true, OAuth: providers}
	env.ts = httptest.NewServer(srv.Routes())
	t.Cleanup(env.ts.Close)
	return env
}

// signIn runs the whole Google flow (start → callback) and returns the JWT.
func (env *oauthEnv) signIn(sub, email, name string) string {
	t := env.t
	t.Helper()
	env.who.sub, env.who.email, env.who.name = sub, email, name
	noRedir := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := noRedir.Get(env.ts.URL + "/api/auth/oauth/google/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	state := loc.Query().Get("state")

	resp, err = noRedir.Get(env.ts.URL + "/api/auth/oauth/google/callback?code=c&state=" + url.QueryEscape(state))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	frag := resp.Header.Get("Location")
	i := strings.Index(frag, "#access_token=")
	if i < 0 {
		t.Fatalf("no token in callback redirect: %q", frag)
	}
	tok, _ := url.QueryUnescape(frag[i+len("#access_token="):])
	return tok
}

func (env *oauthEnv) do(method, path, token, project string, body any, out any) int {
	env.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, env.ts.URL+path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	if project != "" {
		req.Header.Set("X-Conduit-Project", project)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		env.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func projectIDs(ps []model.Project) map[string]bool {
	m := map[string]bool{}
	for _, p := range ps {
		m[p.ID] = true
	}
	return m
}

// A Google sign-in gets a private copy of the AI Factory + GitHub review workflows,
// keeps its own settings/secrets, and cannot see another user's.
func TestOAuthUserGetsOwnWorkflowsAndSettings(t *testing.T) {
	env := newOAuthEnv(t)
	alice := env.signIn("g-alice", "alice@example.com", "Alice")

	var ps []model.Project
	env.do("GET", "/api/projects", alice, "", nil, &ps)
	u, _, _ := env.st.GetUserByEmail("alice@example.com")
	key := seed.UserKey(u.ID)
	factoryP, githubP := seed.UserFactoryProjectID(key), seed.UserGitHubProjectID(key)
	got := projectIDs(ps)
	if len(ps) != 2 || !got[factoryP] || !got[githubP] {
		t.Fatalf("alice's projects = %v, want exactly her factory + github projects", got)
	}

	var wfs []model.Workflow
	env.do("GET", "/api/workflows", alice, factoryP, nil, &wfs)
	if len(wfs) != 1 || wfs[0].ID != seed.UserFactoryWorkflowID(key) || len(wfs[0].Nodes) != len(seed.FactoryWorkflow().Nodes) {
		t.Fatalf("factory project workflows = %+v", wfs)
	}
	env.do("GET", "/api/workflows", alice, githubP, nil, &wfs)
	if len(wfs) != 1 || wfs[0].ID != seed.UserGitHubWorkflowID(key) {
		t.Fatalf("github project workflows = %+v", wfs)
	}

	// Her settings and secrets are stored against her project.
	if c := env.do("PUT", "/api/vars/repo", alice, factoryP, map[string]string{"value": "alice-repo"}, nil); c >= 300 {
		t.Fatalf("put var = %d", c)
	}
	if c := env.do("PUT", "/api/secrets/llm_key", alice, factoryP, map[string]string{"value": "sk-alice"}, nil); c >= 300 {
		t.Fatalf("put secret = %d", c)
	}

	// Signing in again keeps everything (no duplicate projects, settings intact).
	alice = env.signIn("g-alice", "alice@example.com", "Alice")
	env.do("GET", "/api/projects", alice, "", nil, &ps)
	if len(ps) != 2 {
		t.Fatalf("after re-login alice has %d projects, want 2", len(ps))
	}
	var vars map[string]string
	env.do("GET", "/api/vars", alice, factoryP, nil, &vars)
	if vars["repo"] != "alice-repo" {
		t.Fatalf("alice's var after re-login = %v", vars)
	}

	// Another Google user gets separate projects and cannot reach Alice's.
	bob := env.signIn("g-bob", "bob@example.com", "Bob")
	env.do("GET", "/api/projects", bob, "", nil, &ps)
	if got := projectIDs(ps); got[factoryP] || got[githubP] || len(ps) != 2 {
		t.Fatalf("bob's projects = %v; must be his own two only", got)
	}
	if c := env.do("GET", "/api/vars", bob, factoryP, nil, nil); c != http.StatusForbidden {
		t.Fatalf("bob reading alice's vars = %d, want 403", c)
	}
	if c := env.do("GET", "/api/secrets", bob, factoryP, nil, nil); c != http.StatusForbidden {
		t.Fatalf("bob reading alice's secrets = %d, want 403", c)
	}
}

// The bootstrap admin is never provisioned or modified, even via Google with the
// same email.
func TestOAuthLeavesAdminUntouched(t *testing.T) {
	env := newOAuthEnv(t)
	before, _ := env.st.ListProjects()
	env.signIn("g-admin", "admin@conduit.local", "Admin")
	after, _ := env.st.ListProjects()
	if len(after) != len(before) {
		t.Fatalf("admin sign-in created projects: before %d, after %d", len(before), len(after))
	}
	for _, p := range after {
		if strings.HasPrefix(p.ID, "ws-") {
			t.Fatalf("admin got a user workspace: %s", p.ID)
		}
	}
}
