// Inner 5-step progress timeline for phase 1 (frameworks).
//
// choose → install → configure → api key → launch
//
// Sub-step completion is derived from state (filesystem + KeyVault), not
// click-through. The completion check is:
//   choose:    a framework has been selected (UI state, passed in)
//   install:   framework.installed === true (binary on disk)
//   configure: framework.configured === true (config file on disk)
//   api key:   KeyVault has a key for the provider OR provider is local
//   launch:    framework.health endpoint returns 200 OR chat is running
//
// Clicking any completed sub-step or the first incomplete one ("continue")
// navigates to it. Clicking beyond the first incomplete step is blocked
// (the badge shows "complete previous steps first").

import type { SubStepId } from "../lib/terminalParser";

/** "choose" isn't in terminalParser's SubStepId because it has no tmux patterns,
 *  but it still needs to participate in the inner timeline.  */
export type InnerStepId = "choose" | SubStepId;

export type InnerStepState = "complete" | "current" | "pending" | "error" | "skipped";

export interface InnerStepMeta {
  id: InnerStepId;
  label: string;
  n: number;
}

export const INNER_STEPS: InnerStepMeta[] = [
  { id: "choose", label: "choose", n: 1 },
  { id: "install", label: "install", n: 2 },
  { id: "configure", label: "configure", n: 3 },
  { id: "api_key", label: "api key", n: 4 },
  { id: "launch", label: "launch", n: 5 },
];

interface Props {
  /** Per-step state. Caller computes from real data. */
  status: Record<InnerStepId, InnerStepState>;
  /** Currently focused step (what the step panel is showing). */
  active: InnerStepId;
  onSelect: (id: InnerStepId) => void;
  /** Optional: override the label of the currently-active step
   *  (e.g. show "installing — binary phase" while install is running). */
  activeLabelOverride?: string;
}

export default function FrameworkProgressTimeline({
  status,
  active,
  onSelect,
  activeLabelOverride,
}: Props) {
  return (
    <div
      className="flex items-center gap-2 rounded border border-border bg-surface px-3 py-2"
      role="list"
      aria-label="Framework setup progress"
    >
      {INNER_STEPS.map((step, i) => {
        const state = status[step.id];
        const isActive = active === step.id;
        const isComplete = state === "complete" || state === "skipped";
        const isError = state === "error";
        const isPending = state === "pending";
        // Clickable when the step is already done OR it's the "first-incomplete"
        // step (which will be state === "current" thanks to the caller). This
        // keeps pending steps ahead of the frontier blocked.
        const clickable = isComplete || state === "current" || isError;

        return (
          <div key={step.id} className="flex items-center gap-2 flex-1 min-w-0">
            <button
              type="button"
              role="listitem"
              disabled={!clickable}
              onClick={() => clickable && onSelect(step.id)}
              title={
                isPending
                  ? "complete previous steps first"
                  : state === "skipped"
                    ? "no action needed for this step"
                    : undefined
              }
              className={`flex items-center gap-1.5 rounded px-1.5 py-1 text-[11px] min-w-0 transition-colors ${
                isActive
                  ? "text-text"
                  : isComplete
                    ? "text-green hover:bg-green/10"
                    : isError
                      ? "text-red hover:bg-red/10"
                      : "text-text-muted cursor-not-allowed"
              }`}
            >
              <span
                className={`flex h-4 w-4 items-center justify-center rounded-full text-[9px] font-bold shrink-0 ${
                  state === "skipped"
                    ? "bg-text-muted/20 text-text-muted"
                    : isComplete
                      ? "bg-green/20 text-green"
                      : isError
                        ? "bg-red/20 text-red"
                        : isActive
                          ? "bg-accent/20 text-accent"
                          : "bg-text-muted/20 text-text-muted"
                }`}
              >
                {isError
                  ? "!"
                  : state === "skipped"
                    ? "\u00D7"
                    : isComplete
                      ? "\u2713"
                      : step.n}
              </span>
              <span className="truncate">
                {isActive && activeLabelOverride ? activeLabelOverride : step.label}
              </span>
            </button>
            {i < INNER_STEPS.length - 1 && (
              <div
                className={`h-px flex-1 min-w-[12px] transition-colors ${
                  isComplete ? "bg-green/30" : "bg-border"
                }`}
              />
            )}
          </div>
        );
      })}
    </div>
  );
}
