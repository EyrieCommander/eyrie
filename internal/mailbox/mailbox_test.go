package mailbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }
func fixedBackoff(int) time.Duration     { return time.Minute }

func open(t *testing.T, max int) (*Store, *clock, string) {
	t.Helper()
	c := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "mb", "mailbox.db")
	s, err := Open(path, Options{MaxAttempts: max, Backoff: fixedBackoff, Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, c, path
}

var ctx = context.Background()

func mustEnqueue(t *testing.T, s *Store, id, agent, body string) {
	t.Helper()
	if _, dup, err := s.Enqueue(ctx, id, agent, "chief", body); err != nil || dup {
		t.Fatalf("enqueue %s: dup=%v err=%v", id, dup, err)
	}
}

func TestEnqueueDedupesOnID(t *testing.T) {
	s, _, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	m, dup, err := s.Enqueue(ctx, "m1", "codex", "chief", "hello")
	if err != nil || !dup || m.State != Queued {
		t.Fatalf("re-enqueue: %+v dup=%v err=%v", m, dup, err)
	}
	if _, _, err := s.Enqueue(ctx, "m1", "codex", "chief", "different"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting body: err=%v, want ErrConflict", err)
	}
	if _, _, err := s.Enqueue(ctx, "m1", "zeroclaw", "chief", "hello"); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflicting agent: err=%v, want ErrConflict", err)
	}
	all, _ := s.List(ctx, "codex")
	if len(all) != 1 {
		t.Fatalf("messages = %d, want 1", len(all))
	}
}

func TestEnqueueAfterDeliveryDoesNotRequeue(t *testing.T) {
	s, _, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	m, _ := s.Claim(ctx, "codex", "w1", time.Minute)
	if _, err := s.Ack(ctx, m.ID, m.LeaseToken); err != nil {
		t.Fatal(err)
	}
	m, dup, err := s.Enqueue(ctx, "m1", "codex", "chief", "hello")
	if err != nil || !dup || m.State != Delivered {
		t.Fatalf("re-enqueue after delivery: state=%s dup=%v err=%v", m.State, dup, err)
	}
	if _, err := s.Claim(ctx, "codex", "w2", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claim after delivered re-enqueue: %v, want ErrNotFound", err)
	}
}

func TestClaimIsFIFOAndPerAgent(t *testing.T) {
	s, c, _ := open(t, 3)
	mustEnqueue(t, s, "a", "codex", "1")
	c.advance(time.Second)
	mustEnqueue(t, s, "b", "zeroclaw", "2")
	c.advance(time.Second)
	mustEnqueue(t, s, "c", "codex", "3")

	m1, err := s.Claim(ctx, "codex", "w", time.Minute)
	if err != nil || m1.ID != "a" {
		t.Fatalf("first claim = %q %v, want a", m1.ID, err)
	}
	m2, err := s.Claim(ctx, "codex", "w", time.Minute)
	if err != nil || m2.ID != "c" {
		t.Fatalf("second claim = %q %v, want c (a is leased, b is another agent)", m2.ID, err)
	}
	if _, err := s.Claim(ctx, "codex", "w", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("third claim err = %v, want ErrNotFound", err)
	}
}

func TestAckIsReceiptAndIdempotent(t *testing.T) {
	s, c, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	m, _ := s.Claim(ctx, "codex", "w1", time.Minute)
	c.advance(5 * time.Second)
	got, err := s.Ack(ctx, m.ID, m.LeaseToken)
	if err != nil || got.State != Delivered || !got.DeliveredAt.Equal(c.t) {
		t.Fatalf("ack: %+v err=%v", got, err)
	}
	again, err := s.Ack(ctx, m.ID, m.LeaseToken)
	if err != nil || !again.DeliveredAt.Equal(got.DeliveredAt) {
		t.Fatalf("second ack changed receipt: %+v err=%v", again, err)
	}
}

