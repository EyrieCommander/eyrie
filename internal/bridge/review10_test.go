package bridge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// Review 10, finding 1: list keeps a bounded result set while streaming a
// large directory, and still returns the first 1,000 names in sorted order.
func TestListLargeDirectoryBoundedAndSorted(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, "big")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const n = 5000
	names := make([]string, 0, n)
	for i := 0; i < n; i++ {
		// Long names make an unbounded read measurably larger.
		name := fmt.Sprintf("%05d-%0200d.txt", (i*7919)%n, i)
		names = append(names, name)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(names)

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	entries, truncated, err := f.srv.fs.List("docs", "big")
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(entries) != maxListEntries {
		t.Fatalf("got %d entries truncated=%v", len(entries), truncated)
	}
	for i, e := range entries {
		if e.Name != names[i] {
			t.Fatalf("entry %d = %s, want %s (not the first %d in sorted order)", i, e.Name, names[i], maxListEntries)
		}
	}
	// Heap growth is logged for inspection only (GC timing makes it too
	// noisy to assert on); TestListReadsInBatches is the guard.
	t.Logf("heap in use grew by %d bytes", int64(after.HeapInuse)-int64(before.HeapInuse))

	// And the HTTP response is the same list.
	rec := f.do(t, "GET", "/bridge/v1/fs/list?root=docs&path=big", testToken, nil)
	var out struct {
		Entries   []Entry `json:"entries"`
		Truncated bool    `json:"truncated"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Entries) != maxListEntries || !out.Truncated || out.Entries[0].Name != names[0] {
		t.Fatalf("http list: %d entries truncated=%v", len(out.Entries), out.Truncated)
	}
}

// The listing reads the directory in batches: no single ReadDir call may
// ask for the whole directory. Verified via the batch-size hook.
func TestListReadsInBatches(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, "many2")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3*searchDirBatch; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var calls []int
	listReadDirHook = func(n int) { calls = append(calls, n) }
	defer func() { listReadDirHook = nil }()
	if _, _, err := f.srv.fs.List("docs", "many2"); err != nil {
		t.Fatal(err)
	}
	if len(calls) < 3 {
		t.Fatalf("ReadDir called %d times for %d entries; want batched reads", len(calls), 3*searchDirBatch)
	}
	for _, n := range calls {
		if n <= 0 || n > searchDirBatch {
			t.Fatalf("ReadDir(%d): unbounded or oversized batch", n)
		}
	}
}
