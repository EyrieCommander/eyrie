package persona

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultCatalogURL points to the local persona catalog for now
	DefaultCatalogURL = "file:///Users/natalie/Development/eyrie/personas.example.json"
)

// CatalogClient fetches the curated persona catalog.
type CatalogClient struct {
	catalogURL string
	cacheDir   string
	httpClient *http.Client
}

func NewCatalogClient(catalogURL string) (*CatalogClient, error) {
	if catalogURL == "" {
		catalogURL = DefaultCatalogURL
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
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
		path := strings.TrimPrefix(c.catalogURL, "file://")
		data, err := os.ReadFile(path)
		if err != nil {
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
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
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