func TestExpiredLeaseIsReclaimedAndOldOwnerLosesAck(t *testing.T) {
	s, c, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	first, err := s.Claim(ctx, "codex", "w1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, "codex", "w2", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claim during live lease: %v, want ErrNotFound", err)
	}
	c.advance(time.Minute)
	m, err := s.Claim(ctx, "codex", "w2", time.Minute)
	if err != nil || m.LeaseOwner != "w2" || m.Attempts != 2 {
		t.Fatalf("reclaim: %+v err=%v", m, err)
	}
	if _, err := s.Ack(ctx, "m1", first.LeaseToken); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("stale ack: %v, want ErrNotLeaseHolder", err)
	}
	if _, err := s.Release(ctx, "m1", first.LeaseToken, "x"); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("stale release: %v, want ErrNotLeaseHolder", err)
	}
	if _, err := s.Ack(ctx, "m1", m.LeaseToken); err != nil {
		t.Fatalf("current owner ack: %v", err)
	}
}

func TestLateAckOnUnreclaimedLeaseIsAccepted(t *testing.T) {
	s, c, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	l, _ := s.Claim(ctx, "codex", "w1", time.Minute)
	c.advance(2 * time.Minute)
	if m, err := s.Ack(ctx, "m1", l.LeaseToken); err != nil || m.State != Delivered {
		t.Fatalf("late ack: %+v err=%v", m, err)
	}
}

func TestReleaseBacksOffThenDeadLetters(t *testing.T) {
	s, c, _ := open(t, 2)
	mustEnqueue(t, s, "m1", "codex", "hello")

	l, _ := s.Claim(ctx, "codex", "w", time.Minute)
	m, err := s.Release(ctx, "m1", l.LeaseToken, "agent busy")
	if err != nil || m.State != Queued || m.LastError != "agent busy" || !m.NextAttemptAt.Equal(c.t.Add(time.Minute)) {
		t.Fatalf("first release: %+v err=%v", m, err)
	}
	if _, err := s.Claim(ctx, "codex", "w", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claim before backoff: %v, want ErrNotFound", err)
	}
	c.advance(time.Minute)
	l, err = s.Claim(ctx, "codex", "w", time.Minute)
	if err != nil {
		t.Fatalf("claim after backoff: %v", err)
	}
	m, err = s.Release(ctx, "m1", l.LeaseToken, "agent busy")
	if err != nil || m.State != Dead || m.Attempts != 2 {
		t.Fatalf("release at max attempts: %+v err=%v", m, err)
	}
	c.advance(time.Hour)
	if _, err := s.Claim(ctx, "codex", "w", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dead message claimed: %v", err)
	}
	dead, _ := s.List(ctx, "codex", Dead)
	if len(dead) != 1 {
		t.Fatalf("dead list = %d, want 1", len(dead))
	}
}

func TestRepeatedLeaseExpiryDeadLetters(t *testing.T) {
	s, c, _ := open(t, 2)
	mustEnqueue(t, s, "m1", "codex", "hello")
	_, _ = s.Claim(ctx, "codex", "w", time.Minute)
	c.advance(time.Minute)
	_, _ = s.Claim(ctx, "codex", "w", time.Minute)
	c.advance(time.Minute)
	if m, err := s.Claim(ctx, "codex", "w", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("third claim: %+v %v, want ErrNotFound (max attempts 2)", m, err)
	}
	m, _ := s.Get(ctx, "m1")
	if m.State != Dead || m.LastError != "lease expired" {
		t.Fatalf("after expiries: %+v", m)
	}
}

func TestDeadLetterDoesNotBlockLaterMessages(t *testing.T) {
	s, c, _ := open(t, 1)
	mustEnqueue(t, s, "old", "codex", "1")
	c.advance(time.Second)
	mustEnqueue(t, s, "new", "codex", "2")
	_, _ = s.Claim(ctx, "codex", "w", time.Minute)
	c.advance(time.Minute)
	m, err := s.Claim(ctx, "codex", "w", time.Minute)
	if err != nil || m.ID != "new" {
		t.Fatalf("claim = %q %v, want new (old should dead-letter in the same call)", m.ID, err)
	}
}

