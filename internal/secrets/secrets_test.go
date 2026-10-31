package secrets

import (
	"testing"

	"conduit/internal/store"
)

func TestSetGetRoundTrip(t *testing.T) {
	st := store.NewMem()
	m, persistent, err := NewManager(st, "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if !persistent {
		t.Error("expected persistentKey=true when a passphrase is given")
	}
	if err := m.Set("slack_url", "https://hooks.example/abc"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := m.Get("slack_url")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	if v != "https://hooks.example/abc" {
		t.Errorf("value = %q, want the stored URL", v)
	}

	// The stored bytes must be ciphertext, not the plaintext.
	raw, _, _ := st.GetSecret("slack_url")
	if string(raw) == "https://hooks.example/abc" {
		t.Error("secret stored in plaintext")
	}
}

func TestWrongKeyFailsToDecrypt(t *testing.T) {
	st := store.NewMem()
	m1, _, _ := NewManager(st, "key-one")
	if err := m1.Set("token", "s3cr3t"); err != nil {
		t.Fatal(err)
	}
	m2, _, _ := NewManager(st, "key-two") // different key, same store
	if _, _, err := m2.Get("token"); err == nil {
		t.Error("expected decrypt to fail with the wrong key")
	}
}

func TestListAndDelete(t *testing.T) {
	st := store.NewMem()
	m, _, _ := NewManager(st, "k")
	_ = m.Set("a", "1")
	_ = m.Set("b", "2")
	names, _ := m.List()
	if len(names) != 2 {
		t.Fatalf("list = %v, want 2", names)
	}
	if err := m.Delete("a"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := m.Get("a"); ok {
		t.Error("secret 'a' still present after delete")
	}
}

func TestEphemeralKeyWhenNoPassphrase(t *testing.T) {
	_, persistent, err := NewManager(store.NewMem(), "")
	if err != nil {
		t.Fatal(err)
	}
	if persistent {
		t.Error("expected persistentKey=false with no passphrase")
	}
}
