package bridge

import (
	"bufio"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// slowReader yields an endless line slowly, like a huge file on a slow disk.
type slowReader struct{ n int }

func (r *slowReader) Read(p []byte) (int, error) {
	time.Sleep(time.Millisecond)
	for i := range p {
		p[i] = 'z'
	}
	r.n += len(p)
	return len(p), nil
}

// Review 4, finding 1: draining one enormous line honours the deadline.
func TestReadLineCappedHonoursDeadline(t *testing.T) {
	br := bufio.NewReaderSize(&slowReader{}, 4096)
	start := time.Now()
	_, long, timedOut, err := readLineCapped(br, 1024, time.Now().Add(100*time.Millisecond))
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if !timedOut || !long {
		t.Fatalf("timedOut=%v long=%v", timedOut, long)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("took %v past a 100ms deadline", el)
	}
}

// And end to end: a search over a long line stops on budget, truncated.
func TestSearchLongLineStopsOnBudget(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.root, "wide2.txt"), strings.Repeat("q", 40<<20)+"\n")
	old := searchTimeBudget
	searchTimeBudget = 5 * time.Millisecond
	defer func() { searchTimeBudget = old }()
	start := time.Now()
	rec := f.do(t, "GET", "/bridge/v1/fs/search?root=docs&q=nomatch", testToken, nil)
	var out struct {
		Truncated bool `json:"truncated"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if !out.Truncated {
		t.Fatal("search over budget not marked truncated")
	}
	if el := time.Since(start); el > time.Second {
		t.Fatalf("search took %v with a 5ms budget", el)
	}
}

// Review 4, finding 4: one maximal plain line keeps the whole response
// within maxReadBytes.
func TestReadWholeResponseWithinCap(t *testing.T) {
	f := newFixture(t)
	write(t, filepath.Join(f.root, "plain.txt"), strings.Repeat("p", maxReadBytes-1)+"\n"+"second\n")
	for _, q := range []string{"limit=2000", "limit=1", ""} {
		rec := f.do(t, "GET", "/bridge/v1/fs/read?root=docs&path=plain.txt&"+q, testToken, nil)
		if rec.Code != 200 {
			t.Fatal(rec.Code)
		}
		if rec.Body.Len() > maxReadBytes {
			t.Fatalf("%s: response %d bytes, cap %d", q, rec.Body.Len(), maxReadBytes)
		}
	}
	// A small file still reads in full.
	rec := f.do(t, "GET", "/bridge/v1/fs/read?root=docs&path=notes.txt", testToken, nil)
	var r ReadResult
	_ = json.Unmarshal(rec.Body.Bytes(), &r)
	if r.Truncated || r.LinesReturned != 2 {
		t.Fatalf("small read: %+v", r)
	}
}
