// Macro timeline for the unified onboarding flow.
// Renders the 3 macro phases (commander → frameworks → projects) as a single row.
// The active phase shows a ring; completed phases show a check; pending phases are
// greyed out and not clickable.
//
// The inner 5-step timeline for phase 1 (choose → install → configure → api key →
// launch) will be rendered BELOW the macro timeline by FrameworksPhase when it's
// active — the macro timeline stays one row for all phases.

import type { PhaseId, PhaseStatus } from "./OnboardingFlow";

const PHASES: { id: PhaseId; label: string; n: number }[] = [
  { id: "commander", label: "commander", n: 1 },
  { id: "frameworks", label: "frameworks", n: 2 },
  { id: "projects", label: "projects", n: 3 },
];

interface Props {
  active: PhaseId;
  status: PhaseStatus;
  onSelect: (id: PhaseId) => void;
}

export default function MacroTimeline({ active, status, onSelect }: Props) {
  return (
    <div className="flex items-center gap-3 rounded border border-border bg-surface px-4 py-3">
      {PHASES.map((phase, i) => {
        const state = status[phase.id];
        const isActive = active === phase.id;
        const isComplete = state === "complete";
        const isPending = state === "pending";
        const clickable = !isPending;

        return (
          <div key={phase.id} className="flex items-center gap-3 flex-1">
            <button
              type="button"
              data-phase={phase.id}
              disabled={!clickable}
              onClick={() => clickable && onSelect(phase.id)}
              title={isPending ? "complete previous steps first" : undefined}
              className={`flex items-center gap-2 rounded px-2 py-1 text-xs transition-colors ${
                isActive
                  ? "text-text"
                  : isComplete
                    ? "text-green hover:bg-green/10"
                    : "text-text-muted cursor-not-allowed"
              }`}
            >
              <span
                className={`flex h-5 w-5 items-center justify-center rounded-full text-[10px] font-bold ${
                  isComplete
                    ? "bg-green/20 text-green"
                    : isActive
                      ? "bg-accent/20 text-accent"
                      : "bg-text-muted/20 text-text-muted"
                }`}
              >
                {isComplete ? "\u2713" : phase.n}
              </span>
              <span className="font-medium">{phase.label}</span>
            </button>
            {i < PHASES.length - 1 && (
              <div
                className={`h-px flex-1 transition-colors ${
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
