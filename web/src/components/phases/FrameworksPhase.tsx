// Phase 1: Frameworks. The meaty piece.
//
// Inner 5-step flow: choose → install → configure → api key → launch.
// Sub-step status is derived from real data (filesystem, KeyVault) not from
// click-through. Tmux output from the persistent terminal is piped through
// terminalParser so successful install / configure / launch commands update
// the step status badges — but the user must click "continue" to advance.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { AlertTriangle, MessageSquare, RotateCcw } from "lucide-react";
import type { PhaseId } from "../OnboardingFlow";
import type { Framework, KeyEntry } from "../../lib/types";
import { fetchFrameworks, getFrameworkDetail, fetchFrameworkConfig, fetchKeys } from "../../lib/api";
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
import { loadSaved, saveSubStep, isApiKeyConfirmed, setApiKeyConfirmedFor } from "../../lib/onboardingStorage";

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

const VALID_STEPS: InnerStepId[] = ["choose", "install", "configure", "api_key", "launch"];

export default function FrameworksPhase({ onNavigate }: Props) {
  const [searchParams, setSearchParams] = useSearchParams();

  // Browseable framework list (for the choose step)
  const [frameworks, setFrameworks] = useState<Framework[]>([]);
  const [frameworksLoading, setFrameworksLoading] = useState(true);

  // The chosen framework's full detail — initialized from ?fw= param
  const [chosenId, setChosenId] = useState<string | null>(() => {
    const urlFw = searchParams.get("fw");
    if (urlFw && isSafeId(urlFw)) return urlFw;
    const saved = loadSaved();
    if (saved.fw && isSafeId(saved.fw)) return saved.fw;
    return null;
  });
  const [framework, setFramework] = useState<Framework | null>(null);
  const [rawConfig, setRawConfig] = useState<string>("");
  const [keys, setKeys] = useState<KeyEntry[]>([]);
  // Last error line captured from terminal output (for the error banner).
  const [lastError, setLastError] = useState<string | null>(null);

  // The api_key step requires explicit confirmation even when a key already
  // exists in the vault (it may be from a different framework's setup).
  // Keyed per framework so switching frameworks resets the confirmation.
  const [apiKeyConfirmed, setApiKeyConfirmed] = useState(() => {
    const id = isSafeId(chosenId) ? chosenId : null;
    return id ? isApiKeyConfirmed(id) : false;
  });

  // Gateway health status — fed back from the HealthCheck component in
  // LaunchStep. The launch step is only "complete" when the gateway is
  // healthy (or when the framework has no health_url).
  const [gatewayHealthy, setGatewayHealthy] = useState(false);

  // Which step the user is viewing — initialized from ?step= param.
  // Auto-advances when the current step transitions to "complete" (the
  // prevStepStatus effect below clears manualActive so firstIncomplete
  // picks the next step). Also advances on explicit timeline clicks.
  const [manualActive, setManualActive] = useState<InnerStepId | null>(() => {
    const urlStep = searchParams.get("step");
    if (urlStep && VALID_STEPS.includes(urlStep as InnerStepId)) return urlStep as InnerStepId;
    const saved = loadSaved();
    if (saved.step && VALID_STEPS.includes(saved.step as InnerStepId)) return saved.step as InnerStepId;
    return null;
  });

  // Sync fw/step to URL params and localStorage whenever they change.
  const syncParams = useCallback(
    (fw: string | null, step: InnerStepId | null) => {
      setSearchParams((prev) => {
        const next = new URLSearchParams(prev);
        if (fw) next.set("fw", fw);
        else next.delete("fw");
        if (step) next.set("step", step);
        else next.delete("step");
        return next;
      }, { replace: true });
      saveSubStep(fw, step);
    },
    [setSearchParams],
  );

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
        const cfg = await fetchFrameworkConfig(safeId);
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
    ? getFrameworkStatus(framework, null, apiKeyState)
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
    // The api_key step requires both a key in the vault AND explicit user
    // confirmation. This prevents auto-skipping when a key exists from
    // commander setup but the framework might need a different provider.
    const apiKeyDone = configureDone && (status.skipApiKey || (status.hasApiKey && apiKeyConfirmed));

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
    // Launch is "complete" only when the gateway is actually healthy (or
    // the framework has no health_url to check). This prevents "all set"
    // from showing when start gateway fails.
    const noHealthCheck = !framework?.health_url;
    const launchDone = status.isReady && apiKeyDone && (gatewayHealthy || noHealthCheck);
    const launch: InnerStepState = launchDone
      ? "complete"
      : apiKeyDone
        ? "current"
        : "pending";

    return { choose: "complete", install, configure, api_key: apiKey, launch };
  }, [chosenId, status, apiKeyConfirmed, gatewayHealthy, framework?.health_url]);

  // Auto-advance: when the current step completes or becomes unreachable,
  // clear the manual override so `firstIncomplete` picks the next step.
  // Also sync the new position to URL/localStorage.
  const prevStepStatus = useRef(stepStatus);
  useEffect(() => {
    if (manualActive) {
      const cur = stepStatus[manualActive];
      const prev = prevStepStatus.current[manualActive];
      // Step just completed → advance to the next incomplete step
      if (cur === "complete" && prev !== "complete") {
        setManualActive(null);
        syncParams(chosenId, null);
      }
      // Step is unreachable (e.g., framework was uninstalled) → snap back
      if (cur === "pending") {
        setManualActive(null);
        syncParams(chosenId, null);
      }
    }
    prevStepStatus.current = stepStatus;
  }, [manualActive, stepStatus, chosenId, syncParams]);

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
    setManualActive(null);
    setApiKeyConfirmed(isApiKeyConfirmed(id));
    setGatewayHealthy(false);
    syncParams(id, null);
  };

  const handleAddAnother = () => {
    setChosenId(null);
    setManualActive(null);
    setApiKeyConfirmed(false);
    setGatewayHealthy(false);
    syncParams(null, null);
  };

  // When the user is on the launch step and it's complete, show next-phase actions
  const showReadyActions =
    !!framework && active === "launch" && stepStatus.launch === "complete";

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
        onSelect={(step) => {
          setManualActive(step);
          syncParams(chosenId, step);
        }}
      />

      {/* Step panel */}
      <FrameworkStepPanel
        step={active}
        framework={framework}
        frameworks={frameworks}
        apiKey={apiKeyState}
        maskedApiKey={keys.find(k => k.provider === apiKeyState?.provider)?.masked_key}
        onChooseFramework={handleChoose}
        onRun={runInTerminal}
        onRefresh={() => {
          loadChosen();
          refreshKeys();
        }}
        onApiKeyConfirm={() => { setApiKeyConfirmed(true); if (safeId) setApiKeyConfirmedFor(safeId, true); }}
        apiKeyConfirmed={apiKeyConfirmed}
        onNavigateStep={(step) => {
          setManualActive(step);
          syncParams(chosenId, step);
        }}
        onHealthChange={setGatewayHealthy}
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
