#!/bin/sh
# Mutation check for the harness contract's safety rules (H-01).
# Each mutant disables one rule; the package tests must fail for every one.
# Run from the repo root: sh internal/harness/testdata/mutants.sh
# Restores the sources afterwards; exits non-zero if any mutant survives.
set -u
cd "$(git rev-parse --show-toplevel)" || exit 2
tmp=$(mktemp -d) || exit 2
cp internal/harness/run.go internal/harness/contract.go "$tmp/"
restore() { cp "$tmp/run.go" "$tmp/contract.go" internal/harness/; rm -rf "$tmp"; }
trap restore EXIT INT TERM
survived=0
mut() { # file, name, python find, python replace
  cp "$tmp/run.go" "$tmp/contract.go" internal/harness/
  python3 - "$1" "$3" "$4" <<'PY' || { echo "MUTANT ANCHOR MISSING: $2"; survived=1; return; }
import sys
p = "internal/harness/" + sys.argv[1]
s = open(p).read()
a, b = sys.argv[2], sys.argv[3]
if a not in s:
    sys.exit(1)
open(p, "w").write(s.replace(a, b, 1))
PY
  if go test -count=1 -timeout 60s ./internal/harness >/dev/null 2>&1; then
    echo "SURVIVED: $2"; survived=1
  else
    echo "caught:   $2"
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
		return fmt.Errorf("prompt %q: decision arrived' '	if false {
		return fmt.Errorf("prompt %q: decision arrived'
mut run.go "approvals not stopped when cancel begins" \
  '	stopApprovals()

	// Cancel shows' '
	// Cancel shows'
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
		return fmt.Errorf' '	if appr == nil {
		return h.Respond(ctx, p.ID, "accept")
	}
	if false {
		return fmt.Errorf'
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
mut run.go "timeout ignored" \
  '	case <-runCtx.Done():
		why = fmt.Sprintf' '	case <-make(chan struct{}):
		why = fmt.Sprintf'
mut contract.go "dispatch without cancel support" '	if !c.Cancel {' '	if false {'
mut contract.go "silent model fallback" \
  '	if len(c.Models) > 0 && !slices.Contains(c.Models, r.Model) {' '	if false {'
mut contract.go "resume without support" '	if r.ResumeSession != "" && !c.Resume {' '	if false {'
mut contract.go "no timeout required" '	if r.Limits.Timeout <= 0 {' '	if false {'
mut contract.go "wrong harness accepted" '	if r.Harness != name {' '	if false {'
exit $survived
