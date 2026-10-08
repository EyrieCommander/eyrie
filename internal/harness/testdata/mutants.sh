#!/bin/sh
# Mutation check for the harness contract's safety rules (H-01).
# Each mutant disables one rule; the package tests must fail for every one.
# Run from the repo root: sh internal/harness/testdata/mutants.sh
#
# It edits internal/harness/{run,contract}.go in place and restores them on
# exit (a copy is kept in a temp dir). Use a scratch checkout if the tree
# must stay read-only.
#
# A mutant counts as caught only if it builds and a test assertion fails
# ("--- FAIL" in the output). Build failures, panics/timeouts, missing
# anchors and a failing baseline are reported separately, and the script
# exits non-zero for any of them.
set -u
cd "$(git rev-parse --show-toplevel)" || exit 2
tmp=$(mktemp -d) || exit 2
cp internal/harness/run.go internal/harness/contract.go "$tmp/"
restore() { cp "$tmp/run.go" "$tmp/contract.go" internal/harness/; rm -rf "$tmp"; }
trap restore EXIT INT TERM

if ! go vet ./internal/harness >/dev/null 2>&1 || ! go test -count=1 -timeout 120s ./internal/harness >"$tmp/base.log" 2>&1; then
  echo "BASELINE FAILED: fix the unmutated tests first"; tail -20 "$tmp/base.log"; exit 2
fi
echo "baseline: ok"

bad=0
mut() { # file, name, find, replace [, find2, replace2]
  cp "$tmp/run.go" "$tmp/contract.go" internal/harness/
  python3 - "$1" "$3" "$4" "${5-}" "${6-}" <<'PY' || { echo "ANCHOR MISSING: $2"; bad=1; return; }
import sys
p = "internal/harness/" + sys.argv[1]
s = open(p).read()
pairs = [(sys.argv[2], sys.argv[3])]
if sys.argv[4]:
    pairs.append((sys.argv[4], sys.argv[5]))
for a, b in pairs:
    if a not in s:
        sys.exit(1)
    s = s.replace(a, b, 1)
open(p, "w").write(s)
PY
  if ! go vet ./internal/harness >"$tmp/m.log" 2>&1; then
    echo "BUILD FAILED (mutant invalid): $2"; bad=1; return
  fi
  if go test -count=1 -timeout 30s ./internal/harness >"$tmp/m.log" 2>&1; then
    echo "SURVIVED: $2"; bad=1
  elif grep -qE "^[[:space:]]*--- FAIL" "$tmp/m.log"; then
    # An assertion fired. (A later test may also hang on the mutant; the
    # assertion is what counts.)
    echo "caught:   $2 ($(grep -oE -- '--- FAIL: [A-Za-z0-9_]+' "$tmp/m.log" | sort -u | head -1 | cut -d' ' -f3))"
  elif grep -q "^panic: test timed out" "$tmp/m.log"; then
    echo "TIMEOUT (no assertion fired): $2"; bad=1
  else
    echo "ERROR (no assertion failure): $2"; tail -5 "$tmp/m.log"; bad=1
  fi
}
mut run.go "wait error after cancel counts as confirmed exit" \
  '		if w.err == nil || errors.Is(w.err, ErrCancelled) {' '		if true {'
mut run.go "unbounded Cancel on the main path" \
  '	go func() { cancelErr <- h.Cancel(cancelCtx) }()' '	_ = cancelCtx
	cancelErr <- h.Cancel(context.WithoutCancel(ctx))'
mut run.go "unbounded Cancel in receipt-failure cleanup" \
  '	go func() { _ = h.Cancel(cctx); close(done) }()' '	_ = h.Cancel(context.WithoutCancel(ctx))
	close(done)'
mut run.go "late approver decision sent after cancel" \
  '	if ctx.Err() != nil {
		return errLateDecision
	}' ''
mut run.go "approvals not stopped when cancel begins" \
  '	}
	stopApprovals()

	// Cancel first' '	}

	// Cancel first'
mut run.go "runtime error text stored in receipts" \
  '	return "runtime error (detail withheld from receipt)"' '	return err.Error()'
mut run.go "runtime summary stored in receipts" \
  '		return finish(ctx, rec, base, StateSucceeded, "runtime reported success")' '		return finish(ctx, rec, base, StateSucceeded, res.Summary)'
mut run.go "background workers outlive Run" \
  '	workCtx, stopWork := context.WithCancel(context.WithoutCancel(ctx))' '	workCtx, stopWork := context.WithCancel(context.WithoutCancel(ctx))
	stopWork = func() {}'
mut run.go "option the runtime did not offer is sent" \
  '	if !p.Offers(opt) {' '	if false {'
mut run.go "approve invented when no approver" \
  '	if appr == nil {
		return errNoApprover
	}' '	if appr == nil {
		return h.Respond(ctx, p.ID, "accept")
	}'
