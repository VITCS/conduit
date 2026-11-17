package main

import (
	"strings"
	"testing"

	"conduit/internal/secrets"
	"conduit/internal/store"
)

func TestEncVarStoreEncryptsAtRest(t *testing.T) {
	raw := store.NewMem()
	mgr, _, err := secrets.NewManager(raw, "k")
	if err != nil {
		t.Fatal(err)
	}
	s := encVarStore{Store: raw, m: mgr}

	if err := s.PutVar("p", "repo", "acme/site"); err != nil {
		t.Fatal(err)
	}
	stored, _, _ := raw.GetVar("p", "repo")
	if !strings.HasPrefix(stored, "enc:v1:") || strings.Contains(stored, "acme") {
		t.Fatalf("var stored in clear: %q", stored)
	}
	if v, ok, err := s.GetVar("p", "repo"); err != nil || !ok || v != "acme/site" {
		t.Fatalf("GetVar = %q %v %v", v, ok, err)
	}

	// A value saved before encryption was enabled is still readable.
	_ = raw.PutVar("p", "owner", "legacy-plain")
	vs, err := s.ListVars("p")
	if err != nil || vs["repo"] != "acme/site" || vs["owner"] != "legacy-plain" {
		t.Fatalf("ListVars = %v %v", vs, err)
	}

	// A different key cannot read it (and it is hidden, not leaked as ciphertext).
	other, _, _ := secrets.NewManager(raw, "other")
	if vs, _ := (encVarStore{Store: raw, m: other}).ListVars("p"); vs["repo"] != "" {
		t.Fatalf("wrong key exposed a value: %v", vs)
	}
}