func TestSurvivesReopen(t *testing.T) {
	s, c, path := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	_, _ = s.Claim(ctx, "codex", "w1", time.Minute)
	_ = s.Close()

	s2, err := Open(path, Options{MaxAttempts: 3, Backoff: fixedBackoff, Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	m, err := s2.Get(ctx, "m1")
	if err != nil || m.State != Leased || m.LeaseOwner != "w1" || m.Body != "hello" {
		t.Fatalf("after reopen: %+v err=%v", m, err)
	}
	c.advance(time.Minute)
	if m, err := s2.Claim(ctx, "codex", "w2", time.Minute); err != nil || m.ID != "m1" {
		t.Fatalf("reclaim after reopen: %+v err=%v", m, err)
	}
}

func TestFilePermissionsAreOwnerOnly(t *testing.T) {
	_, _, path := open(t, 3)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("db mode = %v, want no group/other bits", fi.Mode().Perm())
	}
	di, _ := os.Stat(filepath.Dir(path))
	if di.Mode().Perm()&0o077 != 0 {
		t.Fatalf("dir mode = %v, want no group/other bits", di.Mode().Perm())
	}
}

func TestUnknownIDs(t *testing.T) {
	s, _, _ := open(t, 3)
	if _, err := s.Get(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get: %v", err)
	}
	if _, err := s.Ack(ctx, "nope", "w"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ack: %v", err)
	}
	if _, err := s.Release(ctx, "nope", "w", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("release: %v", err)
	}
}

func TestSameOwnerReclaimRejectsStaleToken(t *testing.T) {
	s, c, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	old, _ := s.Claim(ctx, "codex", "w1", time.Minute)
	c.advance(time.Minute)
	cur, err := s.Claim(ctx, "codex", "w1", time.Minute)
	if err != nil || cur.LeaseToken == old.LeaseToken || cur.LeaseToken == "" {
		t.Fatalf("reclaim by same owner: %+v err=%v (old token %q)", cur, err, old.LeaseToken)
	}
	if _, err := s.Release(ctx, "m1", old.LeaseToken, "late failure"); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("stale release by same owner: %v, want ErrNotLeaseHolder", err)
	}
	if _, err := s.Ack(ctx, "m1", old.LeaseToken); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("stale ack by same owner: %v, want ErrNotLeaseHolder", err)
	}
	if m, _ := s.Get(ctx, "m1"); m.State != Leased || m.LeaseToken != cur.LeaseToken {
		t.Fatalf("current attempt disturbed: %+v", m)
	}
}

func TestEmptyTokenNeverMatches(t *testing.T) {
	s, _, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	if _, err := s.Ack(ctx, "m1", ""); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("ack with empty token on queued message: %v", err)
	}
	if m, _ := s.Get(ctx, "m1"); m.State != Queued {
		t.Fatalf("state = %s", m.State)
	}
}

