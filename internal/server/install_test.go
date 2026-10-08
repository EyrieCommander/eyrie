package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
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
// countProbes wraps runVersionProbe to count invocations in-process, so
// cache assertions don't depend on the fake script getting scheduled.
func countProbes(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	orig := runVersionProbe
	runVersionProbe = func(ctx context.Context, path string) ([]byte, error) {
		n.Add(1)
		return orig(ctx, path)
	}
	t.Cleanup(func() { runVersionProbe = orig })
	return &n
}

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

	probes := countProbes(t)
	binaryPath, counterPath := fakeVersionBinary(t, t.TempDir(), "fake-framework 1.2.3", "")
	fw := registry.Framework{ID: "fake-framework", BinaryPath: binaryPath}
	if got := frameworkVersion(fw); got != "fake-framework 1.2.3" {
		t.Fatalf("first frameworkVersion = %q", got)
	}
	if got := frameworkVersion(fw); got != "fake-framework 1.2.3" {
		t.Fatalf("second frameworkVersion = %q", got)
	}
	if got := probes.Load(); got != 1 {
		t.Fatalf("probes run = %d, want 1", got)
	}
	if got := probeCount(t, counterPath); got != "1" {
		t.Fatalf("binary ran %s times, want 1", got)
	}
}

// Replacing the binary (new size/mtime) invalidates the cached version.
func TestFrameworkVersionCacheInvalidatesOnBinaryChange(t *testing.T) {
	resetFrameworkVersionCacheForTest()
	t.Cleanup(resetFrameworkVersionCacheForTest)
	withProbeTimeout(t, time.Minute)

	probes := countProbes(t)
	dir := t.TempDir()
	binaryPath, _ := fakeVersionBinary(t, dir, "fake-framework 1.2.3", "")
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
	if got := probes.Load(); got != 2 {
		t.Fatalf("probes run = %d, want 2", got)
	}
}

// The production bound still holds even when the binary leaves a child
// holding its output pipe: the call returns "" within timeout+WaitDelay
// instead of waiting for the child. The fake signals (via a ready file) that
// the child is running before it blocks, and the probe timeout is long
// enough that this happens before the kill, so the held-pipe case is
// actually exercised. The child is stopped by the timeout itself (cleanup
// is only a safety net). The failure is cached: a second call runs no probe.
func TestFrameworkVersionProbeIsBounded(t *testing.T) {
	resetFrameworkVersionCacheForTest()
	t.Cleanup(resetFrameworkVersionCacheForTest)
	const bound = 2 * time.Second
	withProbeTimeout(t, bound)
	probes := countProbes(t)

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
	// The background child inherits stdout; exec makes the foreground
	// process (the one CommandContext kills) a long sleep.
	binaryPath, _ := fakeVersionBinary(t, dir, "never",
		fmt.Sprintf("sleep 60 &\necho $! > %[1]q.tmp && mv %[1]q.tmp %[1]q\nexec sleep 60", pidFile))
	fw := registry.Framework{ID: "fake-framework", BinaryPath: binaryPath}

	start := time.Now()
	got := frameworkVersion(fw)
	elapsed := time.Since(start)
	if got != "" {
		t.Fatalf("hung binary version = %q, want empty", got)
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Skip("fake binary was killed before its child started (machine too loaded); held-pipe case not exercised")
	}
	if limit := bound + frameworkVersionWaitDelay + 5*time.Second; elapsed > limit {
		t.Fatalf("probe took %v; bound is %v + %v WaitDelay (child kept stdout open)", elapsed, bound, frameworkVersionWaitDelay)
	}
	// The probe's child is stopped on timeout, not left for t.Cleanup.
	{
		b, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for syscall.Kill(pid, 0) == nil {
			if time.Now().After(deadline) {
				t.Fatalf("probe child %d still running after the probe timed out", pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	if got := frameworkVersion(fw); got != "" {
		t.Fatalf("second = %q", got)
	}
	if n := probes.Load(); n != 1 {
		t.Fatalf("probes run = %d, want 1 (failure should be cached)", n)
	}
}

// A child that leaves the probe's process group (setsid, as daemons do)
// survives the group kill and still holds stdout. WaitDelay is what keeps
// the call bounded then. The escaped child is killed in cleanup.
func TestFrameworkVersionProbeBoundedWhenChildEscapesGroup(t *testing.T) {
	perl, err := exec.LookPath("perl")
	if err != nil {
		t.Skip("perl not available to setsid a child")
	}
	resetFrameworkVersionCacheForTest()
	t.Cleanup(resetFrameworkVersionCacheForTest)
	const bound = 2 * time.Second
	withProbeTimeout(t, bound)

	dir := t.TempDir()
	pidFile := filepath.Join(dir, "child.pid")
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	escape := fmt.Sprintf(`%[1]q -MPOSIX -e 'POSIX::setsid(); exec "sleep", "60"' &
echo $! > %[2]q.tmp && mv %[2]q.tmp %[2]q
exec sleep 60`, perl, pidFile)
	binaryPath, _ := fakeVersionBinary(t, dir, "never", escape)
	fw := registry.Framework{ID: "fake-framework", BinaryPath: binaryPath}

	start := time.Now()
	if got := frameworkVersion(fw); got != "" {
		t.Fatalf("version = %q, want empty", got)
	}
	elapsed := time.Since(start)
	if _, err := os.Stat(pidFile); err != nil {
		t.Skip("fake binary was killed before its child started (machine too loaded)")
	}
	if limit := bound + frameworkVersionWaitDelay + 5*time.Second; elapsed > limit {
		t.Fatalf("probe took %v; an escaped child holding stdout must not extend it past %v + WaitDelay", elapsed, bound)
	}
}

// Production keeps a 3s bound; tests only override it locally.
func TestFrameworkVersionProbeTimeoutDefault(t *testing.T) {
	if frameworkVersionProbeTimeout != 3*time.Second {
		t.Fatalf("probe timeout = %v, want 3s", frameworkVersionProbeTimeout)
	}
}
