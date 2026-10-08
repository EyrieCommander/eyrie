package approvals

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var ctx = context.Background()

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func open(t *testing.T) (*Store, *clock, string) {
	t.Helper()
	c := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "ap", "approvals.db")
	s, err := Open(path, Options{Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, c, path
}

func binding() Binding {
	return Binding{Actor: "chief", Project: "eyrie", Target: "captain-1", Action: "process.stop", PayloadHash: HashPayload([]byte(`{"pid":123}`))}
}

func approved(t *testing.T, s *Store) Request {
	t.Helper()
	r, err := s.Create(ctx, binding(), "stop pid 123", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if r, err = s.Decide(ctx, r.ID, true, "dan", ViaLocalUI); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestCreateGrantsNothing(t *testing.T) {
	s, _, _ := open(t)
	r, err := s.Create(ctx, binding(), "stop", time.Hour)
	if err != nil || r.State != Requested {
		t.Fatalf("create: %+v %v", r, err)
	}
	if _, err := s.Consume(ctx, r.ID, binding()); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("consume of undecided request: %v, want ErrNotApproved", err)
	}
}

func TestApproveThenConsumeOnce(t *testing.T) {
	s, _, _ := open(t)
	r := approved(t, s)
	if r.DecidedBy != "dan" || r.DecidedVia != ViaLocalUI || r.DecidedAt.IsZero() {
		t.Fatalf("decision audit fields: %+v", r)
	}
	got, err := s.Consume(ctx, r.ID, binding())
	if err != nil || got.State != Consumed || got.ConsumedAt.IsZero() {
		t.Fatalf("consume: %+v %v", got, err)
	}
	if _, err := s.Consume(ctx, r.ID, binding()); !errors.Is(err, ErrAlreadyConsumed) {
		t.Fatalf("replay: %v, want ErrAlreadyConsumed", err)
	}
}

func TestDenyNeverDispatches(t *testing.T) {
	s, _, _ := open(t)
	r, _ := s.Create(ctx, binding(), "stop", time.Hour)
	if r, _ = s.Decide(ctx, r.ID, false, "dan", ViaLocalUI); r.State != Denied {
		t.Fatalf("state = %s", r.State)
	}
	if _, err := s.Consume(ctx, r.ID, binding()); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("consume denied: %v", err)
	}
	if _, err := s.Decide(ctx, r.ID, true, "dan", ViaLocalUI); !errors.Is(err, ErrNotPending) {
		t.Fatalf("flip denied to approved: %v, want ErrNotPending", err)
	}
}

func TestOnlyLocalUICanDecide(t *testing.T) {
	s, _, _ := open(t)
	r, _ := s.Create(ctx, binding(), "stop", time.Hour)
	for _, via := range []Via{"", "chat", "chief", "bridge", "workbench", "LOCAL-UI", "local-ui "} {
		if _, err := s.Decide(ctx, r.ID, true, "dan", via); !errors.Is(err, ErrUntrustedVia) {
			t.Fatalf("via %q: %v, want ErrUntrustedVia", via, err)
		}
	}
	if got, _ := s.Get(ctx, r.ID); got.State != Requested {
		t.Fatalf("state changed by untrusted channel: %s", got.State)
	}
}

func TestExpiryBeforeDecisionAndBeforeConsume(t *testing.T) {
	s, c, _ := open(t)
	r, _ := s.Create(ctx, binding(), "stop", time.Minute)
	c.advance(time.Minute)
	if _, err := s.Decide(ctx, r.ID, true, "dan", ViaLocalUI); !errors.Is(err, ErrExpired) {
		t.Fatalf("decide after expiry: %v, want ErrExpired", err)
	}

	r2, _ := s.Create(ctx, binding(), "stop", time.Minute)
	_, _ = s.Decide(ctx, r2.ID, true, "dan", ViaLocalUI)
	c.advance(time.Minute)
	if _, err := s.Consume(ctx, r2.ID, binding()); !errors.Is(err, ErrExpired) {
		t.Fatalf("consume after expiry: %v, want ErrExpired", err)
	}
	if got, _ := s.Get(ctx, r2.ID); got.State != Expired {
		t.Fatalf("expired approval state = %s", got.State)
	}
}

func TestMismatchedBindingNeverDispatches(t *testing.T) {
	s, _, _ := open(t)
	r := approved(t, s)
	mut := map[string]func(*Binding){
		"actor":   func(b *Binding) { b.Actor = "someone-else" },
		"project": func(b *Binding) { b.Project = "other" },
		"target":  func(b *Binding) { b.Target = "captain-2" },
		"action":  func(b *Binding) { b.Action = "process.start" },
		"payload": func(b *Binding) { b.PayloadHash = HashPayload([]byte(`{"pid":124}`)) },
	}
	for name, f := range mut {
		b := binding()
		f(&b)
		if _, err := s.Consume(ctx, r.ID, b); !errors.Is(err, ErrMismatch) {
			t.Fatalf("%s changed: %v, want ErrMismatch", name, err)
		}
	}
	if _, err := s.Consume(ctx, r.ID, binding()); err != nil {
		t.Fatalf("mismatch attempts used up the approval: %v", err)
	}
}

func TestBadPayloadHashRejected(t *testing.T) {
	s, _, _ := open(t)
	b := binding()
	b.PayloadHash = "deadbeef"
	if _, err := s.Create(ctx, b, "x", time.Hour); err == nil {
		t.Fatal("short hash accepted")
	}
	b.PayloadHash = HashPayload(nil)[:63] + "G"
	if _, err := s.Create(ctx, b, "x", time.Hour); err == nil {
		t.Fatal("non-hex hash accepted")
	}
}

func TestRestartPreservesRequestWithoutGranting(t *testing.T) {
	s, c, path := open(t)
	r, _ := s.Create(ctx, binding(), "stop", time.Hour)
	_ = s.Close()
	s2, err := Open(path, Options{Now: c.now})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	pend, err := s2.Pending(ctx)
	if err != nil || len(pend) != 1 || pend[0].ID != r.ID || pend[0].State != Requested {
		t.Fatalf("pending after restart: %+v %v", pend, err)
	}
	if _, err := s2.Consume(ctx, r.ID, binding()); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("restart granted: %v", err)
	}
}