// Two Stores on one file simulate two Eyrie processes (or a restart overlap).
func TestTwoStoresCannotDoubleLeaseOrOverwrite(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "mailbox.db")
	opts := Options{MaxAttempts: 5, Backoff: fixedBackoff, Now: c.now}
	a, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Open(path, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	const n = 40
	for i := 0; i < n; i++ {
		mustEnqueue(t, a, "m"+strconv.Itoa(i), "codex", "x")
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	claimed := map[string]int{}
	for _, st := range []*Store{a, b, a, b} {
		wg.Add(1)
		go func(st *Store) {
			defer wg.Done()
			for {
				m, err := st.Claim(ctx, "codex", "w", time.Hour)
				if errors.Is(err, ErrNotFound) {
					return
				}
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				claimed[m.ID]++
				mu.Unlock()
			}
		}(st)
	}
	wg.Wait()
	if len(claimed) != n {
		t.Fatalf("claimed %d distinct messages, want %d", len(claimed), n)
	}
	for id, k := range claimed {
		if k != 1 {
			t.Fatalf("%s claimed %d times under a live lease", id, k)
		}
	}
}

func TestReleaseAfterCrossStoreReclaimIsRejected(t *testing.T) {
	// Store a reads the lease, store b reclaims, then a's conditional write
	// must not overwrite b's lease.
	c := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "mailbox.db")
	opts := Options{MaxAttempts: 5, Backoff: fixedBackoff, Now: c.now}
	a, _ := Open(path, opts)
	defer a.Close()
	b, _ := Open(path, opts)
	defer b.Close()
	mustEnqueue(t, a, "m1", "codex", "x")
	old, _ := a.Claim(ctx, "codex", "w1", time.Minute)
	c.advance(time.Minute)
	cur, err := b.Claim(ctx, "codex", "w2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Release(ctx, "m1", old.LeaseToken, "late"); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("cross-store stale release: %v", err)
	}
	if m, _ := b.Get(ctx, "m1"); m.LeaseToken != cur.LeaseToken || m.State != Leased {
		t.Fatalf("b's lease overwritten: %+v", m)
	}
}

func TestOpenTightensExistingFilePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mb")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "mailbox.db")
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(p, 0o644)
	}
	// Pre-existing sidecars stay in place (empty -wal/-shm are valid for
	// SQLite), so the check below covers their tightening too.
	s, err := Open(path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(p), err)
		}
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, want 0600", filepath.Base(p), fi.Mode().Perm())
		}
	}
}

// securePaths itself tightens existing sidecars. Tested directly because
// SQLite also gives -wal/-shm the main file's mode when it opens, which
// would hide a regression in our own chmod in the end-to-end test above.
func TestSecurePathsTightensSidecars(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mb")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path, err := resolvePath(filepath.Join(dir, "mailbox.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(p, 0o644)
	}
	if err := securePaths(path); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, _ := os.Stat(p)
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v after securePaths, want 0600", filepath.Base(p), fi.Mode().Perm())
		}
	}
}

func TestOpenRefusesWritableDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(dir, "mailbox.db"), Options{}); !errors.Is(err, ErrInsecurePath) {
		t.Fatalf("open in 0777 dir: %v, want ErrInsecurePath", err)
	}
}

func TestOpenRefusesSymlinkedDB(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mb")
	_ = os.MkdirAll(dir, 0o700)
	target := filepath.Join(t.TempDir(), "elsewhere.db")
	_ = os.WriteFile(target, nil, 0o600)
	path := filepath.Join(dir, "mailbox.db")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, Options{}); err == nil {
		t.Fatal("opened a symlinked database")
	}
}

// sqliteHeader reports whether the file at p is a SQLite database (non-empty,
// with the magic header). securePaths creates an empty file at the checked
// path, so existence alone proves nothing about where SQLite wrote.
func sqliteHeader(t *testing.T, p string) bool {
	t.Helper()
	b, err := os.ReadFile(p)
	return err == nil && len(b) >= 16 && string(b[:15]) == "SQLite format 3"
}

func TestOddPathCharactersOpenTheCheckedFile(t *testing.T) {
	for _, name := range []string{"a?mode=ro.db", "b#frag.db", "c%2e%2e.db", "d%20e.db", "e e.db"} {
		dir := filepath.Join(t.TempDir(), "x")
		path := filepath.Join(dir, name)
		s, err := Open(path, Options{})
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		mustEnqueue(t, s, "m1", "codex", "x")
		_ = s.Close()
		if !sqliteHeader(t, path) {
			t.Fatalf("%q: no database at the checked path", name)
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			n := e.Name()
			if n == name || n == name+"-wal" || n == name+"-shm" {
				continue
			}
			t.Fatalf("%q: stray file %q next to the database (SQLite opened another path)", name, n)
		}
		if parent, _ := os.ReadDir(filepath.Dir(dir)); len(parent) != 1 {
			t.Fatalf("%q: files outside the db directory: %v", name, parent)
		}
	}
}

