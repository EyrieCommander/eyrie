package bridge

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxReadFileSize  = 10 << 20  // files over this return 413
	maxReadBytes     = 256 << 10 // response content cap
	defaultReadLines = 400
	maxReadLines     = 2000
	maxListEntries   = 1000
	maxSearchHits    = 200
	maxSnippetRunes  = 200
	searchFileBudget = 20000
	sniffBytes       = 8 << 10
)

// searchTimeBudget is a var so tests can shrink it.
var searchTimeBudget = 5 * time.Second

// baseDeny is hard coded. Config can only add to it (ExtraDeny).
// Matched case-insensitively against every path component.
var baseDeny = []string{
	".*",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.jks", "*.keystore", "*.kdbx",
	"id_rsa*", "id_ed25519*", "id_ecdsa*",
	"*.env",
	"*secret*", "*credential*",
}

// fsError carries the HTTP status for a refused fs operation.
type fsError struct {
	status int
	code   string
}

func (e *fsError) Error() string { return e.code }

var (
	errUnknownRoot = &fsError{http.StatusNotFound, "not_found"}
	errNotFound    = &fsError{http.StatusNotFound, "not_found"}
	errBadPath     = &fsError{http.StatusBadRequest, "bad_path"}
	errDenied      = &fsError{http.StatusForbidden, "denied"}
	errBinary      = &fsError{http.StatusUnsupportedMediaType, "binary"}
	errTooLarge    = &fsError{http.StatusRequestEntityTooLarge, "too_large"}
	errNotDir      = &fsError{http.StatusBadRequest, "not_a_directory"}
	errNotFile     = &fsError{http.StatusBadRequest, "not_a_file"}
)

// FS is the read-only, allowlisted view of Dan's folders.
type FS struct {
	roots map[string]string // alias -> symlink-resolved absolute path
	deny  []string
}

// NewFS resolves each root once. A root that does not exist or is not a
// directory is skipped (so it 404s) and reported, rather than failing the
// whole bridge.
func NewFS(roots map[string]string, extraDeny []string) (*FS, []error) {
	f := &FS{roots: map[string]string{}}
	f.deny = append(f.deny, baseDeny...)
	for _, g := range extraDeny {
		f.deny = append(f.deny, strings.ToLower(g))
	}
	var errs []error
	for alias, p := range roots {
		real, err := filepath.EvalSymlinks(filepath.Clean(p))
		if err == nil {
			var fi os.FileInfo
			if fi, err = os.Stat(real); err == nil && !fi.IsDir() {
				err = errors.New("not a directory")
			}
		}
		if err != nil {
			errs = append(errs, &rootError{alias, err})
			continue
		}
		f.roots[alias] = real
	}
	return f, errs
}

type rootError struct {
	alias string
	err   error
}

func (e *rootError) Error() string { return "root " + e.alias + ": " + e.err.Error() }

