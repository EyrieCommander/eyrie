package bridge

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// Review 5, finding 1: deny matching must fold names the way APFS does.
func TestDenyFoldsLikeTheFilesystem(t *testing.T) {
	fsys, _ := NewFS(nil, nil)
	cases := map[string]bool{
		"ſecret.txt":           true,
		"id_rſa":               true,
		"SECRET.TXT":           true,
		"Id_Ed25519":           true,
		"server.\u212Aey":      true, // *.key via Kelvin sign
		"se\u0301cret.txt":     true, // decomposed é is dropped to e -> "secret"
		"pa\u00DFword-secret":  true,
		"\uFB01le.env":         true, // ﬁle.env
		"my.\u212Adbx":         true,
		"notes.txt":            false,
		"ﬁne.md":               false,
		"stra\u00DFe.md":       false,
		"r\u00E9sum\u00E9.txt": false,
	}
	for name, want := range cases {
		if got := fsys.denied(name); got != want {
			t.Errorf("denied(%q) = %v, want %v (fold %q)", name, got, want, foldName(name))
		}
	}
}

// On this machine's filesystem, every code point that opens an ASCII name
// must fold to that name. The scan covers U+0080..U+2FFFF against every
// ASCII letter, digit and the multi-letter expansions APFS performs.
func TestFoldNameCoversAPFSAliases(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("APFS alias scan only meaningful on macOS")
	}
	d := t.TempDir()
	probe := filepath.Join(d, "probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "PROBE")); err != nil {
		t.Skip("filesystem is case-sensitive; nothing to fold")
	}
	names := []string{"ss", "ff", "fi", "fl", "ffi", "ffl", "st"}
	for c := 'a'; c <= 'z'; c++ {
		names = append(names, string(c))
	}
	for c := '0'; c <= '9'; c++ {
		names = append(names, string(c))
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(d, "n-"+n), []byte(n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	misses := 0
	for r := rune(0x80); r <= 0x2FFFF; r++ {
		if (r >= 0xD800 && r <= 0xDFFF) || !utf8.ValidRune(r) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(d, "n-"+string(r)))
		if err != nil {
			continue
		}
		if got := foldName(string(r)); got != string(b) {
			misses++
			if misses < 10 {
				t.Errorf("U+%04X %q opens %q on this filesystem but folds to %q", r, string(r), b, got)
			}
		}
	}
	if misses > 0 {
		t.Fatalf("%d filesystem aliases not covered by foldName", misses)
	}
}

// Review 5, finding 2: a malformed config never puts file content in the
// error (which startBridge logs).
func TestConfigParseErrorIsRedacted(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unquoted key":   "chief_wake_key = sk-live-SUPERSECRET-123\n",
		"broken string":  "chief_wake_key = \"sk-live-SUPERSECRET-123\n",
		"duplicate key":  "chief_wake_key = \"sk-live-SUPERSECRET-123\"\nchief_wake_key = \"sk-live-SUPERSECRET-123\"\n",
		"type mismatch":  "port = \"sk-live-SUPERSECRET-123\"\n",
		"garbage line":   "sk-live-SUPERSECRET-123\n",
		"bad table":      "[sk-live-SUPERSECRET-123\n",
		"unknown in arr": "extra_deny = [sk-live-SUPERSECRET-123]\n",
		// The two shapes the TOML library quotes verbatim in its message:
		"bare word key":  "chief_wake_key = tSUPERSECRET\n",
		"secret in name": "[roots]\nSUPERSECRET-alias = 3\n",
	} {
		path := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadConfig(path)
		if err == nil {
			t.Fatalf("%s: malformed config accepted", name)
		}
		if strings.Contains(err.Error(), "SUPERSECRET") || strings.Contains(err.Error(), "sk-live") || strings.Contains(err.Error(), "tSUPER") {
			t.Fatalf("%s: error leaks file content: %v", name, err)
		}
		if !strings.Contains(err.Error(), path) {
			t.Fatalf("%s: error should name the file: %v", name, err)
		}
	}
}
