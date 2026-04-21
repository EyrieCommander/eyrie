package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// KeyVault centralizes API key management across all agent frameworks.
// Keys are stored in ~/.eyrie/keys.json with mode 0600. Environment variables
// take precedence over the on-disk store (e.g. ANTHROPIC_API_KEY overrides
// the "anthropic" key). Follows the TokenStore pattern exactly.
type KeyVault struct {
	path      string
	mu        sync.Mutex
	keys      map[string]string
	noPersist bool // true when initialised without storage; Set/Delete stay in-memory only
}

// WHY sync.Once singleton: Both the server (for API handlers) and discovery
// (for embedded adapters) need the vault. A singleton avoids import cycles
// and ensures a single source of truth for key lookups.
var (
	vaultOnce     sync.Once
	vaultInstance *KeyVault
)

// providerEnvMap maps provider names to their conventional environment variable
// names. Unknown providers are skipped in EnvSlice — they can't be injected
// without knowing the expected env var name.
var providerEnvMap = map[string]string{
	"anthropic":  "ANTHROPIC_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
	"openai":     "OPENAI_API_KEY",
	"deepseek":   "DEEPSEEK_API_KEY",
}

func NewKeyVault() (*KeyVault, error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	v := &KeyVault{
		path: filepath.Join(dir, "keys.json"),
		keys: make(map[string]string),
	}
	if err := v.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return v, nil
}

// GetKeyVault returns the process-wide KeyVault singleton, creating it on
// first call. Safe to call from any goroutine.
func GetKeyVault() *KeyVault {
	vaultOnce.Do(func() {
		v, err := NewKeyVault()
		if err != nil {
			// Non-fatal: log and return an empty in-memory-only vault so
			// callers can still fall back to environment variables.
			// noPersist=true prevents Set/Delete from writing to "." in the
			// current working directory (which would happen if path is empty).
			fmt.Fprintf(os.Stderr, "WARNING: failed to initialize KeyVault: %v\n", err)
			v = &KeyVault{keys: make(map[string]string), noPersist: true}
		}
		vaultInstance = v
	})
	return vaultInstance
}

// Get returns the API key for the given provider. Environment variable takes
// precedence: the vault checks the provider's mapped env var (from
// providerEnvMap, consistent with EnvSlice) before falling back to
// <PROVIDER>_API_KEY (uppercased), then the on-disk store.
func (v *KeyVault) Get(provider string) string {
	envKey, ok := providerEnvMap[provider]
	if !ok {
		envKey = strings.ToUpper(provider) + "_API_KEY"
	}
	if val := os.Getenv(envKey); val != "" {
		return val
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.keys[provider]
}

// Set stores an API key for the given provider and saves atomically.
// On save() failure the in-memory map is rolled back so it stays consistent
// with what actually made it to disk.
func (v *KeyVault) Set(provider, key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	prev, had := v.keys[provider]
	v.keys[provider] = key
	if err := v.save(); err != nil {
		if had {
			v.keys[provider] = prev
		} else {
			delete(v.keys, provider)
		}
		return err
	}
	return nil
}

// Delete removes an API key for the given provider and saves.
// On save() failure the key is restored so memory doesn't diverge from disk.
func (v *KeyVault) Delete(provider string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	prev, had := v.keys[provider]
	delete(v.keys, provider)
	if err := v.save(); err != nil {
		if had {
			v.keys[provider] = prev
		}
		return err
	}
	return nil
}

// List returns a copy of all stored keys (not env vars).
func (v *KeyVault) List() map[string]string {
	v.mu.Lock()
	defer v.mu.Unlock()
	cp := make(map[string]string, len(v.keys))
	for k, val := range v.keys {
		cp[k] = val
	}
	return cp
}

// EnvSlice builds environment variable assignments for all stored keys using
// the provider-to-envvar mapping. Unknown providers are skipped because we
// don't know which env var the framework expects.
func (v *KeyVault) EnvSlice() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []string
	for provider, key := range v.keys {
		if envName, ok := providerEnvMap[provider]; ok {
			out = append(out, envName+"="+key)
		}
	}
	return out
}

// ValidateKey probes the provider's API to check if the key is valid.
// Returns nil for unknown or local providers (ollama, custom) — they pass
// without validation. Uses a 5-second timeout.
func (v *KeyVault) ValidateKey(ctx context.Context, provider, key string) error {
	switch provider {
	case "anthropic":
		return v.probeAnthropic(ctx, key)
	case "openrouter":
		return v.probeBearer(ctx, "https://openrouter.ai/api/v1/models", key)
	case "openai":
		return v.probeBearer(ctx, "https://api.openai.com/v1/models", key)
	case "deepseek":
		return v.probeBearer(ctx, "https://api.deepseek.com/v1/models", key)
	default:
		// Unknown or local providers (ollama, custom) — skip validation
		return nil
	}
}

func (v *KeyVault) probeAnthropic(ctx context.Context, key string) error {
	// 5-second timeout for validation probes
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.anthropic.com/v1/models", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("x-api-key", key)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("invalid API key (HTTP %d)", resp.StatusCode)
	}
	return nil
}

func (v *KeyVault) probeBearer(ctx context.Context, url, key string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("invalid API key (HTTP %d)", resp.StatusCode)
	}
	return nil
}

func (v *KeyVault) load() error {
	data, err := os.ReadFile(v.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &v.keys)
}

func (v *KeyVault) save() error {
	if v.noPersist || v.path == "" {
		return fmt.Errorf("cannot save: vault initialized without storage path")
	}
	if err := os.MkdirAll(filepath.Dir(v.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v.keys, "", "  ")
	if err != nil {
		return err
	}
	// Atomic write: temp file + rename to avoid partial writes on crash
	tmp := v.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, v.path); err != nil {
		// Best-effort cleanup so a failed rename doesn't leave an
		// orphaned temp file behind.
		_ = os.Remove(tmp)
		return fmt.Errorf("renaming %s to %s: %w", tmp, v.path, err)
	}
	return nil
}