func TestDotDotThroughSymlinkUsesOneResolvedPath(t *testing.T) {
	// /root/safe/link -> /root/other/sub ; open /root/safe/link/../a.db.
	// The OS resolves that to /root/other/a.db; a lexical clean gives
	// /root/safe/a.db. Checks and SQLite must agree on the OS answer.
	root := t.TempDir()
	safe := filepath.Join(root, "safe")
	sub := filepath.Join(root, "other", "sub")
	_ = os.MkdirAll(safe, 0o700)
	_ = os.MkdirAll(sub, 0o700)
	if err := os.Symlink(sub, filepath.Join(safe, "link")); err != nil {
		t.Fatal(err)
	}
	s, err := Open(safe+"/link/../a.db", Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustEnqueue(t, s, "m1", "codex", "x")
	_ = s.Close()
	osPath := filepath.Join(root, "other", "a.db")
	if !sqliteHeader(t, osPath) {
		t.Fatalf("database not at the OS-resolved path %s", osPath)
	}
	if _, err := os.Stat(filepath.Join(safe, "a.db")); err == nil {
		t.Fatal("a database file appeared at the lexically cleaned path")
	}
	if fi, _ := os.Stat(osPath); fi.Mode().Perm() != 0o600 {
		t.Fatalf("resolved db mode = %v", fi.Mode().Perm())
	}
}

func TestConcurrentEnqueueAcrossStores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailbox.db")
	a, _ := Open(path, Options{})
	defer a.Close()
	b, _ := Open(path, Options{})
	defer b.Close()
	for round := 0; round < 20; round++ {
		id := "same-" + strconv.Itoa(round)
		var wg sync.WaitGroup
		results := make([]struct {
			dup bool
			err error
		}, 2)
		for i, st := range []*Store{a, b} {
			wg.Add(1)
			go func(i int, st *Store) {
				defer wg.Done()
				_, results[i].dup, results[i].err = st.Enqueue(ctx, id, "codex", "chief", "same body")
			}(i, st)
		}
		wg.Wait()
		for _, r := range results {
			if r.err != nil {
				t.Fatalf("round %d identical enqueue: %v", round, r.err)
			}
		}
		if results[0].dup == results[1].dup {
			t.Fatalf("round %d: dup flags %v/%v, want exactly one original", round, results[0].dup, results[1].dup)
		}

		cid := "conflict-" + strconv.Itoa(round)
		var errs [2]error
		wg.Add(2)
		go func() { defer wg.Done(); _, _, errs[0] = a.Enqueue(ctx, cid, "codex", "chief", "one") }()
		go func() { defer wg.Done(); _, _, errs[1] = b.Enqueue(ctx, cid, "codex", "chief", "two") }()
		wg.Wait()
		ok, conflict := 0, 0
		for _, e := range errs {
			switch {
			case e == nil:
				ok++
			case errors.Is(e, ErrConflict):
				conflict++
			default:
				t.Fatalf("round %d conflicting enqueue: %v", round, e)
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("round %d: ok=%d conflict=%d, want 1/1", round, ok, conflict)
		}
	}
}

func TestClaimReadsClockAfterLockWait(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "mailbox.db")
	a, _ := Open(path, Options{Now: c.now})
	defer a.Close()
	b, _ := Open(path, Options{Now: c.now})
	defer b.Close()
	mustEnqueue(t, a, "m1", "codex", "x")
	holder, err := b.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	type res struct {
		m   Message
		err error
	}
	out := make(chan res, 1)
	go func() {
		m, err := a.Claim(ctx, "codex", "w", time.Second)
		out <- res{m, err}
	}()
	time.Sleep(150 * time.Millisecond)
	c.advance(2 * time.Second) // the lock wait outlasts the lease
	_ = holder.Rollback()
	r := <-out
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !r.m.LeaseUntil.After(c.now()) {
		t.Fatalf("lease until %v is not after now %v: computed before the lock wait", r.m.LeaseUntil, c.now())
	}
}

