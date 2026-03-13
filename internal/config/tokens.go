package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// TokenStore persists bearer tokens keyed by agent name.
// Tokens are stored in ~/.eyrie/tokens.json with mode 0600.
type TokenStore struct {
	path   string
	mu     sync.Mutex
	tokens map[string]string
}

func NewTokenStore() (*TokenStore, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	ts := &TokenStore{
		path:   filepath.Join(dir, "tokens.json"),
		tokens: make(map[string]string),
	}
	if err := ts.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return ts, nil
}

func (ts *TokenStore) Get(agentName string) string {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.tokens[agentName]
}

func (ts *TokenStore) Set(agentName, token string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.tokens[agentName] = token
	return ts.save()
}

func (ts *TokenStore) load() error {
	data, err := os.ReadFile(ts.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &ts.tokens)
}

func (ts *TokenStore) save() error {
	if err := os.MkdirAll(filepath.Dir(ts.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ts.tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ts.path, data, 0600)
}
