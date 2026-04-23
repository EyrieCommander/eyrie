package config

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// WriteTOMLAtomic writes TOML config atomically using temp file + rename
func WriteTOMLAtomic(path string, data interface{}) error {
	// Get absolute path and directory
	absPath, err := filepath.Abs(ExpandHome(path))
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	dir := filepath.Dir(absPath)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temp file in same directory for atomic rename
	tempFile, err := os.CreateTemp(dir, ".eyrie-config-*.toml.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Ensure temp file is cleaned up on error
	defer func() {
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	// Encode TOML to temp file
	encoder := toml.NewEncoder(tempFile)
	if err := encoder.Encode(data); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to encode TOML: %w", err)
	}

	// Close temp file before rename
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, absPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// WriteJSONAtomic writes JSON config atomically using temp file + rename
func WriteJSONAtomic(path string, data interface{}) error {
	// Get absolute path and directory
	absPath, err := filepath.Abs(ExpandHome(path))
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	dir := filepath.Dir(absPath)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temp file in same directory for atomic rename
	tempFile, err := os.CreateTemp(dir, ".eyrie-config-*.json.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Ensure temp file is cleaned up on error
	defer func() {
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	// Encode JSON to temp file with pretty printing
	encoder := json.NewEncoder(tempFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to encode JSON: %w", err)
	}

	// Close temp file before rename
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, absPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// WriteYAMLAtomic writes YAML config atomically using temp file + rename
func WriteYAMLAtomic(path string, data interface{}) error {
	// Get absolute path and directory
	absPath, err := filepath.Abs(ExpandHome(path))
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	dir := filepath.Dir(absPath)

	// Ensure directory exists
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create temp file in same directory for atomic rename
	tempFile, err := os.CreateTemp(dir, ".eyrie-config-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()

	// Ensure temp file is cleaned up on error
	defer func() {
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	// Encode YAML to temp file
	encoder := yaml.NewEncoder(tempFile)
	encoder.SetIndent(2)
	if err := encoder.Encode(data); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to encode YAML: %w", err)
	}

	// Close encoder and temp file before rename
	if err := encoder.Close(); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, absPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// WriteRawAtomic writes a raw string to a file atomically.
// Used when the config is already in its target format (e.g., raw TOML text
// from the editor) and should be preserved as-is without re-encoding.
func WriteRawAtomic(path string, content string) error {
	absPath, err := filepath.Abs(ExpandHome(path))
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	tempFile, err := os.CreateTemp(dir, ".eyrie-config-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		if err != nil {
			os.Remove(tempPath)
		}
	}()

	if _, err = tempFile.WriteString(content); err != nil {
		tempFile.Close()
		return fmt.Errorf("failed to write config: %w", err)
	}
	if err = tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err = os.Rename(tempPath, absPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}
	return nil
}

// CoerceJSONNumbers recursively walks a value decoded from JSON and converts
// float64 values that are whole numbers (e.g., 42617.0) to int64 (42617).
// This prevents the TOML encoder from writing "port = 42617.0" when the
// original config had "port = 42617".
func CoerceJSONNumbers(v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, child := range val {
			if f, ok := child.(float64); ok && f == float64(int64(f)) && f <= float64(math.MaxInt64) && f >= float64(math.MinInt64) {
				val[k] = int64(f)
			} else {
				CoerceJSONNumbers(child)
			}
		}
	case []interface{}:
		for i, child := range val {
			if f, ok := child.(float64); ok && f == float64(int64(f)) && f <= float64(math.MaxInt64) && f >= float64(math.MinInt64) {
				val[i] = int64(f)
			} else {
				CoerceJSONNumbers(child)
			}
		}
	}
}

// ValidateRawFormat checks that a raw config string is syntactically valid
// for the given format (toml, json, yaml). Returns nil on success.
func ValidateRawFormat(format, content string) error {
	switch format {
	case "toml":
		var v any
		if _, err := toml.Decode(content, &v); err != nil {
			return err
		}
	case "json":
		if !json.Valid([]byte(content)) {
			return fmt.Errorf("invalid JSON syntax")
		}
	case "yaml", "yml":
		var v any
		if err := yaml.Unmarshal([]byte(content), &v); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported config format: %s", format)
	}
	return nil
}

// WriteConfigAtomic writes config file atomically based on format
func WriteConfigAtomic(path string, format string, data interface{}) error {
	switch format {
	case "toml":
		return WriteTOMLAtomic(path, data)
	case "json":
		return WriteJSONAtomic(path, data)
	case "yaml", "yml":
		return WriteYAMLAtomic(path, data)
	default:
		return fmt.Errorf("unsupported config format: %s", format)
	}
}

// SetNestedValue sets a value at a dot-separated path in a nested map,
// creating intermediate maps as needed. Returns false if a non-map node
// blocked the path (e.g., "a.b" where "a" is a string, not a map).
func SetNestedValue(m map[string]any, path string, value any) bool {
	parts := strings.Split(path, ".")
	current := m
	for i := 0; i < len(parts)-1; i++ {
		existing, exists := current[parts[i]]
		if !exists {
			next := make(map[string]any)
			current[parts[i]] = next
			current = next
			continue
		}
		next, ok := existing.(map[string]any)
		if !ok {
			return false
		}
		current = next
	}
	key := parts[len(parts)-1]
	current[key] = value
	return true
}

// PatchConfigFile reads a config file, patches the given dot-path fields,
// and writes back atomically. Creates the file (with parent dirs) if it
// doesn't exist. format is "toml", "json", or "yaml".
func PatchConfigFile(path string, format string, fields map[string]any) error {
	absPath := ExpandHome(path)

	// Read existing config or start with empty map
	cfg := make(map[string]any)
	if data, err := os.ReadFile(absPath); err == nil && len(data) > 0 {
		switch format {
		case "toml":
			if _, err := toml.Decode(string(data), &cfg); err != nil {
				return fmt.Errorf("failed to parse existing TOML: %w", err)
			}
		case "json":
			if err := json.Unmarshal(data, &cfg); err != nil {
				return fmt.Errorf("failed to parse existing JSON: %w", err)
			}
		case "yaml", "yml":
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return fmt.Errorf("failed to parse existing YAML: %w", err)
			}
		default:
			return fmt.Errorf("unsupported format: %s", format)
		}
	}

	// Patch fields
	for key, val := range fields {
		SetNestedValue(cfg, key, val)
	}

	// Ensure parent directory exists
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return WriteConfigAtomic(absPath, format, cfg)
}
