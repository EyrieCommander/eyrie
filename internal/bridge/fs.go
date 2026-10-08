package bridge

import (
	"bufio"
	"bytes"
	"errors"
	"io"
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
	maxSearchDepth   = 64
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
//
// Each root is opened once, at startup, as a directory descriptor. Every
// request walks from that descriptor with openat(O_NOFOLLOW) one component
// at a time (open_unix.go), so:
//   - replacing a root's pathname later cannot redirect the bridge;
//   - a symlink anywhere in the path is refused at open time, not just at
//     an earlier check, so a check-then-swap race cannot reach a link;
//   - the final open is non-blocking and the kind is checked by fstat on the
//     opened descriptor, so FIFOs and devices are refused without blocking.
//
// This replaces os.Root, which follows in-root symlinks even when asked
// for O_NOFOLLOW and so can be raced onto a denied file.
type FS struct {
	roots map[string]*rootHandle
	deny  []string
}

// NewFS opens each root once. A root that does not exist, is not a
// directory, or is itself a symlink is skipped (so it 404s) and reported.
func NewFS(roots map[string]string, extraDeny []string) (*FS, []error) {
	f := &FS{roots: map[string]*rootHandle{}}
	f.deny = append(f.deny, baseDeny...)
	for _, g := range extraDeny {
		f.deny = append(f.deny, strings.ToLower(g))
	}
	var errs []error
	for alias, p := range roots {
		h, err := openRootHandle(filepath.Clean(p))
		if err != nil {
			errs = append(errs, &rootError{alias, err})
			continue
		}
		f.roots[alias] = h
	}
	return f, errs
}

// Close releases the root descriptors.
func (f *FS) Close() {
	for _, h := range f.roots {
		h.close()
	}
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

// open validates alias and path, then opens through the root descriptor.
func (f *FS) open(alias, p string, kind openKind) (*os.File, string, error) {
	h, ok := f.roots[alias]
	if !ok {
		return nil, "", errUnknownRoot
	}
	rel, err := cleanRel(p)
	if err != nil {
		return nil, "", err
	}
	if rel == "." && kind == wantFile {
		return nil, "", errNotFile
	}
	fh, err := f.openRel(h, rel, kind)
	if err != nil {
		return nil, "", err
	}
	return fh, rel, nil
}

// Entry is one list result.
type Entry struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	Mtime string `json:"mtime"`
}

// List returns visible entries of a directory, sorted, capped. Symlinks,
// FIFOs, devices and sockets are hidden, as are denied names.
func (f *FS) List(alias, p string) ([]Entry, bool, error) {
	d, _, err := f.open(alias, p, wantDir)
	if err != nil {
		return nil, false, err
	}
	defer d.Close()
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
			continue
		}
		// Stat relative to the open directory descriptor, never by pathname
		// (DirEntry.Info would re-resolve the name from the process cwd).
		size, mtime, ok := statAt(d, de.Name())
		if !ok {
			continue
		}
		if len(out) == maxListEntries {
			truncated = true
			break
		}
		out = append(out, Entry{Name: de.Name(), Type: typ, Size: size, Mtime: mtime.UTC().Format(time.RFC3339)})
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
	fh, rel, err := f.open(alias, p, wantFile)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	fi, err := fh.Stat()
	if err != nil {
		return nil, errNotFound
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
	br := bufio.NewReader(io.LimitReader(fh, maxReadFileSize))
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

type searchState struct {
	lq        string
	max       int
	deadline  time.Time
	files     int
	hits      []Hit
	truncated bool
}

func (st *searchState) over() bool {
	if len(st.hits) >= st.max || st.files >= searchFileBudget || time.Now().After(st.deadline) {
		st.truncated = true
		return true
	}
	return false
}

// Search does a case-insensitive literal match on names and contents. It
// walks directory descriptors with openat(O_NOFOLLOW), so it never follows a
// symlink and never leaves the root, even if paths change mid-walk.
func (f *FS) Search(alias, p, q string, max int) ([]Hit, bool, error) {
	d, rel, err := f.open(alias, p, wantDir)
	if err != nil {
		return nil, false, err
	}
	defer d.Close()
	if max <= 0 || max > maxSearchHits {
		max = maxSearchHits
	}
	st := &searchState{lq: strings.ToLower(q), max: max, deadline: time.Now().Add(searchTimeBudget)}
	prefix := ""
	if rel != "." {
		prefix = rel + "/"
	}
	f.searchDir(d, prefix, 0, st)
	if len(st.hits) > max {
		st.hits = st.hits[:max]
		st.truncated = true
	}
	return st.hits, st.truncated, nil
}

func (f *FS) searchDir(d *os.File, prefix string, depth int, st *searchState) {
	if depth > maxSearchDepth {
		st.truncated = true
		return
	}
	des, err := d.ReadDir(-1)
	if err != nil {
		return
	}
	sort.Slice(des, func(i, j int) bool { return des[i].Name() < des[j].Name() })
	for _, de := range des {
		if st.over() {
			return
		}
		name := de.Name()
		if f.denied(name) {
			continue
		}
		t := de.Type()
		switch {
		case t.IsDir():
			sub, err := openChild(d, name, wantDir)
			if err != nil {
				continue
			}
			f.searchDir(sub, prefix+name+"/", depth+1, st)
			sub.Close()
		case t.IsRegular():
			st.files++
			rel := prefix + name
			if strings.Contains(strings.ToLower(name), st.lq) {
				st.hits = append(st.hits, Hit{Path: rel, Line: 0, Snippet: snippet(name, st.lq)})
			}
			fh, err := openChild(d, name, wantFile)
			if err != nil {
				continue
			}
			f.searchFile(fh, rel, st)
			fh.Close()
		}
	}
}

func (f *FS) searchFile(fh *os.File, rel string, st *searchState) {
	fi, err := fh.Stat()
	if err != nil || fi.Size() > maxReadFileSize {
		return
	}
	if bin, err := sniff(fh); err != nil || bin {
		return
	}
	sc := bufio.NewScanner(io.LimitReader(fh, maxReadFileSize))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		if len(st.hits) >= st.max {
			st.truncated = true
			return
		}
		if n%1024 == 0 && time.Now().After(st.deadline) {
			st.truncated = true
			return
		}
		line := sc.Text()
		if strings.Contains(strings.ToLower(line), st.lq) {
			st.hits = append(st.hits, Hit{Path: rel, Line: n, Snippet: snippet(line, st.lq)})
		}
	}
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
