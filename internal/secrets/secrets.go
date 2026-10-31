// Package secrets manages encrypted credentials. Values are encrypted with
// AES-256-GCM above the store, so the store only ever persists ciphertext.
//
// The encryption key is derived (SHA-256) from CONDUIT_SECRET_KEY. If that is
// unset, an ephemeral key is generated and a warning is logged — fine for local
// development, but secrets won't survive a restart and can't be shared across
// instances, so set the key in production.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// Store is the subset of the persistence layer the manager needs. Secrets are
// scoped to a project.
type Store interface {
	PutSecret(projectID, name string, ciphertext []byte) error
	GetSecret(projectID, name string) ([]byte, bool, error)
	ListSecretNames(projectID string) ([]string, error)
	DeleteSecret(projectID, name string) error
}

// Manager encrypts/decrypts secret values and persists them via a Store.
type Manager struct {
	store Store
	aead  cipher.AEAD
}

// NewManager builds a manager. If passphrase is empty, a random ephemeral key is
// used (ok is returned false to signal the caller should warn).
func NewManager(store Store, passphrase string) (m *Manager, persistentKey bool, err error) {
	var key [32]byte
	if passphrase != "" {
		key = sha256.Sum256([]byte(passphrase))
		persistentKey = true
	} else {
		if _, err := io.ReadFull(rand.Reader, key[:]); err != nil {
			return nil, false, err
		}
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, false, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, false, err
	}
	return &Manager{store: store, aead: aead}, persistentKey, nil
}

// Set encrypts and stores a secret value in a project.
func (m *Manager) Set(projectID, name, value string) error {
	if name == "" {
		return errors.New("secret name is required")
	}
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	// Prepend the nonce to the ciphertext.
	ct := m.aead.Seal(nonce, nonce, []byte(value), nil)
	return m.store.PutSecret(projectID, name, ct)
}

// Get decrypts and returns a secret value from a project.
func (m *Manager) Get(projectID, name string) (string, bool, error) {
	ct, ok, err := m.store.GetSecret(projectID, name)
	if err != nil || !ok {
		return "", ok, err
	}
	ns := m.aead.NonceSize()
	if len(ct) < ns {
		return "", false, fmt.Errorf("secret %q: ciphertext too short", name)
	}
	nonce, body := ct[:ns], ct[ns:]
	pt, err := m.aead.Open(nil, nonce, body, nil)
	if err != nil {
		return "", false, fmt.Errorf("secret %q: decrypt failed (wrong key?): %w", name, err)
	}
	return string(pt), true, nil
}

// Lookup is Get without the error, for use as an executor secret resolver.
func (m *Manager) Lookup(projectID, name string) (string, bool) {
	v, ok, err := m.Get(projectID, name)
	if err != nil {
		return "", false
	}
	return v, ok
}

// List returns the secret names in a project (never values).
func (m *Manager) List(projectID string) ([]string, error) { return m.store.ListSecretNames(projectID) }

// Delete removes a secret from a project.
func (m *Manager) Delete(projectID, name string) error { return m.store.DeleteSecret(projectID, name) }
