package bridge

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Review 6, finding 1: search applies the os.OpenRoot cross-check to every
// descendant, so a subtree that is no longer under the root (moved out
// mid-search, simulated here by pointing the guard elsewhere) is skipped
// and the result is marked truncated.
func TestSearchChecksDescendantsAgainstSecondGuard(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	write(t, filepath.Join(root, "top.md"), "needle top\n")
	write(t, filepath.Join(root, "sub", "deep.md"), "needle deep\n")
	fsys, errs := NewFS(map[string]string{"docs": root}, nil)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	defer fsys.Close()

	hits, truncated, err := fsys.Search("docs", "", "needle", 0)
	if err != nil || len(hits) != 2 || truncated {
		t.Fatalf("baseline: %+v truncated=%v err=%v", hits, truncated, err)
	}

	// After search opens sub/, move it out of the root and put a different
	// directory at that path. Search holds a descriptor to the moved
	// directory; the per-descendant os.Root check must refuse it.
	outside := filepath.Join(base, "outside")
	searchChildHook = func(rel string) {
		if rel == "sub" {
			if err := os.Rename(filepath.Join(root, "sub"), outside); err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(root, "sub", "decoy.md"), "needle decoy\n")
		}
	}
	defer func() { searchChildHook = nil }()

	hits, truncated, err = fsys.Search("docs", "", "needle", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if strings.Contains(h.Path, "deep") {
			t.Fatalf("search read through a subtree moved outside the root: %+v", hits)
		}
	}
	if !truncated {
		t.Fatalf("skipped subtree not reported: %+v", hits)
	}
}

type failingReader struct {
	data string
	done bool
}

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		return copy(p, r.data), nil
	}
	return 0, errors.New("disk on fire")
}

// Review 6, finding 2: a read failure after some content is an error, not
// a 200 with partial content.
func TestReadLinesFailureIsAnError(t *testing.T) {
	res := &ReadResult{}
	err := readLines(&failingReader{data: "one\ntwo\n"}, res, 1, 400, maxReadBytes)
	if err != errReadFailed {
		t.Fatalf("err = %v, want errReadFailed (content %q)", err, res.Content)
	}
	res = &ReadResult{}
	if err := readLines(strings.NewReader("one\ntwo\n"), res, 1, 400, maxReadBytes); err != nil || res.LinesReturned != 2 {
		t.Fatalf("clean read: %v %+v", err, res)
	}
}
