package embedded

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// KeyStore reads API keys from ~/.eyrie/keys.json and environment variables.
// Environment variables take precedence over the JSON file. Keys are looked
// up by provider name (e.g. "openrouter", "anthropic", "openai").
type KeyStore struct {
	mu   sync.RWMutex
	keys map[string]string
}

// NewKeyStore creates a KeyStore, loading any existing keys from
// ~/.eyrie/keys.json. Missing or unreadable files are silently ignored.
func NewKeyStore() *KeyStore {
	ks := &KeyStore{keys: make(map[string]string)}
	ks.load()
	return ks
}

// Get returns the API key for the given provider. Environment variable
// takes precedence: the store checks <PROVIDER>_API_KEY (uppercased)
// before falling back to the on-disk store.
func (ks *KeyStore) Get(provider string) string {
	// Env var takes precedence: OPENROUTER_API_KEY, ANTHROPIC_API_KEY, etc.
	envKey := strings.ToUpper(provider) + "_API_KEY"
	if val := os.Getenv(envKey); val != "" {
		return val
	}

	ks.mu.RLock()
	defer ks.mu.RUnlock()
	return ks.keys[provider]
}

// load reads the on-disk key file. Called once at construction.
func (ks *KeyStore) load() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	data, err := os.ReadFile(filepath.Join(home, ".eyrie", "keys.json"))
	if err != nil {
		return
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	_ = json.Unmarshal(data, &ks.keys)
}
