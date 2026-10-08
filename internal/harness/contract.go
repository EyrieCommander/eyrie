// Package harness defines the execution contract between Eyrie and the
// harnesses it dispatches work to (Codex exec, Claude Code, ZeroClaw ACP,
// Cursor cloud, ...). One Request is one attempt by one harness.
//
// The contract, in brief:
//
//   - A Request names the offering, harness and exact model, the workspace,
//     the request/task/attempt IDs, an optional approval binding, and
//     limits. Validate rejects an incomplete request before anything runs.
//   - An Adapter reports Capabilities (launch, resume, cancel, usage, native
//     approvals, models). Check refuses a request the adapter can't honour,
//     before dispatch; there is no silent model or capability fallback.
//   - Approval prompts stay runtime-native: the adapter surfaces the
//     runtime's own options, and a decision must be one of those option IDs,
//     forwarded verbatim. Eyrie never invents "approve" or "decline".
//   - Receipts are local truth. Run records a receipt before launch and a
//     terminal receipt after; the attempt's outcome is what Eyrie recorded,
//     not what the adapter claims. If a receipt can't be written, nothing
//     launches (before) or the outcome is unknown (after).
//
// This package defines the boundary and the lifecycle driver. Durable
// receipt storage (S2-04), the approvals store (EY-A2) and concrete
// adapters (S2-03, H-CC, H-ZC, H-CUR1) plug into it.
package harness

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Request is one execution attempt.
type Request struct {
	RequestID string // stable across retries of the same request
	TaskID    string
	AttemptID string // distinct per attempt; reusing one is refused

	OfferingID string // catalog offering the picker chose
	Harness    string // must equal the adapter's Name()
	Model      string // exact model; no fallback to a default

	Workspace string // absolute path the harness may use
	Prompt    string // bounded brief; never logged in receipts

	// ResumeSession continues a native session; requires Capabilities.Resume.
	// Empty means a fresh session.
	ResumeSession string

	// Approval binds this attempt to a scoped approval (approvals store).
	// Nil means no pre-approval; runtime prompts go to the Approver.
	Approval *ApprovalBinding

	Limits Limits

	// RequireUsage refuses adapters that can't report usage.
	RequireUsage bool
}

// ApprovalBinding ties an attempt to one approval record.
type ApprovalBinding struct {
	ApprovalID  string
	PayloadHash string // hash of the frozen request the approval covers
}

// Limits bound an attempt. Timeout is required: every attempt can be ended.
type Limits struct {
	Timeout time.Duration
	// CancelGrace is how long Run waits for the harness to confirm exit
	// after cancelling. Zero means DefaultCancelGrace.
	CancelGrace time.Duration
}

const DefaultCancelGrace = 10 * time.Second

// Capabilities is what an adapter can actually do. Missing means missing.
type Capabilities struct {
	Launch          bool
	Resume          bool
	Cancel          bool // can end a running attempt and confirm it ended
	Usage           bool // reports token/cost usage
	NativeApprovals bool // surfaces runtime approval prompts
	// Models the adapter can run. Empty means it can't say, and any
	// non-empty model is passed through for the runtime to accept or reject.
	Models []string
}

// Adapter is one harness.
type Adapter interface {
	Name() string
	Capabilities(ctx context.Context) (Capabilities, error)
	// Start launches the attempt and must respect ctx: Run cancels it if
	// the caller cancels or the timeout passes during startup. Returning an
	// error wrapping ErrStartAmbiguous means the runtime may have accepted
	// it; Run records unknown and the attempt must be reconciled before any
	// retry. Any other error means nothing is running.
	Start(ctx context.Context, req Request) (Handle, error)
}

// Handle is a running attempt.
type Handle interface {
	// NativeSession is the runtime's own session/thread ID, if any.
	NativeSession() string
	// Events streams progress and approval prompts; closed when the
	// attempt ends.
	Events() <-chan Event
	// Respond answers a native approval prompt with one of its options.
	Respond(ctx context.Context, promptID, optionID string) error
	// Cancel asks the runtime to stop. It must respect ctx; Run bounds it.
	// Wait reports whether the runtime actually stopped.
	Cancel(ctx context.Context) error
	// Wait blocks until the attempt ends or ctx is done. After a cancel,
	// exit counts as confirmed only if Wait returns nil or an error
	// wrapping ErrCancelled; any other error means the runtime's state is
	// unknown (e.g. the transport failed and work may continue).
	Wait(ctx context.Context) (Result, error)
}

