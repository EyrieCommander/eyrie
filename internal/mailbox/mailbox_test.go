package mailbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	if _, err := s.Ack(ctx, m.ID, "w1"); err != nil {
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
	got, err := s.Ack(ctx, m.ID, "w1")
	if err != nil || got.State != Delivered || !got.DeliveredAt.Equal(c.t) {
		t.Fatalf("ack: %+v err=%v", got, err)
	}
	again, err := s.Ack(ctx, m.ID, "w1")
	if err != nil || !again.DeliveredAt.Equal(got.DeliveredAt) {
		t.Fatalf("second ack changed receipt: %+v err=%v", again, err)
	}
}

func TestExpiredLeaseIsReclaimedAndOldOwnerLosesAck(t *testing.T) {
	s, c, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	if _, err := s.Claim(ctx, "codex", "w1", time.Minute); err != nil {
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
	if _, err := s.Ack(ctx, "m1", "w1"); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("stale ack: %v, want ErrNotLeaseHolder", err)
	}
	if _, err := s.Release(ctx, "m1", "w1", "x"); !errors.Is(err, ErrNotLeaseHolder) {
		t.Fatalf("stale release: %v, want ErrNotLeaseHolder", err)
	}
	if _, err := s.Ack(ctx, "m1", "w2"); err != nil {
		t.Fatalf("current owner ack: %v", err)
	}
}

func TestLateAckOnUnreclaimedLeaseIsAccepted(t *testing.T) {
	s, c, _ := open(t, 3)
	mustEnqueue(t, s, "m1", "codex", "hello")
	_, _ = s.Claim(ctx, "codex", "w1", time.Minute)
	c.advance(2 * time.Minute)
	if m, err := s.Ack(ctx, "m1", "w1"); err != nil || m.State != Delivered {
		t.Fatalf("late ack: %+v err=%v", m, err)
	}
}

func TestReleaseBacksOffThenDeadLetters(t *testing.T) {
	s, c, _ := open(t, 2)
	mustEnqueue(t, s, "m1", "codex", "hello")

	_, _ = s.Claim(ctx, "codex", "w", time.Minute)
	m, err := s.Release(ctx, "m1", "w", "agent busy")
	if err != nil || m.State != Queued || m.LastError != "agent busy" || !m.NextAttemptAt.Equal(c.t.Add(time.Minute)) {
		t.Fatalf("first release: %+v err=%v", m, err)
	}
	if _, err := s.Claim(ctx, "codex", "w", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatalf("claim before backoff: %v, want ErrNotFound", err)
	}
	c.advance(time.Minute)
	if _, err := s.Claim(ctx, "codex", "w", time.Minute); err != nil {
		t.Fatalf("claim after backoff: %v", err)
	}
	m, err = s.Release(ctx, "m1", "w", "agent busy")
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
