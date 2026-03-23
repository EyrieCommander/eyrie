package persona

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxPersonaCatalogSize = 5 * 1024 * 1024 // 5 MB


// DefaultCatalogURL returns the persona catalog URL, checking the
// EYRIE_CATALOG_URL environment variable first and falling back to
// a bundled file path relative to the user's home directory.
func DefaultCatalogURL() string {
	if env := os.Getenv("EYRIE_CATALOG_URL"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "file://./personas.example.json"
	}
	// Source path is distinct from cache path (~/.eyrie/cache/personas.json)
	p := filepath.Join(home, ".eyrie", "personas.json")
	u := url.URL{Scheme: "file", Path: p}
	return u.String()
}

// CatalogClient fetches the curated persona catalog.
type CatalogClient struct {
	catalogURL string
	cacheDir   string
	httpClient *http.Client
}

func NewCatalogClient(catalogURL string) (*CatalogClient, error) {
	if catalogURL == "" {
		catalogURL = DefaultCatalogURL()
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "."
	}
	cacheDir := filepath.Join(home, ".eyrie", "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, err
	}
	return &CatalogClient{
		catalogURL: catalogURL,
		cacheDir:   cacheDir,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// Fetch retrieves the persona catalog.
func (c *CatalogClient) Fetch(ctx context.Context) (*PersonaRegistry, error) {
	// Try cache first
	if reg, err := c.loadCache(); err == nil {
		return reg, nil
	}

	if strings.HasPrefix(c.catalogURL, "file://") {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("read local persona catalog: %w", err)
		}
		u, parseErr := url.Parse(c.catalogURL)
		if parseErr != nil {
			return nil, fmt.Errorf("parse catalog URL: %w", parseErr)
		}
		localPath := filepath.FromSlash(u.Path)
		data, err := os.ReadFile(localPath)
		if err != nil {
			return nil, fmt.Errorf("read local persona catalog: %w", err)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("read local persona catalog: %w", err)
		}
		var reg PersonaRegistry
		if err := json.Unmarshal(data, &reg); err != nil {
			return nil, fmt.Errorf("parse persona catalog: %w", err)
		}
		_ = c.saveCache(&reg)
		return &reg, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.catalogURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch persona catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("persona catalog returned status %d", resp.StatusCode)
	}

	var reg PersonaRegistry
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPersonaCatalogSize)).Decode(&reg); err != nil {
		return nil, fmt.Errorf("parse persona catalog: %w", err)
	}
	_ = c.saveCache(&reg)
	return &reg, nil
}

func (c *CatalogClient) loadCache() (*PersonaRegistry, error) {
	path := filepath.Join(c.cacheDir, "personas.json")
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if time.Since(stat.ModTime()) > 24*time.Hour {
		return nil, fmt.Errorf("cache expired")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reg PersonaRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	return &reg, nil
}

func (c *CatalogClient) saveCache(reg *PersonaRegistry) error {
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.cacheDir, "personas.json"), data, 0o644)
}
