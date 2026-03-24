package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// HiddenStore persists hidden session keys per agent.
// Stored in ~/.eyrie/hidden_sessions.json with mode 0600.
type HiddenStore struct {
	path string
	mu   sync.Mutex
	data map[string][]string // agentName -> []sessionKey
}

func NewHiddenStore() (*HiddenStore, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	hs := &HiddenStore{
		path: filepath.Join(dir, "hidden_sessions.json"),
		data: make(map[string][]string),
	}
	if err := hs.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return hs, nil
}

func (hs *HiddenStore) IsHidden(agentName, sessionKey string) bool {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	for _, k := range hs.data[agentName] {
		if k == sessionKey {
			return true
		}
	}
	return false
}

func (hs *HiddenStore) Hide(agentName, sessionKey string) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	for _, k := range hs.data[agentName] {
		if k == sessionKey {
			return nil
		}
	}
	hs.data[agentName] = append(hs.data[agentName], sessionKey)
	return hs.save()
}

func (hs *HiddenStore) Unhide(agentName, sessionKey string) error {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	keys := hs.data[agentName]
	for i, k := range keys {
		if k == sessionKey {
			hs.data[agentName] = append(keys[:i], keys[i+1:]...)
			return hs.save()
		}
	}
	return nil
}

func (hs *HiddenStore) load() error {
	data, err := os.ReadFile(hs.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &hs.data)
}

func (hs *HiddenStore) save() error {
	if err := os.MkdirAll(filepath.Dir(hs.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(hs.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(hs.path, data, 0600)
}
