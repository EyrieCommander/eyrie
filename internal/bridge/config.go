// Package bridge is Eyrie's public-facing bridge listener: a separate
// http.Server (never the management API's mux) that lets the chief reply
// to prompts and read allowlisted folders, read-only.
//
// The bridge is off by default. It starts only when the secret config file
// exists, holds a bridge token hash, and is not group/world readable.
package bridge

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultPort is the bridge's loopback port when the config sets none.
const DefaultPort = 7201

// Config is the bridge's local secret config. Real values come from Dan;
// the repo only ships docs/bridge.example.toml with placeholders.
type Config struct {
	// Port the bridge binds on 127.0.0.1. Funnel/tunnel targets this port only.
	Port int `toml:"port"`
	// ChiefWakeURL / ChiefWakeKey: where Eyrie POSTs prompts for the chief.
	ChiefWakeURL string `toml:"chief_wake_url,omitempty"`
	ChiefWakeKey string `toml:"chief_wake_key,omitempty"`
	// BridgeTokenSHA256 is the hex SHA-256 of the bridge bearer token.
	// The token itself is never stored.
	BridgeTokenSHA256 string `toml:"bridge_token_sha256"`
	// Roots maps alias -> absolute folder path. Empty by default.
	Roots map[string]string `toml:"roots,omitempty"`
	// ExtraDeny adds glob patterns (matched case-insensitively against each
	// path component) to the hard-coded deny list. It can never remove one.
	ExtraDeny []string `toml:"extra_deny,omitempty"`
	// ClientIPHeader, when set (e.g. "X-Forwarded-For", "Cf-Connecting-Ip"),
	// is used for the client address of requests that arrive from loopback,
	// i.e. through Funnel or the tunnel. Ignored for non-loopback peers.
	ClientIPHeader string `toml:"client_ip_header,omitempty"`
	// AccessLog path; default ~/.eyrie/logs/bridge-access.jsonl.
	AccessLog string `toml:"access_log,omitempty"`
}

var hexSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var aliasRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ErrNotConfigured means no bridge config file exists: the bridge stays off.
var ErrNotConfigured = errors.New("bridge not configured")

// DefaultConfigPath is ~/.eyrie/bridge.toml, overridable by EYRIE_BRIDGE_CONFIG.
func DefaultConfigPath() (string, error) {
	if p := os.Getenv("EYRIE_BRIDGE_CONFIG"); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".eyrie", "bridge.toml"), nil
}

// CheckPerms refuses a secret file that is group or world accessible.
func CheckPerms(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is mode %04o; the bridge refuses to start unless it is 0600 (chmod 600 %s)", path, fi.Mode().Perm(), path)
	}
	return nil
}

// ReadConfig parses the file without validating it (used by token rotate).
func ReadConfig(path string) (Config, error) {
	var c Config
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return c, ErrNotConfigured
	}
	if err := CheckPerms(path); err != nil {
		return c, err
	}
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return c, redactParseError(path, err)
	}
	return c, nil
}

// knownKeys are the config keys whose names are safe to show in an error.
var knownKeys = map[string]bool{
	"port": true, "chief_wake_url": true, "chief_wake_key": true, "bridge_token_sha256": true,
	"roots": true, "extra_deny": true, "client_ip_header": true, "access_log": true,
}

// redactParseError turns a TOML decode error into a diagnostic that never
// contains file content. The TOML library's messages quote the offending
// value (an unquoted wake key would land in the log), so only the line and
// column, and the key name when it is one of ours, are kept.
func redactParseError(path string, err error) error {
	var pe toml.ParseError
	if errors.As(err, &pe) {
		where := fmt.Sprintf("line %d", pe.Position.Line)
		if pe.Position.Col > 0 {
			where += fmt.Sprintf(", column %d", pe.Position.Col)
		}
		if knownKeys[pe.LastKey] {
			where += fmt.Sprintf(" (key %s)", pe.LastKey)
		}
		return fmt.Errorf("parse %s: invalid TOML at %s (details withheld because they can contain secret values)", path, where)
	}
	// Type errors (e.g. a string where a number belongs) also quote values.
	return fmt.Errorf("parse %s: invalid bridge config (details withheld because they can contain secret values)", path)
}

// LoadConfig reads and validates the bridge config. ErrNotConfigured when
// the file is absent; any other error means the bridge must not start.
func LoadConfig(path string) (Config, error) {
	c, err := ReadConfig(path)
	if err != nil {
		return c, err
	}
	return c, c.Validate()
}

// Validate checks the config is safe to serve with.
func (c *Config) Validate() error {
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("bridge port %d out of range", c.Port)
	}
	c.BridgeTokenSHA256 = strings.ToLower(strings.TrimSpace(c.BridgeTokenSHA256))
	if !hexSHA256.MatchString(c.BridgeTokenSHA256) {
		return errors.New("bridge_token_sha256 missing or not 64 hex chars (run: eyrie bridge token rotate)")
	}
	for alias, p := range c.Roots {
		if !aliasRE.MatchString(alias) {
			return fmt.Errorf("root alias %q must match %s", alias, aliasRE)
		}
		if !filepath.IsAbs(p) {
			return fmt.Errorf("root %q: path must be absolute", alias)
		}
	}
	for _, g := range c.ExtraDeny {
		if _, err := filepath.Match(strings.ToLower(g), "x"); err != nil {
			return fmt.Errorf("extra_deny pattern %q: %w", g, err)
		}
	}
	return nil
}

// WakeConfigured reports whether prompts can be sent to the chief.
func (c Config) WakeConfigured() bool {
	return c.ChiefWakeURL != "" && c.ChiefWakeKey != ""
}
