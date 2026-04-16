// Phase 0: Commander setup.
//
// The commander backend (internal/commander/) lives on feature/eyrie-commander
// and hasn't been merged into this branch yet (step 4 of the plan does that).
// Until then this is a static "ready" placeholder. When the merge lands, wire
// in a health-check fetch to /api/commander/history — if it succeeds, show
// provider/model + history count; if it fails (e.g. missing OPENROUTER_API_KEY)
// switch to a yellow "commander isn't configured" card.

export default function CommanderPhase() {
  return (
    <div className="rounded border border-border bg-surface p-6 space-y-4">
      <div className="flex items-center gap-3">
        <div className="flex h-8 w-8 items-center justify-center rounded-full bg-green/20 text-green text-sm">
          &#10003;
        </div>
        <div>
          <div className="text-sm font-semibold text-text">Eyrie is your commander</div>
          <p className="text-xs text-text-muted mt-0.5">
            Ask me anything via the chat panel &rarr;
          </p>
        </div>
      </div>
      <p className="text-[10px] text-text-muted border-t border-border pt-3">
        (Commander backend merges in step 4 of the unified-onboarding plan; this
        card becomes live then — provider/model info, history count, clear button.)
      </p>
    </div>
  );
}
