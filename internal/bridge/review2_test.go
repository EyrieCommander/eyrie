package bridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Review 2, finding 2: a wake failure recorded after an interim reply must
// not demote the message from waiting back to pending or to failed.
func TestWakeFailureDoesNotDemoteInterimReply(t *testing.T) {
	old := wakeRetryDelays
	wakeRetryDelays = []time.Duration{50 * time.Millisecond}
	defer func() { wakeRetryDelays = old }()

	release := make(chan struct{})
	var calls atomic.Int32
	wake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			<-release // hold the first attempt until the interim reply lands
		}
		w.WriteHeader(503)
	}))
	defer wake.Close()

	store, _ := OpenStore(filepath.Join(t.TempDir(), "c.db"))
	defer store.Close()
	svc := NewService(store, Config{ChiefWakeURL: wake.URL, ChiefWakeKey: "k"})
	m, err := svc.Send("chief", "hi")
	if err != nil {
		t.Fatal(err)
	}
	// The chief got the wake (we are mid-response) and sends an interim line.
	time.Sleep(20 * time.Millisecond)
	if _, err := store.AddReply("chief", m.MessageID, "r0", "on it", false); err != nil {
		t.Fatal(err)
	}
	close(release) // attempt 1 now fails with 503
	svc.wg.Wait()

	got, _ := store.GetMessage(m.MessageID)
	if got.State != StateWaiting {
		t.Fatalf("state %s after interim reply + failed wake, want waiting", got.State)
	}
	if calls.Load() != 1 {
		t.Fatalf("wake retried %d times after the chief already replied", calls.Load()-1)
	}
}

// Review 2, finding 3: Retry must not be swallowed by a delivery loop that
// has decided its outcome but not yet released its slot.
func TestRetryIsNotStrandedByFinishingDelivery(t *testing.T) {
	old := wakeRetryDelays
	wakeRetryDelays = nil
	defer func() { wakeRetryDelays = old }()

	var fail atomic.Bool
	fail.Store(true)
	var calls atomic.Int32
	wake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(202)
	}))
	defer wake.Close()

	store, _ := OpenStore(filepath.Join(t.TempDir(), "c.db"))
	defer store.Close()
	svc := NewService(store, Config{ChiefWakeURL: wake.URL, ChiefWakeKey: "k"})
	m, _ := svc.Send("chief", "hi")
	svc.wg.Wait()
	if got, _ := store.GetMessage(m.MessageID); got.State != StateFailed {
		t.Fatalf("setup: state %s", got.State)
	}

	// Simulate the window the reviewer found: the slot is still held.
	svc.mu.Lock()
	svc.inflight[m.MessageID] = true
	svc.mu.Unlock()
	if _, err := svc.Retry(m.MessageID); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.GetMessage(m.MessageID); got.State != StateFailed {
		t.Fatalf("retry while a loop owns the message changed state to %s (would strand it)", got.State)
	}
	svc.mu.Lock()
	delete(svc.inflight, m.MessageID)
	svc.mu.Unlock()

	// Once the slot is free, Retry really delivers.
	fail.Store(false)
	if _, err := svc.Retry(m.MessageID); err != nil {
		t.Fatal(err)
	}
	svc.wg.Wait()
	if got, _ := store.GetMessage(m.MessageID); got.State != StateWaiting {
		t.Fatalf("after retry: %s, want waiting", got.State)
	}
}

// Review 2, finding 4: a line longer than any scanner buffer must not end
// the search early.
func TestSearchSurvivesVeryLongLine(t *testing.T) {
	f := newFixture(t)
	long := strings.Repeat("x", 2<<20) + " needle-on-long-line\nafter needle-after\n"
	write(t, filepath.Join(f.root, "long.txt"), long)
	rec := f.do(t, "GET", "/bridge/v1/fs/search?root=docs&q=needle-", testToken, nil)
	var out struct {
		Hits      []Hit `json:"hits"`
		Truncated bool  `json:"truncated"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	found := map[int]bool{}
	for _, h := range out.Hits {
		if h.Path == "long.txt" {
			found[h.Line] = true
			if len([]rune(h.Snippet)) > maxSnippetRunes {
				t.Fatalf("snippet %d runes", len([]rune(h.Snippet)))
			}
		}
	}
	if !found[1] || !found[2] {
		t.Fatalf("long-line matches missing: %+v (truncated=%v)", out.Hits, out.Truncated)
	}
}

// Review 2, finding 5: binary files are skipped entirely, name included.
func TestSearchSkipsBinaryByName(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.root, "blobby.txt"), []byte("blob text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := f.do(t, "GET", "/bridge/v1/fs/search?root=docs&q=blob", testToken, nil)
	var out struct {
		Hits []Hit `json:"hits"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	for _, h := range out.Hits {
		if h.Path == "blob.bin" {
			t.Fatalf("binary file returned by name: %+v", out.Hits)
		}
	}
	if len(out.Hits) == 0 {
		t.Fatal("text file with the term in its name was not found")
	}
}
