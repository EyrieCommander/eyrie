package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

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