// EventKind distinguishes events.
type EventKind string

const (
	EventProgress EventKind = "progress"
	EventApproval EventKind = "approval"
)

// Event is one thing the runtime reported.
type Event struct {
	Kind     EventKind
	Summary  string        // short, no transcript text
	Approval *NativePrompt // set for EventApproval
}

// NativePrompt is a runtime approval prompt, kept in the runtime's terms.
type NativePrompt struct {
	ID      string
	Action  string // e.g. "command", "file_change" as the runtime names it
	Detail  string // what the runtime asked about, for display
	Options []NativeOption
}

// NativeOption is one choice the runtime offered, with its native ID.
type NativeOption struct {
	ID    string
	Label string
}

// Offers reports whether optionID is one of the prompt's options.
func (p *NativePrompt) Offers(optionID string) bool {
	return p != nil && slices.ContainsFunc(p.Options, func(o NativeOption) bool { return o.ID == optionID })
}

// Result is what the harness reports at the end. Run turns it into a
// receipt; it is a claim, not the outcome. Summary and Err are runtime
// text and may echo the prompt or secrets, so they are never copied into
// receipts; only the caller sees them.
type Result struct {
	Success bool
	Summary string
	Model   string // model the runtime says it used; "" if not reported
	Usage   *Usage
	Err     string
}

// UsageKind says where a usage figure came from. Unknown is never zero.
type UsageKind string

const (
	UsageReported  UsageKind = "reported"
	UsageEstimated UsageKind = "estimated"
	UsageQuota     UsageKind = "quota"
	UsageUnknown   UsageKind = "unknown"
)

type Usage struct {
	Kind         UsageKind
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
}

var (
	ErrInvalidRequest   = errors.New("invalid execution request")
	ErrUnsupported      = errors.New("harness does not support this request")
	ErrStartAmbiguous   = errors.New("start outcome unknown")
	ErrDuplicateAttempt = errors.New("attempt already recorded")
	ErrOptionNotOffered = errors.New("approval option not offered by the runtime")
	// ErrCancelled is what Wait wraps when the runtime confirmed it stopped
	// because of Cancel.
	ErrCancelled = errors.New("attempt cancelled; runtime confirmed exit")
)

// Validate checks a request is complete. It does not consult any adapter.
func (r Request) Validate() error {
	var missing []string
	for name, v := range map[string]string{
		"request_id": r.RequestID, "task_id": r.TaskID, "attempt_id": r.AttemptID,
		"offering_id": r.OfferingID, "harness": r.Harness, "model": r.Model,
		"workspace": r.Workspace,
	} {
		if strings.TrimSpace(v) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return fmt.Errorf("%w: missing %s", ErrInvalidRequest, strings.Join(missing, ", "))
	}
	if !strings.HasPrefix(r.Workspace, "/") {
		return fmt.Errorf("%w: workspace must be absolute", ErrInvalidRequest)
	}
	if r.Limits.Timeout <= 0 {
		return fmt.Errorf("%w: limits.timeout is required", ErrInvalidRequest)
	}
	if r.Approval != nil && (r.Approval.ApprovalID == "" || r.Approval.PayloadHash == "") {
		return fmt.Errorf("%w: approval binding needs approval_id and payload_hash", ErrInvalidRequest)
	}
	return nil
}

// Check refuses a request this adapter can't honour. Every attempt has a
// timeout, so cancellation is always required.
func Check(name string, c Capabilities, r Request) error {
	var gaps []string
	if r.Harness != name {
		gaps = append(gaps, fmt.Sprintf("request is for harness %q, adapter is %q", r.Harness, name))
	}
	if !c.Launch {
		gaps = append(gaps, "launch")
	}
	if !c.Cancel {
		gaps = append(gaps, "cancel")
	}
	if r.ResumeSession != "" && !c.Resume {
		gaps = append(gaps, "resume")
	}
	if r.RequireUsage && !c.Usage {
		gaps = append(gaps, "usage reporting")
	}
	if len(c.Models) > 0 && !slices.Contains(c.Models, r.Model) {
		gaps = append(gaps, fmt.Sprintf("model %q", r.Model))
	}
	if len(gaps) > 0 {
		return fmt.Errorf("%w: %s", ErrUnsupported, strings.Join(gaps, "; "))
	}
	return nil
}