// Aliases returns the configured root aliases, sorted. Never paths.
func (f *FS) Aliases() []string {
	out := make([]string, 0, len(f.roots))
	for a := range f.roots {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func (f *FS) denied(name string) bool {
	n := strings.ToLower(name)
	for _, pat := range f.deny {
		if ok, _ := filepath.Match(pat, n); ok {
			return true
		}
	}
	return false
}

// cleanRel validates a client path and returns it slash-separated and
// cleaned ("." for the root). It never touches the filesystem.
func cleanRel(p string) (string, error) {
	if strings.IndexByte(p, 0) >= 0 {
		return "", errBadPath
	}
	if p == "" || p == "." {
		return ".", nil
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return "", errBadPath
	}
	// Backslashes are refused outright: they are not separators on the mini,
	// but a name containing one is never something Dan needs us to read.
	if strings.ContainsRune(p, '\\') {
		return "", errBadPath
	}
	// Reject any ".." component before cleaning, so "a/../b" is refused
	// rather than quietly normalised.
	for _, c := range strings.Split(p, "/") {
		if c == ".." {
			return "", errBadPath
		}
	}
	c := filepath.ToSlash(filepath.Clean(p))
	if c == ".." || strings.HasPrefix(c, "../") || strings.HasPrefix(c, "/") {
		return "", errBadPath
	}
	return c, nil
}

// resolve checks alias, path shape, the deny list on every component, and
// symlinks (any symlink component is refused in slice 1), then confirms the
// symlink-resolved result sits at or under the root.
func (f *FS) resolve(alias, p string) (root, rel string, err error) {
	root, ok := f.roots[alias]
	if !ok {
		return "", "", errUnknownRoot
	}
	rel, err = cleanRel(p)
	if err != nil {
		return "", "", err
	}
	if rel == "." {
		return root, rel, nil
	}
	cur := root
	for _, c := range strings.Split(rel, "/") {
		if f.denied(c) {
			return "", "", errDenied
		}
		cur = filepath.Join(cur, c)
		fi, err := os.Lstat(cur)
		if err != nil {
			return "", "", errNotFound
		}
		if fi.Mode()&fs.ModeSymlink != 0 {
			return "", "", errDenied
		}
	}
	real, err := filepath.EvalSymlinks(cur)
	if err != nil {
		return "", "", errNotFound
	}
	if real != root && !strings.HasPrefix(real, root+string(filepath.Separator)) {
		return "", "", errDenied
	}
	return root, rel, nil
}

// openUnder opens rel through os.Root as a second guard against escape.
func openUnder(root, rel string) (*os.File, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Open(rel)
}

// Entry is one list result.
type Entry struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	Mtime string `json:"mtime"`
}

// List returns visible entries of a directory, sorted, capped.
func (f *FS) List(alias, p string) ([]Entry, bool, error) {
	root, rel, err := f.resolve(alias, p)
	if err != nil {
		return nil, false, err
	}
	d, err := openUnder(root, rel)
	if err != nil {
		return nil, false, errNotFound
	}
	defer d.Close()
	fi, err := d.Stat()
	if err != nil {
		return nil, false, errNotFound
	}
	if !fi.IsDir() {
		return nil, false, errNotDir
	}
	des, err := d.ReadDir(-1)
	if err != nil {
		return nil, false, errNotFound
	}
	sort.Slice(des, func(i, j int) bool { return des[i].Name() < des[j].Name() })
	out := []Entry{}
	truncated := false
	for _, de := range des {
		if f.denied(de.Name()) {
			continue
		}
		t := de.Type()
		var typ string
		switch {
		case t.IsDir():
			typ = "dir"
		case t.IsRegular():
			typ = "file"
		default:
			continue // symlinks, devices, sockets: hidden
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		if len(out) == maxListEntries {
			truncated = true
			break
		}
		out = append(out, Entry{Name: de.Name(), Type: typ, Size: info.Size(), Mtime: info.ModTime().UTC().Format(time.RFC3339)})
	}
	return out, truncated, nil
}

// looksBinary: a NUL byte or invalid UTF-8 in the sniffed prefix. When the
// prefix was cut short of EOF, a partial rune at the very end is allowed.
func looksBinary(b []byte, eof bool) bool {
	if bytes.IndexByte(b, 0) >= 0 {
		return true
	}
	if utf8.Valid(b) {
		return false
	}
	if eof {
		return true
	}
	for i := 1; i < utf8.UTFMax && i <= len(b); i++ {
		if utf8.Valid(b[:len(b)-i]) {
			// Only a short tail is invalid: accept if it is a rune prefix.
			return !isRunePrefix(b[len(b)-i:])
		}
	}
	return true
}

func isRunePrefix(t []byte) bool {
	if len(t) == 0 || len(t) >= utf8.UTFMax {
		return false
	}
	return !utf8.FullRune(t)
}

func sniff(fh *os.File) (bool, error) {
	buf := make([]byte, sniffBytes)
	n, err := io.ReadFull(fh, buf)
	eof := errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
	if err != nil && !eof {
		return false, err
	}
	if _, err := fh.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	return looksBinary(buf[:n], eof), nil
}

// ReadResult is the fs/read response.
type ReadResult struct {
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	Offset        int    `json:"offset"`
	LinesReturned int    `json:"lines_returned"`
	Truncated     bool   `json:"truncated"`
	Content       string `json:"content"`
}

// Read returns lines [offset, offset+limit) of a text file.
func (f *FS) Read(alias, p string, offset, limit int) (*ReadResult, error) {
	root, rel, err := f.resolve(alias, p)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return nil, errNotFile
	}
	fh, err := openUnder(root, rel)
	if err != nil {
		return nil, errNotFound
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return nil, errNotFound
	}
	if !fi.Mode().IsRegular() {
		return nil, errNotFile
	}
	if fi.Size() > maxReadFileSize {
		return nil, errTooLarge
	}
	bin, err := sniff(fh)
	if err != nil {
		return nil, errNotFound
	}
	if bin {
		return nil, errBinary
	}
	res := &ReadResult{Path: rel, Size: fi.Size(), Offset: offset}
	br := bufio.NewReader(fh)
	var sb strings.Builder
	line := 0
	for {
		s, rerr := br.ReadString('\n')
		if s != "" {
			line++
			if line >= offset {
				if res.LinesReturned == limit || sb.Len()+len(s) > maxReadBytes {
					res.Truncated = true
					break
				}
				sb.WriteString(s)
				res.LinesReturned++
			}
		}
		if rerr != nil {
			break
		}
	}
	res.Content = sb.String()
	return res, nil
}

// Hit is one search result. Line 0 means the file name matched.
type Hit struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

var errStopWalk = errors.New("stop")

// Search does a case-insensitive literal match on names and contents.
func (f *FS) Search(alias, p, q string, max int) ([]Hit, bool, error) {
	root, rel, err := f.resolve(alias, p)
	if err != nil {
		return nil, false, err
	}
	if max <= 0 || max > maxSearchHits {
		max = maxSearchHits
	}
	lq := strings.ToLower(q)
	deadline := time.Now().Add(searchTimeBudget)
	hits := []Hit{}
	files := 0
	truncated := false
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, false, errNotFound
	}
	defer r.Close()
	// fs.WalkDir over the os.Root's FS: never follows symlinks, never leaves root.
	walkErr := fs.WalkDir(r.FS(), rel, func(path string, de fs.DirEntry, err error) error {
		if err != nil {
			if de != nil && de.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if time.Now().After(deadline) || files >= searchFileBudget || len(hits) >= max {
			truncated = true
			return errStopWalk
		}
		if path == rel {
			return nil
		}
		if f.denied(de.Name()) {
			if de.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if de.IsDir() || !de.Type().IsRegular() {
			return nil
		}
		files++
		if strings.Contains(strings.ToLower(de.Name()), lq) {
			hits = append(hits, Hit{Path: path, Line: 0, Snippet: snippet(de.Name(), lq)})
		}
		if f.searchFile(r, path, lq, max, deadline, &hits) {
			truncated = true
			return errStopWalk
		}
		return nil
	})
	if walkErr != nil && walkErr != errStopWalk {
		return nil, false, errNotFound
	}
	if len(hits) > max {
		hits = hits[:max]
		truncated = true
	}
	return hits, truncated, nil
}

// searchFile appends content hits; returns true if a budget was hit mid-file.
func (f *FS) searchFile(r *os.Root, rel, lq string, max int, deadline time.Time, hits *[]Hit) bool {
	fh, err := r.Open(rel)
	if err != nil {
		return false
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxReadFileSize {
		return false
	}
	if bin, err := sniff(fh); err != nil || bin {
		return false
	}
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		if len(*hits) >= max {
			return true
		}
		if n%1024 == 0 && time.Now().After(deadline) {
			return true
		}
		line := sc.Text()
		if strings.Contains(strings.ToLower(line), lq) {
			*hits = append(*hits, Hit{Path: rel, Line: n, Snippet: snippet(line, lq)})
		}
	}
	return false
}

// snippet returns up to maxSnippetRunes runes of s around the first match.
func snippet(s, lq string) string {
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= maxSnippetRunes {
		return string(rs)
	}
	lower := []rune(strings.ToLower(string(rs)))
	at := 0
	if idx := strings.Index(string(lower), lq); idx > 0 {
		at = utf8.RuneCountInString(string(lower)[:idx])
	}
	start := at - maxSnippetRunes/4
	if start < 0 {
		start = 0
	}
	if start+maxSnippetRunes > len(rs) {
		start = len(rs) - maxSnippetRunes
	}
	return string(rs[start : start+maxSnippetRunes])
}
