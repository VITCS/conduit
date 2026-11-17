package main

import (
	"conduit/internal/secrets"
	"conduit/internal/store"
)

// encVarStore encrypts project variables (the "settings" a user saves: repo,
// owner, model, Jira base URL, …) at rest with the same key as secrets. Every
// other store method passes straight through. Values stored before encryption
// was enabled are still readable and are re-encrypted the next time they are saved.
type encVarStore struct {
	store.Store
	m *secrets.Manager
}

func (s encVarStore) PutVar(projectID, name, value string) error {
	sealed, err := s.m.SealString(value)
	if err != nil {
		return err
	}
	return s.Store.PutVar(projectID, name, sealed)
}

func (s encVarStore) GetVar(projectID, name string) (string, bool, error) {
	v, ok, err := s.Store.GetVar(projectID, name)
	if err != nil || !ok {
		return v, ok, err
	}
	pt, err := s.m.OpenString(v)
	if err != nil {
		return "", false, err
	}
	return pt, true, nil
}

func (s encVarStore) ListVars(projectID string) (map[string]string, error) {
	vs, err := s.Store.ListVars(projectID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(vs))
	for k, v := range vs {
		pt, err := s.m.OpenString(v)
		if err != nil {
			continue // unreadable with this key: hide it rather than leak ciphertext
		}
		out[k] = pt
	}
	return out, nil
}