func TestConcurrentConsumeAcrossStoresDispatchesOnce(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "approvals.db")
	a, _ := Open(path, Options{Now: c.now})
	defer a.Close()
	b, _ := Open(path, Options{Now: c.now})
	defer b.Close()
	r := approved(t, a)
	var wins int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		st := a
		if i%2 == 1 {
			st = b
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := st.Consume(ctx, r.ID, binding()); err == nil {
				atomic.AddInt32(&wins, 1)
			} else if !errors.Is(err, ErrAlreadyConsumed) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins != 1 {
		t.Fatalf("consumed %d times, want 1", wins)
	}
}

func TestMalformedRecordsFailClosed(t *testing.T) {
	s, _, _ := open(t)
	cases := map[string]string{
		"unknown state":          `UPDATE approvals SET state = 'granted'`,
		"approved without UI":    `UPDATE approvals SET state = 'approved', decided_by = 'dan', decided_via = 'chat', decided_at = 1`,
		"approved, no decider":   `UPDATE approvals SET state = 'approved', decided_by = '', decided_via = 'local-ui', decided_at = 1`,
		"bad hash":               `UPDATE approvals SET payload_hash = 'x'`,
		"consumed, no time":      `UPDATE approvals SET state = 'consumed', decided_by = 'dan', decided_via = 'local-ui', decided_at = 1, consumed_at = 0`,
		"approved, consumed_at":  `UPDATE approvals SET state = 'approved', decided_by = 'dan', decided_via = 'local-ui', decided_at = 1, consumed_at = 5`,
		"requested with decider": `UPDATE approvals SET decided_by = 'dan', decided_via = 'local-ui', decided_at = 1`,
		"expires before create":  `UPDATE approvals SET expires_at = created_at`,
	}
	for name, q := range cases {
		r, _ := s.Create(ctx, binding(), name, time.Hour)
		if _, err := s.db.Exec(q+` WHERE id = ?`, r.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Consume(ctx, r.ID, binding()); !errors.Is(err, ErrMalformed) {
			t.Fatalf("%s: consume = %v, want ErrMalformed", name, err)
		}
		if _, err := s.Get(ctx, r.ID); !errors.Is(err, ErrMalformed) {
			t.Fatalf("%s: get = %v, want ErrMalformed", name, err)
		}
		_, _ = s.db.Exec(`DELETE FROM approvals WHERE id = ?`, r.ID)
	}
}

func TestOpenFileHygiene(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.Chmod(dir, 0o777)
	if _, err := Open(filepath.Join(dir, "a.db"), Options{}); !errors.Is(err, ErrInsecurePath) {
		t.Fatalf("writable dir: %v", err)
	}
	dir2 := filepath.Join(t.TempDir(), "ok")
	_ = os.MkdirAll(dir2, 0o700)
	p := filepath.Join(dir2, "a.db")
	_ = os.WriteFile(p, nil, 0o644)
	_ = os.Chmod(p, 0o644)
	s, err := Open(p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v", fi.Mode().Perm())
	}
	dir3 := filepath.Join(t.TempDir(), "ln")
	_ = os.MkdirAll(dir3, 0o700)
	target := filepath.Join(t.TempDir(), "x.db")
	_ = os.WriteFile(target, nil, 0o600)
	_ = os.Symlink(target, filepath.Join(dir3, "a.db"))
	if _, err := Open(filepath.Join(dir3, "a.db"), Options{}); err == nil {
		t.Fatal("opened symlinked db")
	}
}

func TestUnknownID(t *testing.T) {
	s, _, _ := open(t)
	if _, err := s.Consume(ctx, "nope", binding()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("consume unknown: %v", err)
	}
	if _, err := s.Decide(ctx, "nope", true, "dan", ViaLocalUI); !errors.Is(err, ErrNotFound) {
		t.Fatalf("decide unknown: %v", err)
	}
}

func TestConsumeHoldsWriteLockAcrossReadAndWrite(t *testing.T) {
	// While store a is between its read and its write, store b's Consume must
	// wait for a's transaction, then see the approval already used.
	c := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "approvals.db")
	a, _ := Open(path, Options{Now: c.now})
	defer a.Close()
	b, _ := Open(path, Options{Now: c.now})
	defer b.Close()
	r := approved(t, a)

	bDone := make(chan error, 1)
	testHookBeforeConsumeWrite = func() {
		testHookBeforeConsumeWrite = nil
		go func() {
			_, err := b.Consume(ctx, r.ID, binding())
			bDone <- err
		}()
		select {
		case err := <-bDone:
			bDone <- err // b finished inside a's window: the lock didn't hold
		case <-time.After(200 * time.Millisecond):
		}
	}
	defer func() { testHookBeforeConsumeWrite = nil }()

	if _, err := a.Consume(ctx, r.ID, binding()); err != nil {
		t.Fatalf("store a consume: %v", err)
	}
	if err := <-bDone; !errors.Is(err, ErrAlreadyConsumed) {
		t.Fatalf("store b after a committed: %v, want ErrAlreadyConsumed", err)
	}
}

