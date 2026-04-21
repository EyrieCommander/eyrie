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
		return "file://./personas.json"
	}
	// Source path is distinct from cache path (~/.eyrie/cache/personas.json)
	p := filepath.Join(home, ".eyrie", "personas.json")
	slashed := filepath.ToSlash(p)
	if len(slashed) > 0 && slashed[0] != '/' {
		slashed = "/" + slashed
	}
	u := url.URL{Scheme: "file", Path: slashed}
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
	var cacheDir string
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cacheDir = filepath.Join(home, ".eyrie", "cache")
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			cacheDir = "" // caching disabled
		}
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
		localPath := u.Path
		if u.Host != "" {
			localPath = u.Host + localPath
		}
		// On Windows, strip leading slash from /C:/... paths
		if len(localPath) >= 3 && localPath[0] == '/' && localPath[2] == ':' {
			localPath = localPath[1:]
		}
		localPath = filepath.FromSlash(localPath)
		f, err := os.Open(localPath)
		if err != nil {
			return nil, fmt.Errorf("read local persona catalog: %w", err)
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return nil, fmt.Errorf("read local persona catalog: %w", err)
		}
		if fi.Size() > maxPersonaCatalogSize {
			return nil, fmt.Errorf("local persona catalog exceeds %d byte limit", maxPersonaCatalogSize)
		}
		data, err := io.ReadAll(io.LimitReader(f, maxPersonaCatalogSize))
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

	// Read body with a limit; read one extra byte to detect oversized responses
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPersonaCatalogSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading persona catalog: %w", err)
	}
	if int64(len(data)) > maxPersonaCatalogSize {
		return nil, fmt.Errorf("persona catalog response too large (%d bytes, max %d)", len(data), maxPersonaCatalogSize)
	}
	var reg PersonaRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse persona catalog: %w", err)
	}
	_ = c.saveCache(&reg)
	return &reg, nil
}

func (c *CatalogClient) loadCache() (*PersonaRegistry, error) {
	if c.cacheDir == "" {
		return nil, fmt.Errorf("caching disabled")
	}
	path := filepath.Join(c.cacheDir, "personas.json")
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if time.Since(stat.ModTime()) > 24*time.Hour {
		return nil, fmt.Errorf("cache expired")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxPersonaCatalogSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxPersonaCatalogSize {
		return nil, fmt.Errorf("cached persona catalog exceeds %d byte limit", maxPersonaCatalogSize)
	}
	var reg PersonaRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	return &reg, nil
}

func (c *CatalogClient) saveCache(reg *PersonaRegistry) error {
	if c.cacheDir == "" {
		return nil // caching disabled
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.cacheDir, "personas.json"), data, 0o644)
}
