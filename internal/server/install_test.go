package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Audacity88/eyrie/internal/registry"
)

func TestSetupAdapterSupportsCodexAppServer(t *testing.T) {
	progress := &installProgress{}
	err := setupAdapter(&registry.Framework{AdapterType: "app-server"}, progress)
	if err != nil {
		t.Fatalf("setupAdapter app-server error = %v", err)
	}
}

func TestScaffoldConfigCreatesSchemaDefaultConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	fw := &registry.Framework{
		ConfigFormat: "json",
		ConfigPath:   configPath,
		ConfigSchema: &registry.ConfigSchema{CommonFields: []registry.ConfigField{
			{Key: "binary_path", Default: "codex"},
			{Key: "model", Default: "gpt-5.4"},
		}},
	}

	progress := &installProgress{}
	if err := scaffoldConfig(context.Background(), fw, "", progress); err != nil {
		t.Fatalf("scaffoldConfig error = %v", err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["binary_path"] != "codex" || got["model"] != "gpt-5.4" {
		t.Fatalf("unexpected config defaults: %#v", got)
	}
}

// fakeVersionBinary writes a shell script that counts its invocations in
// counterPath and prints version. Extra shell is inserted before printing.
func fakeVersionBinary(t *testing.T, dir, version, extra string) (binaryPath, counterPath string) {
	t.Helper()
	counterPath = filepath.Join(dir, "counter")
	binaryPath = filepath.Join(dir, "fake-framework")
	script := fmt.Sprintf(`#!/bin/sh
count=0
if [ -f %[1]q ]; then
  count=$(cat %[1]q)
fi
count=$((count + 1))
printf '%%s\n' "$count" > %[1]q
%[3]s
printf '%[2]s\n'
`, counterPath, version, extra)
	if err := os.WriteFile(binaryPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binaryPath, counterPath
}

func probeCount(t *testing.T, counterPath string) string {
	t.Helper()
	data, err := os.ReadFile(counterPath)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}

// withProbeTimeout sets the probe bound for one test. The cache tests use a
// generous bound so they check caching, not how fast the machine schedules
// a shell under load (the old 3s bound made this test fail during
// concurrent race builds: the first probe timed out and returned "").
func withProbeTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	old := frameworkVersionProbeTimeout
	frameworkVersionProbeTimeout = d
	t.Cleanup(func() { frameworkVersionProbeTimeout = old })
}

func TestFrameworkVersionCachesBinaryProbe(t *testing.T) {
	resetFrameworkVersionCacheForTest()
	t.Cleanup(resetFrameworkVersionCacheForTest)
	withProbeTimeout(t, time.Minute)

	binaryPath, counterPath := fakeVersionBinary(t, t.TempDir(), "fake-framework 1.2.3", "")
	fw := registry.Framework{ID: "fake-framework", BinaryPath: binaryPath}
	if got := frameworkVersion(fw); got != "fake-framework 1.2.3" {
		t.Fatalf("first frameworkVersion = %q", got)
	}
	if got := frameworkVersion(fw); got != "fake-framework 1.2.3" {
		t.Fatalf("second frameworkVersion = %q", got)
	}
	if got := probeCount(t, counterPath); got != "1" {
		t.Fatalf("binary probe count = %s, want 1", got)
	}
}

// Replacing the binary (new size/mtime) invalidates the cached version.
func TestFrameworkVersionCacheInvalidatesOnBinaryChange(t *testing.T) {
	resetFrameworkVersionCacheForTest()
	t.Cleanup(resetFrameworkVersionCacheForTest)
	withProbeTimeout(t, time.Minute)

	dir := t.TempDir()
	binaryPath, counterPath := fakeVersionBinary(t, dir, "fake-framework 1.2.3", "")
	fw := registry.Framework{ID: "fake-framework", BinaryPath: binaryPath}
	if got := frameworkVersion(fw); got != "fake-framework 1.2.3" {
		t.Fatalf("first = %q", got)
	}
	fakeVersionBinary(t, dir, "fake-framework 1.3.0-upgraded", "")
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(binaryPath, future, future); err != nil {
		t.Fatal(err)
	}
	if got := frameworkVersion(fw); got != "fake-framework 1.3.0-upgraded" {
		t.Fatalf("after upgrade = %q, want the new version", got)
	}
	if got := probeCount(t, counterPath); got != "2" {
		t.Fatalf("probe count = %s, want 2", got)
	}
}

// The production bound still holds: a binary that never answers returns ""
// promptly (here it also leaves a background child holding the output
// pipe), and the failure is cached rather than re-probed on every request.
func TestFrameworkVersionProbeIsBounded(t *testing.T) {
	resetFrameworkVersionCacheForTest()
	t.Cleanup(resetFrameworkVersionCacheForTest)
	withProbeTimeout(t, 300*time.Millisecond)

	// A background child keeps stdout open after the probe is killed; exec
	// makes the killed process the foreground sleeper.
	// The child's pid is recorded so the test leaves nothing running.
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	})
	binaryPath, counterPath := fakeVersionBinary(t, dir, "never", fmt.Sprintf("sleep 30 &\necho $! > %q\nexec sleep 30", pidFile))
	fw := registry.Framework{ID: "fake-framework", BinaryPath: binaryPath}
	start := time.Now()
	if got := frameworkVersion(fw); got != "" {
		t.Fatalf("hung binary version = %q, want empty", got)
	}
	if el := time.Since(start); el > 3*time.Second {
		t.Fatalf("probe took %v with a 300ms bound", el)
	}
	// The failure is cached: the second call must not run (and wait out)
	// another probe. Timing is used because under load the script may be
	// killed before it records anything.
	start = time.Now()
	if got := frameworkVersion(fw); got != "" {
		t.Fatalf("second = %q", got)
	}
	if el := time.Since(start); el >= frameworkVersionProbeTimeout {
		t.Fatalf("second call took %v: failed probe was re-run instead of cached", el)
	}
	// The probe can be killed before the script's first line runs under
	// load, so count >= 1 is not guaranteed; what matters is that the
	// second call didn't re-probe. Counter content, if any, is "1".
	if b, err := os.ReadFile(counterPath); err == nil && strings.TrimSpace(string(b)) != "1" {
		t.Fatalf("failed probe re-run (count %s), want cached after 1", strings.TrimSpace(string(b)))
	}
}

// Production keeps a 3s bound; tests only override it locally.
func TestFrameworkVersionProbeTimeoutDefault(t *testing.T) {
	if frameworkVersionProbeTimeout != 3*time.Second {
		t.Fatalf("probe timeout = %v, want 3s", frameworkVersionProbeTimeout)
	}
}
