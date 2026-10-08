package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Review 7, finding 2: a file that ends exactly at the sniff window with a
// partial rune is binary (invalid UTF-8), not text. One byte more and the
// partial rune is forgiven, because more of the file follows.
func TestSniffEOFAtWindowBoundary(t *testing.T) {
	f := newFixture(t)
	exact := []byte(strings.Repeat("a", sniffBytes-1) + "\xc3")
	if err := os.WriteFile(filepath.Join(f.root, "edge.txt"), exact, 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := f.do(t, "GET", "/bridge/v1/fs/read?root=docs&path=edge.txt", testToken, nil); rec.Code != 415 {
		t.Fatalf("file ending in a partial rune at exactly %d bytes: %d, want 415", sniffBytes, rec.Code)
	}
	ok := []byte(strings.Repeat("a", sniffBytes-1) + "\xc3\xa9\n") // é straddles the window
	if err := os.WriteFile(filepath.Join(f.root, "straddle.txt"), ok, 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := f.do(t, "GET", "/bridge/v1/fs/read?root=docs&path=straddle.txt", testToken, nil); rec.Code != 200 {
		t.Fatalf("valid rune straddling the window: %d, want 200", rec.Code)
	}
}
