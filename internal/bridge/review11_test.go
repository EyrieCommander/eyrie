package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Review 11: token rotation never writes the config through a
// predictable, pre-existing or symlinked temp file.
//
// The old code wrote <config>.tmp with os.WriteFile, which keeps an
// existing file's mode (so the secret sat in a 0644 file until a later
// chmod) and follows a symlink at that name. Both are planted here at the
// exact name the old code used.
func TestRotateTokenDoesNotUsePlantedTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.toml")
	if err := os.WriteFile(path, []byte("chief_wake_key = \"wake-SECRET\"\nbridge_token_sha256 = \""+HashToken("x")+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Symlink at <config>.tmp to a file the attacker can read later.
	stolen := filepath.Join(dir, "stolen.txt")
	if err := os.Symlink(stolen, path+".tmp"); err != nil {
		t.Fatal(err)
	}
	if _, err := RotateToken(path); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(stolen); err == nil {
		t.Fatalf("rotation wrote the secret config through the planted symlink (%d bytes)", len(b))
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("config mode %v err %v", fi.Mode(), err)
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "wake-SECRET") {
		t.Fatal("rotation lost chief_wake_key")
	}
	for _, e := range mustReadDir(t, dir) {
		if strings.Contains(e, ".tmp-") {
			t.Fatalf("leftover temp file %s", e)
		}
	}
}

// A pre-existing world-readable <config>.tmp is never the file the secret
// goes into (the old code reused it, keeping 0644 during the write).
func TestRotateTokenIgnoresPreexistingTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bridge.toml")
	if err := os.WriteFile(path, []byte("chief_wake_key = \"wake-SECRET\"\nbridge_token_sha256 = \""+HashToken("x")+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	planted := path + ".tmp"
	if err := os.WriteFile(planted, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Hold the planted file open, as a watching attacker would: if the
	// secret is ever written into this inode, the descriptor sees it even
	// after a rename moves the name away.
	watch, err := os.Open(planted)
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Close()
	if _, err := RotateToken(path); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4096)
	n, _ := watch.ReadAt(buf, 0)
	if strings.Contains(string(buf[:n]), "wake-SECRET") {
		t.Fatal("secret written into the pre-existing world-readable temp file")
	}
}

// If the config path itself is a symlink, rotation replaces the link with
// a regular 0600 file rather than writing through it.
func TestRotateTokenReplacesSymlinkedConfig(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere.toml")
	if err := os.WriteFile(target, []byte("port = 7201\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "bridge.toml")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := RotateToken(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("config is %v (want regular 0600)", fi.Mode())
	}
	if b, _ := os.ReadFile(target); strings.Contains(string(b), "bridge_token_sha256") {
		t.Fatal("rotation wrote through the symlink")
	}
}

func mustReadDir(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}
