package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

// maxWakeText is the cap on prompt text carried in the wake payload. Longer
// prompts are truncated and flagged; the chief fetches the full text from
// GET /bridge/v1/prompts/{message_id}.
const maxWakeText = 4000

// wakeRetryDelays: first attempt immediately, then retry after each delay.
// A var so tests can shorten it.
var wakeRetryDelays = []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}

const wakeAttemptTimeout = 10 * time.Second

// WakePayload is the body POSTed to chief_wake_url.
type WakePayload struct {
	Source         string `json:"source"`
	Type           string `json:"type"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	Text           string `json:"text"`
	Truncated      bool   `json:"truncated,omitempty"`
	TS             string `json:"ts"`
}

// BuildWake makes the payload for m, truncating text at maxWakeText runes.
func BuildWake(m *Message) WakePayload {
	p := WakePayload{
		Source:         "eyrie",
		Type:           "eyrie.prompt",
		ConversationID: m.ConversationID,
		MessageID:      m.MessageID,
		Text:           m.Text,
		TS:             m.TS.UTC().Format(time.RFC3339),
	}
	if utf8.RuneCountInString(m.Text) > maxWakeText {
		p.Text = string([]rune(m.Text)[:maxWakeText])
		p.Truncated = true
	}
	return p
}

// Waker sends prompts to the chief's wake endpoint.
type Waker struct {
	URL    string
	Key    string
	Client *http.Client
}

// sendOnce POSTs the payload. Errors never include the key or URL query.
func (w *Waker) sendOnce(ctx context.Context, p WakePayload) error {
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, wakeAttemptTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("wake request: invalid chief_wake_url")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+w.Key)
	req.Header.Set("X-Automation-Key", w.Key)
	c := w.Client
	if c == nil {
		c = http.DefaultClient
	}
	resp, err := c.Do(req)
	if err != nil {
		// *url.Error embeds the full URL; report only the cause class.
		if ctx.Err() != nil {
			return fmt.Errorf("wake: timed out")
		}
		return fmt.Errorf("wake: connection failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("wake: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Deliver tries once plus len(wakeRetryDelays) retries, all with the same
// message_id. onAttempt is called after each attempt with the attempt number
// and its error (nil on success). Returns the last error, nil on success.
func (w *Waker) Deliver(ctx context.Context, m *Message, onAttempt func(n int, err error)) error {
	p := BuildWake(m)
	var err error
	for i := 0; i <= len(wakeRetryDelays); i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wakeRetryDelays[i-1]):
			}
		}
		err = w.sendOnce(ctx, p)
		if onAttempt != nil {
			onAttempt(i+1, err)
		}
		if err == nil {
			return nil
		}
	}
	return err
}