mut run.go "unconfirmed cancel reported cancelled" \
  '		return finish(ctx, rec, base, StateUnknown, reason)' '		return finish(ctx, rec, base, StateCancelled, reason)'
mut run.go "ambiguous start reported failed" \
  '			state = StateUnknown // may be running' '			state = StateFailed // may be running'
mut run.go "model mismatch accepted" \
  '	case res.Model != "" && res.Model != req.Model:' '	case false:'
mut run.go "missing usage reported as zero" \
  '		base.Usage = &Usage{Kind: UsageUnknown}' '		base.Usage = &Usage{Kind: UsageReported}'
mut run.go "launch without pre-launch receipt" \
  '	if err := record(ctx, rec, base, StateDispatching, ""); err != nil {
		return Receipt{}, fmt.Errorf("not dispatched: %w", err)
	}' '	_ = record(ctx, rec, base, StateDispatching, "")'
mut run.go "adapter claim trusted when terminal receipt fails" \
  '		return unknown(base, fmt.Errorf("record %s: %w", s, err))' '		return r, nil'
mut run.go "reused attempt id allowed" \
  '	if r.State == StateDispatching {' '	if false {'
mut run.go "timeout ignored (running attempt)" \
  '		case <-runCtx.Done():
			why = fmt.Sprintf' '		case <-make(chan struct{}):
			why = fmt.Sprintf'
mut contract.go "dispatch without cancel support" '	if !c.Cancel {' '	if false {'
mut contract.go "silent model fallback" \
  '	if len(c.Models) > 0 && !slices.Contains(c.Models, r.Model) {' '	if false {'
mut contract.go "resume without support" '	if r.ResumeSession != "" && !c.Resume {' '	if false {'
mut contract.go "no timeout required" '	if r.Limits.Timeout <= 0 {' '	if false {'
mut contract.go "wrong harness accepted" '	if r.Harness != name {' '	if false {'
mut run.go "respond error text stored in receipts (both layers)" \
  '		return errRespondFailed' '		return fmt.Errorf("respond: %v", err)' \
  '	return "approval failed"' '	return err.Error()'
mut run.go "approver error text stored in receipts (both layers)" \
  '		return errApproverError' '		return fmt.Errorf("approver: %v", err)' \
  '	return "approval failed"' '	return err.Error()'
mut run.go "already-cancelled request launches" \
  '	if err := ctx.Err(); err != nil {
		return Receipt{}, fmt.Errorf("not dispatched: %w", err)
	}' ''
mut run.go "caller cancel does not reach Start" \
  '	case <-ctx.Done():
		why = "cancelled by caller"
	case <-runCtx.Done():
		why = fmt.Sprintf("timeout after %s", req.Limits.Timeout)
	}
	cancelStart()' '	case <-make(chan struct{}):
		why = "cancelled by caller"
	case <-runCtx.Done():
		why = fmt.Sprintf("timeout after %s", req.Limits.Timeout)
	}
	cancelStart()'
mut run.go "hung Start waited on forever" \
  '	case <-timer.C:
		go func() { // late handle' '	case <-make(chan struct{}):
		go func() { // late handle'
mut run.go "late handle from hung Start left running" \
  '				boundedCancel(ctx, s.h, grace)' '				_ = s'
mut run.go "pump blocks on a stuck approver" \
  '				case <-approvalsCtx.Done():
					return
				}' '				}'
mut run.go "pump runs after a cancelled start" \
  '	if startWhy == "" {
		go pump()
	}' '	go pump()' \
  '	if ctx.Err() != nil {
		return errLateDecision // cancellation began; don'"'"'t even ask
	}' ''
# Not a mutant: the early stopApprovals() after a cancelled start and the
# pump gate are redundant (either alone blocks an answer). Removing both
# leaves only a sub-microsecond race against the later stopApprovals(),
# which no test hits reliably, so it is not counted either way.
mut run.go "cancel_requested recorded before Cancel is sent" \
  '	go func() { cancelErr <- h.Cancel(cancelCtx) }()

	if err := record(ctx, rec, base, StateCancelRequested, why); err != nil {
		return unknown(base, fmt.Errorf("record cancel_requested: %w", err))
	}' '	if err := record(ctx, rec, base, StateCancelRequested, why); err != nil {
		boundedCancel(ctx, h, grace)
		return unknown(base, fmt.Errorf("record cancel_requested: %w", err))
	}
	go func() { cancelErr <- h.Cancel(cancelCtx) }()'
mut run.go "receipt writes unbounded" \
  '	case <-wctx.Done():
		return errReceiptStalled' '	case <-make(chan struct{}):
		return errReceiptStalled'
mut run.go "start result trusted when cancellation also ready" \
  '		return s.h, cancelReason(ctx, runCtx, req), s.err' '		return s.h, "", s.err'
exit $bad