func TestConsumeCannotCommitAfterExpiryUsingAStaleClock(t *testing.T) {
	// Reviewer's scenario: between Consume's read and its write, another
	// Store grabs the write lock, the approval expires, then the lock is
	// released. Consume must not succeed after the expiry with a clock value
	// it read before the wait. With Consume holding the lock across read and
	// write, the other Store can't get in at all.
	c := &clock{t: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
	path := filepath.Join(t.TempDir(), "approvals.db")
	a, _ := Open(path, Options{Now: c.now})
	defer a.Close()
	b, _ := Open(path, Options{Now: c.now})
	defer b.Close()
	r, _ := a.Create(ctx, binding(), "stop", time.Minute)
	_, _ = a.Decide(ctx, r.ID, true, "dan", ViaLocalUI)

	bAcquired := make(chan struct{})
	bFinished := make(chan struct{})
	testHookBeforeConsumeWrite = func() {
		testHookBeforeConsumeWrite = nil
		go func() {
			defer close(bFinished)
			tx, err := b.db.BeginTx(ctx, nil)
			if err != nil {
				return
			}
			close(bAcquired)
			time.Sleep(150 * time.Millisecond)
			c.advance(2 * time.Minute)
			_ = tx.Rollback()
		}()
		select {
		case <-bAcquired:
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer func() { testHookBeforeConsumeWrite = nil }()

	got, err := a.Consume(ctx, r.ID, binding())
	returnedAt := c.now()
	<-bFinished
	if err == nil && !returnedAt.Before(r.ExpiresAt) {
		t.Fatalf("consume succeeded at %v, after expiry %v (consumed_at %v)", returnedAt, r.ExpiresAt, got.ConsumedAt)
	}
	if err != nil && !errors.Is(err, ErrExpired) {
		t.Fatalf("consume: %v", err)
	}
}

// sqliteHeader reports whether p is a SQLite database. securePaths creates
// an empty file at the checked path, so existence alone proves nothing.
func sqliteHeader(p string) bool {
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
		if _, err := s.Create(ctx, binding(), "x", time.Hour); err != nil {
			t.Fatalf("%q create: %v", name, err)
		}
		_ = s.Close()
		if !sqliteHeader(path) {
			t.Fatalf("%q: no database at the checked path", name)
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			n := e.Name()
			if n != name && n != name+"-wal" && n != name+"-shm" {
				t.Fatalf("%q: stray file %q (SQLite opened another path)", name, n)
			}
		}
		if parent, _ := os.ReadDir(filepath.Dir(dir)); len(parent) != 1 {
			t.Fatalf("%q: files outside the db directory: %v", name, parent)
		}
	}
}

func TestDotDotThroughSymlinkUsesOneResolvedPath(t *testing.T) {
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
	if _, err := s.Create(ctx, binding(), "x", time.Hour); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	osPath := filepath.Join(root, "other", "a.db")
	if !sqliteHeader(osPath) {
		t.Fatalf("database not at the OS-resolved path %s", osPath)
	}
	if _, err := os.Stat(filepath.Join(safe, "a.db")); err == nil {
		t.Fatal("a database file appeared at the lexically cleaned path")
	}
	if fi, _ := os.Stat(osPath); fi.Mode().Perm() != 0o600 {
		t.Fatalf("resolved db mode = %v", fi.Mode().Perm())
	}
}
