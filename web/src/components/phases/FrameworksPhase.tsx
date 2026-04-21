// Phase 1: Frameworks. The meaty piece.
//
// Inner 5-step flow: choose → install → configure → api key → launch.
// Sub-step status is derived from real data (filesystem, KeyVault) not from
// click-through. Tmux output from the persistent terminal is piped through
// terminalParser so successful install / configure / launch commands advance
// the timeline without waiting on the 5-second filesystem polling loop.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { AlertTriangle, MessageSquare, RotateCcw } from "lucide-react";
import type { PhaseId } from "../OnboardingFlow";
import type { Framework, InstallProgress, KeyEntry } from "../../lib/types";
import { fetchFrameworks, getFrameworkDetail, fetchAgentConfig, fetchKeys } from "../../lib/api";
import {
  deriveApiKeyState,
  findProviderField,
  getFrameworkStatus,
} from "../../lib/frameworkStatus";
import { matchLine } from "../../lib/terminalParser";
import Terminal, { TerminalHandle } from "../Terminal";
import FrameworkProgressTimeline, {
  INNER_STEPS,
  InnerStepId,
  InnerStepState,
} from "../FrameworkProgressTimeline";
import FrameworkStepPanel, { askCommander } from "../FrameworkStepPanel";

/**
 * Pull a provider value out of a raw config string. We don't try to fully
 * parse TOML/YAML here — a line-anchored regex catches the common shape
 * `provider = "openrouter"` (TOML), `provider: openrouter` (YAML), or
 * `"provider": "openrouter"` (JSON). Returns null if nothing matches.
 */
function extractProviderFromRaw(raw: string, fieldKey: string): string | null {
  if (!raw) return null;
  const baseName = fieldKey.split(".").pop() || fieldKey;
  const patterns = [
    new RegExp(`^\\s*${baseName}\\s*=\\s*["']([^"']+)["']`, "m"), // TOML
    new RegExp(`"${baseName}"\\s*:\\s*"([^"]+)"`, "m"), // JSON
    new RegExp(`^\\s*${baseName}:\\s*["']?([^\\s"'#]+)["']?`, "m"), // YAML
  ];
  for (const re of patterns) {
    const m = re.exec(raw);
    if (m) return m[1];
  }
  return null;
}

/** Find the first sub-step whose state is "current". Fallback to "choose". */
function firstIncomplete(
  status: Record<InnerStepId, InnerStepState>,
): InnerStepId {
  for (const s of INNER_STEPS) {
    if (status[s.id] === "current" || status[s.id] === "error") return s.id;
  }
  // All complete → rest on "launch"
  return "launch";
}

function isSafeId(id: string | null | undefined): id is string {
  return !!id && /^[a-zA-Z0-9_-]+$/.test(id);
}

interface Props {
  onNavigate?: (phase: PhaseId) => void;
}

