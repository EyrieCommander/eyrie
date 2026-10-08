package bridge

import (
	"bufio"
	"bytes"
	"encoding/json"
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
	// osRoots is the spec's os.OpenRoot second guard, opened once at
	// startup alongside each root descriptor. Every successful open is
	// cross-checked against it (see open).
	osRoots map[string]*os.Root
	deny    []string
}

// NewFS opens each root once. A root that does not exist, is not a
// directory, or is itself a symlink is skipped (so it 404s) and reported.
func NewFS(roots map[string]string, extraDeny []string) (*FS, []error) {
	f := &FS{roots: map[string]*rootHandle{}, osRoots: map[string]*os.Root{}}
	f.deny = append(f.deny, baseDeny...)
	for _, g := range extraDeny {
		f.deny = append(f.deny, foldName(g))
	}
	var errs []error
	for alias, p := range roots {
		h, err := openRootHandle(filepath.Clean(p))
		if err != nil {
			errs = append(errs, &rootError{alias, err})
			continue
		}
		r, err := os.OpenRoot(filepath.Clean(p))
		if err != nil {
			h.close()
			errs = append(errs, &rootError{alias, err})
			continue
		}
		f.roots[alias] = h
		f.osRoots[alias] = r
	}
	return f, errs
}

// Close releases the root descriptors.
func (f *FS) Close() {
	for _, h := range f.roots {
		h.close()
	}
	for _, r := range f.osRoots {
		r.Close()
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
	n := foldName(name)
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
	// Second guard (spec): os.Root must resolve rel inside the root to the
	// very file we opened. A mismatch (the path changed under us, or any
	// escape) fails closed. The primary walk already refused symlinks, so
	// os.Root's own symlink following can't widen what we serve.
	if !f.sameUnderOSRoot(alias, rel, fh) {
		fh.Close()
		return nil, "", errDenied
	}
	return fh, rel, nil
}

func (f *FS) sameUnderOSRoot(alias, rel string, fh *os.File) bool {
	r, ok := f.osRoots[alias]
	if !ok {
		return false
	}
	viaRoot, err := r.Lstat(rel)
	if err != nil {
		return false
	}
	opened, err := fh.Stat()
	if err != nil {
		return false
	}
	return os.SameFile(viaRoot, opened)
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
	// maxReadBytes caps the whole JSON response: reserve the envelope (the
	// widest it can be: limit-digit line count, "false", and the encoder's
	// trailing newline) and budget the content's encoded size against the rest.
	envelope, _ := json.Marshal(ReadResult{Path: rel, Size: fi.Size(), Offset: offset, LinesReturned: limit, Truncated: false})
	budget := maxReadBytes - len(envelope) - 1
	br := bufio.NewReader(io.LimitReader(fh, maxReadFileSize))
	var sb strings.Builder
	encoded := 0
	line := 0
	for {
		s, rerr := br.ReadString('\n')
		if s != "" {
			line++
			if line >= offset {
				// Budget the JSON-encoded size, not raw bytes: escaping
				// (<, >, &, quotes, control chars) can grow content ~6x.
				enc := jsonStringLen(s)
				if res.LinesReturned == limit || encoded+enc > budget {
					res.Truncated = true
					break
				}
				sb.WriteString(s)
				encoded += enc
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

// searchDirBatch bounds memory per directory read.
const searchDirBatch = 256

// searchDir walks d in bounded batches, checking the budget per entry. Any
// entry it could not inspect (read, open or stat error) marks the result
// truncated, so a partial answer is never reported as complete. Entries
// are visited in directory order (sorting would need the whole listing).
func (f *FS) searchDir(d *os.File, prefix string, depth int, st *searchState) {
	if depth > maxSearchDepth {
		st.truncated = true
		return
	}
	for {
		if st.over() {
			return
		}
		des, err := d.ReadDir(searchDirBatch)
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
					st.truncated = true
					continue
				}
				f.searchDir(sub, prefix+name+"/", depth+1, st)
				sub.Close()
			case t.IsRegular():
				st.files++
				fh, err := openChild(d, name, wantFile)
				if err != nil {
					st.truncated = true
					continue
				}
				f.searchFile(fh, name, prefix+name, st)
				fh.Close()
			}
		}
		if err != nil {
			if err != io.EOF {
				st.truncated = true
			}
			return
		}
	}
}

// searchFile skips binary files entirely (name included), then matches the
// name and every line. There is no size cap here (that limit is for /read);
// the time and hit budgets bound the work. Lines are read whole with a
// per-line cap; a longer line is matched on its first maxSearchLine bytes
// and the result is marked truncated. Stat, sniff and read errors also mark
// the result truncated.
func (f *FS) searchFile(fh *os.File, name, rel string, st *searchState) {
	bin, err := sniff(fh)
	if err != nil {
		st.truncated = true
		return
	}
	if bin {
		return
	}
	if strings.Contains(strings.ToLower(name), st.lq) {
		st.hits = append(st.hits, Hit{Path: rel, Line: 0, Snippet: snippet(name, st.lq)})
	}
	br := bufio.NewReaderSize(fh, 64<<10)
	n := 0
	for {
		line, long, timedOut, rerr := readLineCapped(br, maxSearchLine, st.deadline)
		if timedOut {
			st.truncated = true
			return
		}
		if line != "" || long {
			n++
			if long {
				st.truncated = true
			}
			if len(st.hits) >= st.max {
				st.truncated = true
				return
			}
			if n%256 == 0 && time.Now().After(st.deadline) {
				st.truncated = true
				return
			}
			if strings.Contains(strings.ToLower(line), st.lq) {
				st.hits = append(st.hits, Hit{Path: rel, Line: n, Snippet: snippet(strings.TrimRight(line, "\r\n"), st.lq)})
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				st.truncated = true
			}
			return
		}
	}
}

// maxSearchLine caps how much of one line search keeps in memory.
const maxSearchLine = 4 << 20

// readLineCapped returns the next line (with its newline), keeping at most
// max bytes; long reports that the rest of the line was discarded. The
// deadline is checked on every chunk (bufio hands back at most its buffer
// size per call), so one enormous line can't run past the search budget.
func readLineCapped(br *bufio.Reader, max int, deadline time.Time) (line string, long, timedOut bool, err error) {
	var sb strings.Builder
	for chunk := 0; ; chunk++ {
		if chunk > 0 && time.Now().After(deadline) {
			return sb.String(), long, true, nil
		}
		frag, isPrefix, rerr := br.ReadLine()
		if sb.Len() < max {
			room := max - sb.Len()
			if len(frag) > room {
				frag = frag[:room]
				long = true
			}
			sb.Write(frag)
		} else if len(frag) > 0 {
			long = true
		}
		if rerr != nil {
			return sb.String(), long, false, rerr
		}
		if !isPrefix {
			sb.WriteByte('\n')
			return sb.String(), long, false, nil
		}
	}
}

// jsonStringLen is the number of bytes encoding/json writes for s's
// contents (without quotes), with its default HTML escaping.
func jsonStringLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\' || c == '\n' || c == '\r' || c == '\t' || c == '\b' || c == '\f':
				n += 2
			case c < 0x20 || c == '<' || c == '>' || c == '&':
				n += 6 // \u00XX
			default:
				n++
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			n += 6 // \ufffd
		} else if r == '\u2028' || r == '\u2029' {
			n += 6
		} else {
			n += size
		}
		i += size
	}
	return n
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
