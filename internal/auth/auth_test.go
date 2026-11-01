package auth

import (
	"testing"
	"time"

	"conduit/internal/model"
	"conduit/internal/store"
)

func newSvc(t *testing.T) *Service {
	t.Helper()
	return New(store.NewMem(), "test-secret")
}

func TestCreateAndAuthenticate(t *testing.T) {
	s := newSvc(t)
	if _, err := s.CreateUser("Alice@Example.com", "Alice", "s3cret", model.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	// Email is normalized to lowercase.
	u, err := s.Authenticate("alice@example.com", "s3cret")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if u.Role != model.RoleAdmin || u.PasswordHash == "s3cret" {
		t.Errorf("unexpected user: role=%s hashStoredPlain=%v", u.Role, u.PasswordHash == "s3cret")
	}
	if _, err := s.Authenticate("alice@example.com", "wrong"); err != ErrInvalidCredentials {
		t.Errorf("wrong password err = %v, want ErrInvalidCredentials", err)
	}
	if _, err := s.Authenticate("nobody@example.com", "x"); err != ErrInvalidCredentials {
		t.Errorf("unknown user err = %v, want ErrInvalidCredentials", err)
	}
}

func TestDuplicateEmailRejected(t *testing.T) {
	s := newSvc(t)
	if _, err := s.CreateUser("a@b.com", "A", "pw", model.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser("a@b.com", "A2", "pw2", model.RoleMember); err == nil {
		t.Error("expected duplicate email to be rejected")
	}
}

func TestJWTRoundTripAndTamper(t *testing.T) {
	s := newSvc(t)
	u, _ := s.CreateUser("u@e.com", "U", "pw", model.RoleMember)
	tok, err := s.IssueJWT(u)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.ParseJWT(tok)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.Sub != u.ID || c.Role != model.RoleMember {
		t.Errorf("claims mismatch: %+v", c)
	}
	// Tamper with the payload → signature check fails.
	if _, err := s.ParseJWT(tok + "x"); err == nil {
		t.Error("tampered token accepted")
	}
	// A token signed with a different secret is rejected.
	other := New(store.NewMem(), "different")
	if _, err := other.ParseJWT(tok); err == nil {
		t.Error("token verified under the wrong secret")
	}
}

func TestBootstrapAdminOnce(t *testing.T) {
	st := store.NewMem()
	s := New(st, "k")
	created, err := s.EnsureBootstrapAdmin("admin@conduit.local", "pw")
	if err != nil || !created {
		t.Fatalf("first bootstrap: created=%v err=%v", created, err)
	}
	created2, err := s.EnsureBootstrapAdmin("admin@conduit.local", "pw")
	if err != nil || created2 {
		t.Errorf("second bootstrap should be a no-op: created=%v err=%v", created2, err)
	}
}

func TestAPITokenLifecycle(t *testing.T) {
	s := newSvc(t)
	plain, meta, err := s.CreateAPIToken("ci", model.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if plain == "" || meta.Hash == plain {
		t.Error("token should be returned plaintext and stored hashed")
	}
	got, ok := s.LookupAPIToken(plain)
	if !ok || got.Role != model.RoleMember {
		t.Errorf("lookup: ok=%v role=%s", ok, got.Role)
	}
	if _, ok := s.LookupAPIToken("cdt_bogus"); ok {
		t.Error("bogus token resolved")
	}
	if err := s.DeleteAPIToken(meta.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LookupAPIToken(plain); ok {
		t.Error("token still valid after delete")
	}
}

func TestRoleCanWrite(t *testing.T) {
	for _, r := range []model.Role{model.RoleOwner, model.RoleAdmin, model.RoleMember} {
		if !r.CanWrite() {
			t.Errorf("%s should be able to write", r)
		}
	}
	if model.RoleViewer.CanWrite() {
		t.Error("viewer should be read-only")
	}
}

func TestOAuthStateRoundTrip(t *testing.T) {
	st := store.NewMem()
	svc := New(st, "state-secret")

	state := svc.SignState("google", time.Minute)
	if p, ok := svc.VerifyState(state); !ok || p != "google" {
		t.Fatalf("round-trip failed: provider=%q ok=%v", p, ok)
	}
	// Tampered state is rejected.
	if _, ok := svc.VerifyState(state + "x"); ok {
		t.Error("tampered state verified")
	}
	// A state from a different secret is rejected.
	other := New(st, "different").SignState("google", time.Minute)
	if _, ok := svc.VerifyState(other); ok {
		t.Error("state signed with a different secret verified")
	}
	// Expired state is rejected.
	expired := svc.SignState("github", -time.Second)
	if _, ok := svc.VerifyState(expired); ok {
		t.Error("expired state verified")
	}
}

func TestCreateOAuthUserHasNoUsablePassword(t *testing.T) {
	st := store.NewMem()
	svc := New(st, "s")
	u, err := svc.CreateOAuthUser("dev@example.com", "Dev", model.RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "dev@example.com" || u.Role != model.RoleMember {
		t.Fatalf("unexpected user %+v", u)
	}
	// The random password must not authenticate.
	if _, err := svc.Authenticate("dev@example.com", ""); err == nil {
		t.Error("empty password authenticated an oauth user")
	}
}
