package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

// The os.OpenRoot second guard rejects an open whose file is not the one
// os.Root resolves at that path (here: the root's os.Root points somewhere
// else entirely, as if the primary walk had been fooled).
func TestOSRootSecondGuardFailsClosed(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "a")
	b := filepath.Join(base, "b")
	write(t, filepath.Join(a, "doc.md"), "from a\n")
	write(t, filepath.Join(b, "doc.md"), "from b\n")
	fsys, errs := NewFS(map[string]string{"docs": a}, nil)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	defer fsys.Close()
	if _, err := fsys.Read("docs", "doc.md", 1, 10); err != nil {
		t.Fatalf("baseline read: %v", err)
	}
	other, err := os.OpenRoot(b)
	if err != nil {
		t.Fatal(err)
	}
	fsys.osRoots["docs"].Close()
	fsys.osRoots["docs"] = other
	if _, err := fsys.Read("docs", "doc.md", 1, 10); err != errDenied {
		t.Fatalf("guard let a mismatched file through: %v", err)
	}
	if _, _, err := fsys.List("docs", ""); err != errDenied {
		t.Fatalf("guard let a mismatched dir through: %v", err)
	}
}
