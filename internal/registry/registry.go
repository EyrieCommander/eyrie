package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// CacheTTL defines how long cached registry data is valid
	CacheTTL = 24 * time.Hour
)

// defaultRegistryURL returns the registry URL, resolving ~ to the user's home directory.
// Looks for ~/.eyrie/registry.json first, then falls back to ~/.eyrie/cache/registry.json.
// TODO: Update this to the actual hosted registry URL for production.
func defaultRegistryURL() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	// Prefer user-provided registry, fall back to cached copy
	for _, rel := range []string{
		filepath.Join(".eyrie", "registry.json"),
		filepath.Join(".eyrie", "cache", "registry.json"),
	} {
		p := filepath.Join(home, rel)
		if _, err := os.Stat(p); err == nil {
			slashed := filepath.ToSlash(p)
			if len(slashed) > 0 && slashed[0] != '/' {
				slashed = "/" + slashed
			}
			return (&url.URL{Scheme: "file", Path: slashed}).String()
		}
	}
	return ""
}

// Client fetches and caches the Claw frameworks registry
type Client struct {
	registryURL string
	cacheDir    string
	httpClient  *http.Client
}

// NewClient creates a new registry client
func NewClient(registryURL string) (*Client, error) {
	if registryURL == "" {
		registryURL = defaultRegistryURL()
	}
	if registryURL == "" {
		return nil, fmt.Errorf("no registry found: place registry.json in ~/.eyrie/ or set a registry URL")
	}

	cacheDir, err := getCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	return &Client{
		registryURL: registryURL,
		cacheDir:    cacheDir,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Fetch retrieves the registry, using cache if available and not expired
func (c *Client) Fetch(ctx context.Context, forceRefresh bool) (*Registry, error) {
	// Try cache first unless force refresh
	if !forceRefresh {
		if reg, err := c.loadCache(); err == nil {
			return reg, nil
		}
	}

	// Handle file:// URLs for local registries
	if strings.HasPrefix(c.registryURL, "file://") {
		u, parseErr := url.Parse(c.registryURL)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid registry URL: %w", parseErr)
		}
		localPath := u.Path
		// On Windows, strip leading slash from /C:/... paths
		if len(localPath) >= 3 && localPath[0] == '/' && localPath[2] == ':' {
			localPath = localPath[1:]
		}
		localPath = filepath.FromSlash(localPath)
		data, err := os.ReadFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read local registry: %w", err)
		}

		var reg Registry
		if err := json.Unmarshal(data, &reg); err != nil {
			return nil, fmt.Errorf("failed to parse registry: %w", err)
		}

		// Cache it
		if err := c.saveCache(&reg); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to cache registry: %v\n", err)
		}

		return &reg, nil
	}

	// Fetch from remote
	req, err := http.NewRequestWithContext(ctx, "GET", c.registryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d", resp.StatusCode)
	}

	var reg Registry
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return nil, fmt.Errorf("failed to parse registry: %w", err)
	}

	// Cache it
	if err := c.saveCache(&reg); err != nil {
		// Log warning but don't fail
		fmt.Fprintf(os.Stderr, "Warning: failed to cache registry: %v\n", err)
	}

	return &reg, nil
}

// GetFramework retrieves a specific framework by ID
func (c *Client) GetFramework(ctx context.Context, id string) (*Framework, error) {
	reg, err := c.Fetch(ctx, false)
	if err != nil {
		return nil, err
	}

	for i := range reg.Frameworks {
		if reg.Frameworks[i].ID == id {
			return &reg.Frameworks[i], nil
		}
	}

	return nil, fmt.Errorf("framework %q not found in registry", id)
}

// ListFrameworks returns all available frameworks.
// When forceRefresh is true the cache is bypassed.
func (c *Client) ListFrameworks(ctx context.Context, forceRefresh bool) ([]Framework, error) {
	reg, err := c.Fetch(ctx, forceRefresh)
	if err != nil {
		return nil, err
	}
	return reg.Frameworks, nil
}

// getCacheDir returns the cache directory path, creating it if needed
func getCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cacheDir := filepath.Join(home, ".eyrie", "cache")
	return cacheDir, os.MkdirAll(cacheDir, 0755)
}

// loadCache loads registry from local cache if valid
func (c *Client) loadCache() (*Registry, error) {
	path := filepath.Join(c.cacheDir, "registry.json")

	// Check if file exists and is not expired
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if time.Since(stat.ModTime()) > CacheTTL {
		return nil, fmt.Errorf("cache expired")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var reg Registry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}

	return &reg, nil
}

// saveCache persists registry to local cache
func (c *Client) saveCache(reg *Registry) error {
	path := filepath.Join(c.cacheDir, "registry.json")

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}