func TestStaleTokenRejectedAfterReclaimerAcks(t *testing.T) {
	s, c, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "x")
	a, _ := s.Claim(ctx, "codex", "w1", time.Minute)
	c.advance(time.Minute)
	b, _ := s.Claim(ctx, "codex", "w2", time.Minute)
	if _, err := s.Ack(ctx, "m1", b.LeaseToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ack(ctx, "m1", a.LeaseToken); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("stale token ack after delivery: %v, want ErrNotLeaseHolder", err)
	}
	if _, err := s.Ack(ctx, "m1", "unrelated"); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("unrelated token ack after delivery: %v", err)
	}
	m, err := s.Ack(ctx, "m1", b.LeaseToken)
	if err != nil || m.DeliveredToken != b.LeaseToken {
		t.Fatalf("retry of the delivering ack: %+v %v", m, err)
	}
}

func TestOpenRefusesGroupWritableAncestor(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "shared")
	if err := os.MkdirAll(filepath.Join(parent, "mine"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o770); err != nil { // group-writable, not sticky
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	_, err := Open(filepath.Join(parent, "mine", "x.db"), Options{})
	if !errors.Is(err, ErrInsecurePath) {
		t.Fatalf("open under a group-writable ancestor: %v, want ErrInsecurePath", err)
	}
	if _, serr := os.Stat(filepath.Join(parent, "mine", "x.db")); serr == nil {
		t.Fatal("database file created before the ancestry check refused")
	}
}

func TestOpenAllowsStickyWritableAncestor(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "tmpish")
	if err := os.MkdirAll(filepath.Join(parent, "mine"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	s, err := Open(filepath.Join(parent, "mine", "x.db"), Options{})
	if err != nil {
		t.Fatalf("sticky world-writable ancestor (like /tmp) refused: %v", err)
	}
	_ = s.Close()
}

func TestOpenDanglingSymlinkCreatesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "mb")
	_ = os.MkdirAll(dir, 0o700)
	target := filepath.Join(t.TempDir(), "outside", "planted.db")
	_ = os.MkdirAll(filepath.Dir(target), 0o700)
	if err := os.Symlink(target, filepath.Join(dir, "x.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(dir, "x.db"), Options{}); err == nil {
		t.Fatal("opened through a dangling symlink")
	}
	if _, err := os.Lstat(target); err == nil {
		t.Fatal("Open created the symlink's target outside the mailbox directory")
	}
}

func TestReleaseRacingReclaimInTheWindowIsRejected(t *testing.T) {
	// Store a's Release has read a valid lease; before its write, store b
	// reclaims the (expired) message. a's conditional write must not
	// overwrite b's lease.
	c := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "mailbox.db")
	a, _ := Open(path, Options{MaxAttempts: 5, Backoff: fixedBackoff, Now: c.now})
	defer a.Close()
	b, _ := Open(path, Options{MaxAttempts: 5, Backoff: fixedBackoff, Now: c.now})
	defer b.Close()
	mustEnqueue(t, a, "m1", "codex", "x")
	old, _ := a.Claim(ctx, "codex", "w1", time.Minute)
	var cur Message
	var claimErr error
	testHookReleaseBetweenReadAndWrite = func() {
		testHookReleaseBetweenReadAndWrite = nil
		c.advance(time.Minute)
		cur, claimErr = b.Claim(ctx, "codex", "w2", time.Minute)
	}
	defer func() { testHookReleaseBetweenReadAndWrite = nil }()
	if _, err := a.Release(ctx, "m1", old.LeaseToken, "late"); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("release after an in-window reclaim: %v, want ErrNotLeaseHolder", err)
	}
	if claimErr != nil {
		t.Fatal(claimErr)
	}
	if m, _ := b.Get(ctx, "m1"); m.State != Leased || m.LeaseToken != cur.LeaseToken {
		t.Fatalf("b's lease overwritten: %+v", m)
	}
}
