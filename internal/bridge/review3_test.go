package bridge

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// jsonStringLen must match encoding/json exactly, or the read budget is wrong.
func TestJSONStringLenMatchesEncoder(t *testing.T) {
	alphabet := []string{"a", "<", ">", "&", `"`, `\`, "\n", "\r", "\t", "\b", "\f", "\x00", "\x01", "\x1f", "\x7f", "é", "日", "😀", "\u2028", "\u2029", "\xff", "\xc3"}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 5000; i++ {
		var sb strings.Builder
		for j := rng.Intn(40); j > 0; j-- {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		s := sb.String()
		b, _ := json.Marshal(s)
		if got, want := jsonStringLen(s), len(b)-2; got != want {
			t.Fatalf("jsonStringLen(%q) = %d, encoder wrote %d", s, got, want)
		}
	}
}

// Review 3, finding 4: the read response stays within the limit after
// JSON escaping.
func TestReadResponseSizeAfterEscaping(t *testing.T) {
	f := newFixture(t)
	line := strings.Repeat("<", 100) + "\n"
	write(t, filepath.Join(f.root, "angle.txt"), strings.Repeat(line, maxReadLines))
	rec := f.do(t, "GET", "/bridge/v1/fs/read?root=docs&path=angle.txt&limit=2000", testToken, nil)
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	var r ReadResult
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	encoded, _ := json.Marshal(r.Content)
	if len(encoded) > maxReadBytes+2 {
		t.Fatalf("encoded content %d bytes, limit %d", len(encoded), maxReadBytes)
	}
	if !r.Truncated || r.LinesReturned == 0 {
		t.Fatalf("expected a truncated partial read, got %d lines truncated=%v", r.LinesReturned, r.Truncated)
	}
	if rec.Body.Len() > maxReadBytes+4096 {
		t.Fatalf("response body %d bytes", rec.Body.Len())
	}
}

// Review 3, finding 2: search covers text files over 10 MiB (the size cap
// is for /read only).
func TestSearchCoversLargeFiles(t *testing.T) {
	f := newFixture(t)
	big := strings.Repeat("filler line\n", (maxReadFileSize/12)+1000) + "bigneedle here\n"
	write(t, filepath.Join(f.root, "huge-notes.txt"), big)
	rec := f.do(t, "GET", "/bridge/v1/fs/search?root=docs&q=bigneedle", testToken, nil)
	var out struct {
		Hits      []Hit `json:"hits"`
		Truncated bool  `json:"truncated"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Hits) != 1 || out.Hits[0].Path != "huge-notes.txt" {
		t.Fatalf("hits %+v truncated=%v", out.Hits, out.Truncated)
	}
	rec = f.do(t, "GET", "/bridge/v1/fs/search?root=docs&q=huge-notes", testToken, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Hits) == 0 || out.Hits[0].Line != 0 {
		t.Fatalf("large file name not matched: %+v", out.Hits)
	}
}

// Review 3, finding 3: entries search could not inspect mark the result
// truncated instead of silently disappearing.
func TestSearchReportsUninspectableEntries(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	f := newFixture(t)
	locked := filepath.Join(f.root, "locked")
	write(t, filepath.Join(locked, "inner.txt"), "needle\n")
	unreadable := filepath.Join(f.root, "unreadable.txt")
	write(t, unreadable, "needle\n")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700); os.Chmod(unreadable, 0o600) })

	rec := f.do(t, "GET", "/bridge/v1/fs/search?root=docs&q=needle", testToken, nil)
	var out struct {
		Hits      []Hit `json:"hits"`
		Truncated bool  `json:"truncated"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.Truncated {
		t.Fatalf("unreadable entries skipped but truncated=false: %+v", out.Hits)
	}
	found := false
	for _, h := range out.Hits {
		if h.Path == "picker/doc.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("readable match missing: %+v", out.Hits)
	}
}

// Review 3, finding 1: search checks its budget while enumerating a large
// directory, not after reading and sorting all of it.
func TestSearchBudgetDuringLargeDirectory(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.root, "many")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3*searchDirBatch; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f"+strings.Repeat("0", 4)+itoa(i)+".txt"), []byte("hit\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rec := f.do(t, "GET", "/bridge/v1/fs/search?root=docs&path=many&q=hit&max=5", testToken, nil)
	var out struct {
		Hits      []Hit `json:"hits"`
		Truncated bool  `json:"truncated"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Hits) != 5 || !out.Truncated {
		t.Fatalf("max=5: %d hits truncated=%v", len(out.Hits), out.Truncated)
	}

	old := searchTimeBudget
	searchTimeBudget = -time.Second // already expired
	defer func() { searchTimeBudget = old }()
	rec = f.do(t, "GET", "/bridge/v1/fs/search?root=docs&path=many&q=hit", testToken, nil)
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if len(out.Hits) != 0 || !out.Truncated {
		t.Fatalf("expired budget: %d hits truncated=%v", len(out.Hits), out.Truncated)
	}
}

// A line longer than the per-line cap is matched on its prefix and marks
// the result truncated, without holding the whole line in memory.
func TestSearchOverlongLineMarksTruncated(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.root, "wide.txt"), "early-hit "+strings.Repeat("y", maxSearchLine+10)+"\nnext-hit\n")
	rec := f.do(t, "GET", "/bridge/v1/fs/search?root=docs&path=&q=-hit", testToken, nil)
	var out struct {
		Hits      []Hit `json:"hits"`
		Truncated bool  `json:"truncated"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	lines := map[int]bool{}
	for _, h := range out.Hits {
		if h.Path == "wide.txt" {
			lines[h.Line] = true
		}
	}
	if !lines[1] || !lines[2] || !out.Truncated {
		t.Fatalf("hits %+v truncated=%v", out.Hits, out.Truncated)
	}
}

func itoa(i int) string {
	b := []byte{}
	if i == 0 {
		return "0"
	}
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
