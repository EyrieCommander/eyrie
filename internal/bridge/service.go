package bridge

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
)

// Service is the Chief front door: it owns the durable store and the wake
// sender, and is shared by the bridge listener (replies, prompt fetch) and
// the loopback management API (Dan's UI). It holds no reference to the
// management server.
type Service struct {
	Store *Store
	waker *Waker // nil when chief_wake_url/key are not configured
	// Notify is a UI refresh hint (e.g. publish on the event bus). The UI
	// always re-reads the store; Notify carries no data that matters.
	Notify func(conversationID string)

	ctx      context.Context
	cancel   context.CancelFunc
	inflight sync.Map // message_id -> struct{}: one delivery loop per message
	wg       sync.WaitGroup
}

// ErrWakeNotConfigured: prompts can be stored but not sent.
var ErrWakeNotConfigured = errors.New("chief wake is not configured")

// ErrEmptyPrompt: nothing to send.
var ErrEmptyPrompt = errors.New("empty prompt")

// MaxPromptBytes bounds a prompt Dan can send from the UI.
const MaxPromptBytes = 64 << 10

// NewService wires a store and (optionally) the chief wake endpoint.
func NewService(store *Store, cfg Config) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{Store: store, ctx: ctx, cancel: cancel}
	if cfg.WakeConfigured() {
		s.waker = &Waker{URL: cfg.ChiefWakeURL, Key: cfg.ChiefWakeKey, Client: &http.Client{}}
	}
	return s
}

// WakeConfigured reports whether Send can reach the chief.
func (s *Service) WakeConfigured() bool { return s.waker != nil }

func (s *Service) notify(conv string) {
	if s.Notify != nil {
		s.Notify(conv)
	}
}

// Send saves the prompt (state pending) and then delivers it asynchronously.
// The returned message is already durable.
func (s *Service) Send(conversationID, text string) (*Message, error) {
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyPrompt
	}
	if s.waker == nil {
		return nil, ErrWakeNotConfigured
	}
	if conversationID == "" {
		conversationID = "chief"
	}
	m, err := s.Store.CreateMessage(conversationID, text)
	if err != nil {
		return nil, err
	}
	s.deliver(m)
	return m, nil
}

// Retry re-sends a failed or still-waiting message with the same message_id.
// It never resends automatically; only Dan's Retry button calls this.
func (s *Service) Retry(messageID string) (*Message, error) {
	if s.waker == nil {
		return nil, ErrWakeNotConfigured
	}
	m, err := s.Store.GetMessage(messageID)
	if err != nil {
		return nil, err
	}
	if m.State == StateAnswered {
		return m, nil
	}
	if err := s.Store.SetState(m.MessageID, StatePending, 0, ""); err != nil {
		return nil, err
	}
	m.State = StatePending
	s.deliver(m)
	return m, nil
}

// ResumePending restarts delivery of messages left pending by a restart.
func (s *Service) ResumePending() {
	if s.waker == nil {
		return
	}
	ms, err := s.Store.Unsent()
	if err != nil {
		slog.Warn("bridge: could not list unsent prompts", "error", err)
		return
	}
	for i := range ms {
		s.deliver(&ms[i])
	}
}

func (s *Service) deliver(m *Message) {
	if _, busy := s.inflight.LoadOrStore(m.MessageID, struct{}{}); busy {
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.inflight.Delete(m.MessageID)
		err := s.waker.Deliver(s.ctx, m, func(n int, err error) {
			if err == nil {
				_ = s.Store.SetState(m.MessageID, StateWaiting, n, "")
			} else {
				_ = s.Store.SetState(m.MessageID, StatePending, n, err.Error())
			}
			s.notify(m.ConversationID)
		})
		if err != nil && s.ctx.Err() == nil {
			cur, gerr := s.Store.GetMessage(m.MessageID)
			if gerr == nil && cur.State == StatePending {
				_ = s.Store.SetState(m.MessageID, StateFailed, cur.Attempts, err.Error())
			}
			slog.Warn("bridge: chief wake failed", "message_id", m.MessageID, "error", err)
			s.notify(m.ConversationID)
		}
	}()
}

// Close stops delivery loops and waits for them. Pending messages stay
// pending and resume on next start.
func (s *Service) Close() {
	s.cancel()
	s.wg.Wait()
}