export default function FrameworksPhase({ onNavigate }: Props) {
  // Browseable framework list (for the choose step)
  const [frameworks, setFrameworks] = useState<Framework[]>([]);
  const [frameworksLoading, setFrameworksLoading] = useState(true);

  // The chosen framework's full detail
  const [chosenId, setChosenId] = useState<string | null>(null);
  const [framework, setFramework] = useState<Framework | null>(null);
  const [rawConfig, setRawConfig] = useState<string>("");
  const [keys, setKeys] = useState<KeyEntry[]>([]);
  // installProgress stays unused for now — SSE is not re-wired here. Filesystem
  // polling + tmux output parsing are the two signals that drive transitions.
  const [installProgress] = useState<InstallProgress | null>(null);

  // Last error line captured from terminal output (for the error banner).
  const [lastError, setLastError] = useState<string | null>(null);

  // Manual override: user clicked a specific step. Cleared automatically when
  // that step transitions to "complete" (so auto-advance resumes).
  const [manualActive, setManualActive] = useState<InnerStepId | null>(null);

  const termRef = useRef<TerminalHandle>(null);
  const safeId = isSafeId(chosenId) ? chosenId : null;

  // Initial + after-install refetch of framework list. Keys are fetched here
  // and after every successful save from ApiKeyForm.
  const refreshFrameworks = useCallback(async () => {
    try {
      setFrameworksLoading(true);
      const list = await fetchFrameworks();
      setFrameworks(list);
    } finally {
      setFrameworksLoading(false);
    }
  }, []);

  const refreshKeys = useCallback(async () => {
    try {
      const entries = await fetchKeys();
      setKeys(entries);
    } catch {
      setKeys([]);
    }
  }, []);

  const loadChosen = useCallback(async () => {
    if (!safeId) {
      setFramework(null);
      setRawConfig("");
      return;
    }
    let fw: Framework | null = null;
    try {
      fw = await getFrameworkDetail(safeId);
      setFramework(fw);
    } catch {
      setFramework(null);
    }
    // Only fetch config when the framework is installed + configured.
    // Before that, the config endpoint 404s every poll cycle.
    if (fw?.installed && fw?.configured) {
      try {
        const cfg = await fetchAgentConfig(safeId);
        setRawConfig(cfg.content);
      } catch {
        setRawConfig("");
      }
    } else {
      setRawConfig("");
    }
  }, [safeId]);

  useEffect(() => {
    refreshFrameworks();
    refreshKeys();
  }, [refreshFrameworks, refreshKeys]);
  useEffect(() => {
    loadChosen();
  }, [loadChosen]);

  // Gentle filesystem poll while the chosen framework is in a transitional
  // state (so we catch install/configure completion even if the tmux parser
  // misses a marker). Skip once the framework is fully ready.
  const needsPolling =
    framework && (!framework.installed || !framework.configured);
  useEffect(() => {
    if (!safeId || !needsPolling) return;
    const id = setInterval(() => {
      loadChosen();
      refreshKeys();
    }, 5000);
    return () => clearInterval(id);
  }, [safeId, needsPolling, loadChosen, refreshKeys]);

  // Derive provider + api-key state
  const providerField = framework ? findProviderField(framework) : null;
  const providerValue = providerField
    ? extractProviderFromRaw(rawConfig, providerField.key) ??
      (typeof providerField.default === "string" ? providerField.default : null)
    : null;
  const apiKeyState = deriveApiKeyState(providerValue, keys);
  const status = framework
    ? getFrameworkStatus(framework, installProgress, apiKeyState)
    : null;

  // Derive sub-step statuses
  const stepStatus = useMemo<Record<InnerStepId, InnerStepState>>(() => {
    if (!chosenId) {
      return {
        choose: "current",
        install: "pending",
        configure: "pending",
        api_key: "pending",
        launch: "pending",
      };
    }
    if (!status) {
      return {
        choose: "complete",
        install: "current",
        configure: "pending",
        api_key: "pending",
        launch: "pending",
      };
    }

    // Steps are sequential: a step can only be "complete" or "current" if
    // all prior steps are done. This prevents api_key showing green when
    // configure isn't finished (e.g., key already exists from commander setup).
    const installDone = status.isInstalled;
    const configureDone = installDone && status.isConfigured;
    const apiKeyDone = configureDone && (status.hasApiKey || status.skipApiKey);

    const install: InnerStepState = installDone
      ? "complete"
      : status.isError
        ? "error"
        : "current";
    const configure: InnerStepState = configureDone
      ? "complete"
      : installDone
        ? "current"
        : "pending";
    const apiKey: InnerStepState = !configureDone
      ? "pending"
      : status.skipApiKey
        ? "skipped"
        : apiKeyDone
          ? "complete"
          : "current";
    const launch: InnerStepState = status.isReady && apiKeyDone
      ? "complete"
      : apiKeyDone
        ? "current"
        : "pending";

    return { choose: "complete", install, configure, api_key: apiKey, launch };
  }, [chosenId, status]);

  // If the user is watching a step that just transitioned to complete
  // (e.g., install finished while they were on the install panel), clear
  // the override so auto-advance kicks in. But don't clear it if the
  // user navigated to an already-complete step to review it.
  const prevStepStatus = useRef(stepStatus);
  useEffect(() => {
    if (
      manualActive &&
      stepStatus[manualActive] === "complete" &&
      prevStepStatus.current[manualActive] !== "complete"
    ) {
      setManualActive(null);
    }
    prevStepStatus.current = stepStatus;
  }, [manualActive, stepStatus]);

  const active: InnerStepId = manualActive ?? firstIncomplete(stepStatus);

  // Terminal output parser → refetch on match + capture errors
  const activeRef = useRef(active);
  activeRef.current = active;
  const handleOutput = useCallback(
    (line: string) => {
      // Capture error lines for the error banner
      if (/^error[\s:[]/i.test(line) || /^ERROR\b/.test(line)) {
        setLastError(line.length > 200 ? line.slice(0, 200) + "…" : line);
      }
      const step = activeRef.current;
      if (step === "choose" || step === "api_key") return;
      const m = matchLine(line, step);
      if (m) {
        setLastError(null); // Clear error on success
        loadChosen();
        refreshKeys();
      }
    },
    [loadChosen, refreshKeys],
  );

  // Convenience: run a command in the tmux terminal
  const runInTerminal = useCallback((cmd: string) => {
    termRef.current?.runCommand(cmd);
  }, []);

  const handleChoose = (id: string) => {
    setChosenId(id);
    setManualActive(null); // let auto-advance to install
  };

  const handleAddAnother = () => {
    setChosenId(null);
    setManualActive(null);
  };

  // When the chosen framework reaches ready, offer next-step affordances
  const showReadyActions =
    !!framework && status?.isReady && !manualActive;

  return (
    <div className="space-y-4">
      {/* Header for chosen framework (or "pick one") */}
      {framework ? (
        <div className="flex items-center gap-2 text-xs">
          <span className="text-text-muted">framework:</span>
          <span className="font-semibold text-text">{framework.name}</span>
          {status?.badge && (
            <span
              className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${
                status.badge.color === "green"
                  ? "bg-green/10 text-green"
                  : status.badge.color === "red"
                    ? "bg-red/10 text-red"
                    : status.badge.color === "blue"
                      ? "bg-blue/10 text-blue"
                      : "bg-yellow/10 text-yellow"
              }`}
            >
              {status.badge.label}
            </span>
          )}
          <div className="flex-1" />
          <button
            onClick={handleAddAnother}
            className="text-[10px] text-text-muted hover:text-text transition-colors"
          >
            + framework
          </button>
        </div>
      ) : frameworksLoading ? (
        <div className="text-xs text-text-muted">loading frameworks…</div>
      ) : null}

      {/* Inner 5-step timeline */}
      <FrameworkProgressTimeline
        status={stepStatus}
        active={active}
        onSelect={setManualActive}
      />

      {/* Step panel */}
      <FrameworkStepPanel
        step={active}
        framework={framework}
        frameworks={frameworks}
        apiKey={apiKeyState}
        onChooseFramework={handleChoose}
        onRun={runInTerminal}
        onRefresh={() => {
          loadChosen();
          refreshKeys();
        }}
        safeId={safeId}
      />

      {/* Error banner */}
      {status?.isError && lastError && (
        <div className="rounded border border-red/30 bg-red/5 px-4 py-3 space-y-2">
          <div className="flex items-center gap-2 text-xs">
            <AlertTriangle className="h-3.5 w-3.5 text-red shrink-0" />
            <span className="font-medium text-red">install failed</span>
            <span className="text-text-muted truncate">&mdash; {lastError}</span>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => askCommander(`Help me resolve this install error for ${framework?.name}: ${lastError}`)}
              className="flex items-center gap-1.5 rounded bg-purple px-3 py-1.5 text-xs font-medium text-white hover:bg-purple/80 transition-colors"
            >
              <MessageSquare className="h-3 w-3" />
              ask the commander
            </button>
            <button
              onClick={() => safeId && runInTerminal(`eyrie install ${safeId} -y`)}
              className="flex items-center gap-1 rounded border border-red/30 px-3 py-1.5 text-xs font-medium text-red hover:bg-red/5 transition-colors"
            >
              <RotateCcw className="h-3 w-3" />
              retry install
            </button>
          </div>
        </div>
      )}

      {/* "ready" affordance */}
      {showReadyActions && (
        <div className="rounded border border-green/30 bg-green/5 px-4 py-3 space-y-2">
          <div className="text-xs text-text">
            <span className="font-medium text-green">&#10003; all set</span> &mdash; {framework!.name} is ready.
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={handleAddAnother}
              className="text-xs text-text-muted hover:text-text transition-colors"
            >
              set up another framework
            </button>
            <span className="text-border">|</span>
            <button
              onClick={() => onNavigate?.("projects")}
              className="text-xs font-medium text-accent hover:text-accent/80 transition-colors"
            >
              continue to projects &rarr;
            </button>
          </div>
        </div>
      )}

      {/* Persistent tmux terminal — hidden on "choose" since there's
          nothing to run yet. Keyed on framework id so switching
          frameworks re-connects to that framework's dedicated session. */}
      {active !== "choose" && <div className="h-[320px]">
        <Terminal
          key={`fw-shell-${safeId ?? "none"}`}
          ref={termRef}
          agentName={safeId || "shell"}
          useShell
          inline
          session={safeId ? `eyrie-${safeId}` : undefined}
          onOutput={handleOutput}
        />
      </div>}
    </div>
  );
}
