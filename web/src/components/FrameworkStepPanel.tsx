// Active-sub-step content panel for phase 1 (frameworks).
//
// Renders different content based on which sub-step is active. Layout pattern
// across all sub-steps: shared header ("step N of 5 · <label>" + description)
// followed by step-specific actions.
//
// All commands run through the parent's tmux terminal (via onRun), so the
// terminal output appears below the panel and the parser can detect success
// markers to update step status and auto-advance the timeline.

import { useEffect, useRef, useState } from "react";
import { Download, Terminal as TerminalIcon, FileEdit, Play, Check, MessageSquare } from "lucide-react";
import FrameworkCard from "./FrameworkCard";
import ApiKeyForm from "./ApiKeyForm";
import ConfigFieldsForm from "./ConfigFieldsForm";
import type { Framework } from "../lib/types";
import type { ApiKeyState } from "../lib/frameworkStatus";
import type { InnerStepId } from "./FrameworkProgressTimeline";
import { shellQuote } from "../lib/shell";
import { COMMANDER_PREFILL_EVENT } from "../lib/events";

interface Props {
  step: InnerStepId;
  /** The currently-chosen framework (null only in "choose" step). */
  framework: Framework | null;
  /** All installable frameworks (for "choose"). */
  frameworks: Framework[];
  /** Current API-key state for the selected framework's provider. */
  apiKey: ApiKeyState | null;
  /** Caller picks a framework (from "choose" step). */
  onChooseFramework: (id: string) => void;
  /** Paste a command into the tmux terminal and press enter. */
  onRun: (cmd: string) => void;
  /** Refetch framework detail + keys (called after edits). */
  onRefresh: () => void;
  /** The masked key value for display (e.g. "sk-o***9505"). */
  maskedApiKey?: string;
  /** User confirms the api key step (even when a key already exists). */
  onApiKeyConfirm?: () => void;
  /** Whether the user has confirmed the api key step. */
  apiKeyConfirmed?: boolean;
  /** Navigate to a different inner step (e.g. back to configure). */
  onNavigateStep?: (step: InnerStepId) => void;
  /** Reports gateway health status back to the parent for step gating. */
  onHealthChange?: (healthy: boolean) => void;
  /** Sanitised framework id safe to interpolate into shell commands. */
  safeId: string | null;
}

export default function FrameworkStepPanel(props: Props) {
  const { step } = props;

  return (
    <div className="rounded border border-border bg-surface p-4 space-y-4">
      {step === "choose" && <ChooseStep {...props} />}
      {step === "install" && <InstallStep {...props} />}
      {step === "configure" && <ConfigureStep {...props} />}
      {step === "api_key" && <ApiKeyStep {...props} />}
      {step === "launch" && <LaunchStep {...props} />}
    </div>
  );
}

// ── step 1: choose ──────────────────────────────────────────────────────
function ChooseStep({ frameworks, onChooseFramework }: Props) {
  return (
    <div className="space-y-3">
      <StepHeader n={1} label="choose a framework" />
      <p className="text-xs text-text-secondary">
        Which agent runtime do you want to start with? You can always add more later.
      </p>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {frameworks.map((fw) => (
          <FrameworkCard key={fw.id} framework={fw} onSelect={onChooseFramework} />
        ))}
      </div>
      <AskCommander question="what framework should I pick?" label="not sure which to pick?" />
    </div>
  );
}

// ── step 2: install ─────────────────────────────────────────────────────
function InstallStep({ framework, safeId, onRun }: Props) {
  if (!framework || !safeId) return <WaitingForFramework />;

  const handleAuto = () => onRun(`eyrie install ${safeId} -y`);
  const handleManual = () => {
    const cmd = framework.install_cmd || `eyrie install ${safeId}`;
    onRun(cmd);
  };

  return (
    <div className="space-y-3">
      <StepHeader n={2} label="install binary" />
      <p className="text-xs text-text-secondary">
        Get {framework.name} onto your machine. Either option works — pick whichever fits
        your workflow.
      </p>

      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        <OptionCard
          icon={<Download className="h-3.5 w-3.5" />}
          title="install via eyrie"
          hint={<code className="text-[10px]">$ eyrie install {safeId} -y</code>}
          description="Downloads the binary and wires it into Eyrie's discovery system."
          onAction={handleAuto}
          actionLabel="start install"
          actionStyle="primary"
        />
        <OptionCard
          icon={<TerminalIcon className="h-3.5 w-3.5" />}
          title="install manually"
          hint={<code className="text-[10px]">$ {framework.install_cmd || `eyrie install ${safeId}`}</code>}
          description="Run the framework's own install command if you prefer to manage it yourself."
          onAction={handleManual}
          actionLabel="paste into terminal"
          actionStyle="secondary"
        />
      </div>

      {framework.requirements && framework.requirements.length > 0 && (
        <div className="rounded border border-border px-3 py-2">
          <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
            requirements
          </p>
          <p className="mt-1 text-xs text-text-secondary font-mono">
            {framework.requirements.join(", ")}
          </p>
        </div>
      )}
    </div>
  );
}

// ── step 3: configure ───────────────────────────────────────────────────
function ConfigureStep({ framework, safeId, onRun, onRefresh }: Props) {
  if (!framework || !safeId) return <WaitingForFramework />;
  const hasSchema = (framework.config_schema?.common_fields?.length ?? 0) > 0;
  const [tab, setTab] = useState<"form" | "wizard" | "edit">(hasSchema ? "form" : "wizard");

  const handleWizard = () => {
    const binary = framework.binary_path || safeId;
    onRun(`${shellQuote(binary)} onboard`);
  };

  const handleEdit = () => {
    onRun("${EDITOR:-vi} " + shellQuote(framework.config_path));
  };

  return (
    <div className="space-y-3">
      <StepHeader n={3} label="configure" />
      <p className="text-xs text-text-secondary">
        Pick a provider, model, and defaults. Config lives at{" "}
        <code className="text-[10px] text-text-muted">{framework.config_path}</code>.
      </p>

      <div className="flex border-b border-border">
        {hasSchema && (
          <TabButton active={tab === "form"} onClick={() => setTab("form")}>
            quick setup
          </TabButton>
        )}
        <TabButton active={tab === "wizard"} onClick={() => setTab("wizard")}>
          run wizard in terminal
        </TabButton>
        <TabButton active={tab === "edit"} onClick={() => setTab("edit")}>
          edit config file
        </TabButton>
      </div>

      {tab === "form" && hasSchema && (
        <ConfigFieldsForm framework={framework} onSaved={onRefresh} />
      )}

      {tab === "wizard" && (
        <div className="space-y-2">
          <code className="block rounded border border-border bg-bg px-2 py-1.5 text-[11px] text-text-muted">
            $ {framework.binary_path || safeId} onboard
          </code>
          <button
            onClick={handleWizard}
            className="flex items-center gap-1.5 rounded bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent-hover"
          >
            <Play className="h-3 w-3" />
            run wizard
          </button>
        </div>
      )}

      {tab === "edit" && (
        <div className="space-y-2">
          <code className="block rounded border border-border bg-bg px-2 py-1.5 text-[11px] text-text-muted">
            $ $EDITOR {framework.config_path}
          </code>
          <button
            onClick={handleEdit}
            className="flex items-center gap-1.5 rounded border border-border px-3 py-1.5 text-xs font-medium text-text-secondary transition-colors hover:text-text"
          >
            <FileEdit className="h-3 w-3" />
            open in $EDITOR
          </button>
        </div>
      )}
    </div>
  );
}

// ── step 4: api key ─────────────────────────────────────────────────────
function ApiKeyStep({ framework, apiKey, maskedApiKey, apiKeyConfirmed, onRefresh, onApiKeyConfirm, onNavigateStep }: Props) {
  if (!framework) return <WaitingForFramework />;

  // Local / no-key providers auto-skip. Show a confirmation card.
  if (apiKey?.isLocal) {
    return (
      <div className="space-y-3">
        <StepHeader n={4} label="api key" />
        <div className="flex items-center gap-3 rounded border border-green/30 bg-green/5 px-4 py-3">
          <Check className="h-4 w-4 text-green shrink-0" />
          <div>
            <p className="text-xs font-medium text-text">No key needed for {apiKey.provider}</p>
            <p className="text-[10px] text-text-muted mt-0.5">
              Local / gateway providers don't require an API key.
            </p>
          </div>
        </div>
      </div>
    );
  }

  // Provider couldn't be detected from the config (maybe TOML not parsed).
  if (!apiKey) {
    return (
      <div className="space-y-3">
        <StepHeader n={4} label="api key" />
        <p className="text-xs text-text-secondary">
          Configure a provider first (step 3) so we know which key to collect.
          The hint from the registry:
        </p>
        <code className="block rounded border border-border bg-bg px-2 py-1.5 text-[11px] text-text-muted">
          {framework.config_schema?.api_key_hint || "no hint available"}
        </code>
      </div>
    );
  }

  // Key already exists in the vault — show confirmation UI
  if (apiKey.hasKey && !apiKeyConfirmed) {
    return (
      <div className="space-y-3">
        <StepHeader n={4} label="api key" />
        <div className="flex items-center justify-between rounded border border-border bg-bg px-3 py-2">
          <div>
            <p className="text-[10px] uppercase tracking-wider text-text-muted">
              detected provider (from your config)
            </p>
            <p className="mt-0.5 text-xs font-semibold text-text">{apiKey.provider}</p>
          </div>
          {onNavigateStep && (
            <button
              onClick={() => onNavigateStep("configure")}
              className="text-[10px] text-text-muted hover:text-accent transition-colors"
            >
              change provider &rarr;
            </button>
          )}
        </div>
        <div className="rounded border border-green/30 bg-green/5 px-4 py-3 space-y-3">
          <div className="flex items-center gap-2">
            <Check className="h-3.5 w-3.5 text-green shrink-0" />
            <div>
              <p className="text-xs text-text">
                A key for <span className="font-semibold">{apiKey.provider}</span> is already in the vault.
              </p>
              {maskedApiKey && (
                <p className="text-[10px] text-text-muted font-mono mt-0.5">{maskedApiKey}</p>
              )}
            </div>
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => onApiKeyConfirm?.()}
              className="rounded bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent-hover"
            >
              use this key
            </button>
            <span className="text-[10px] text-text-muted">or replace it below</span>
          </div>
        </div>
        <ApiKeyForm provider={apiKey.provider} hideSavedStatus onSaved={() => { onRefresh(); onApiKeyConfirm?.(); }} />
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <StepHeader n={4} label="api key" />
      <div className="flex items-center justify-between rounded border border-border bg-bg px-3 py-2">
        <div>
          <p className="text-[10px] uppercase tracking-wider text-text-muted">
            detected provider (from your config)
          </p>
          <p className="mt-0.5 text-xs font-semibold text-text">{apiKey.provider}</p>
        </div>
        {onNavigateStep && (
          <button
            onClick={() => onNavigateStep("configure")}
            className="text-[10px] text-text-muted hover:text-accent transition-colors"
          >
            change provider &rarr;
          </button>
        )}
      </div>
      <ApiKeyForm provider={apiKey.provider} onSaved={() => { onRefresh(); onApiKeyConfirm?.(); }} />
      <p className="text-[10px] text-text-muted">
        or set <code className="font-mono">{apiKey.provider.toUpperCase().replace(/-/g, "_")}_API_KEY</code>{" "}
        as an environment variable and restart Eyrie.
      </p>
    </div>
  );
}

// ── step 5: launch ──────────────────────────────────────────────────────
const CHAT_COMMANDS: Record<string, string> = {
  zeroclaw: "zeroclaw agent",
  openclaw: "openclaw tui",
  picoclaw: "picoclaw agent",
  hermes: "hermes",
};

function LaunchStep({ framework, safeId, onRun, onHealthChange }: Props) {
  if (!framework || !safeId) return <WaitingForFramework />;

  const handleGateway = () => {
    if (!framework.start_cmd) return;
    // start_cmd may use the bare binary name (e.g. "picoclaw gateway").
    // Rewrite it to use the full binary_path so it works even when the
    // binary isn't in $PATH.
    const binary = framework.binary_path;
    if (binary) {
      const args = framework.start_cmd.split(" ").slice(1).join(" ");
      onRun(`${shellQuote(binary)}${args ? " " + args : ""}`);
    } else {
      onRun(framework.start_cmd);
    }
  };

  // Pre-compute whether we can actually produce a chat command. If not,
  // disable the button instead of leaving it clickable with a silent no-op.
  const chatFallback = CHAT_COMMANDS[safeId];
  const chatCommand: string | null = (() => {
    const binary = framework.binary_path;
    const chatArgs = (chatFallback || "").split(" ").slice(1).join(" ");
    if (binary) return `${shellQuote(binary)}${chatArgs ? " " + chatArgs : ""}`;
    if (chatFallback) return chatFallback;
    return null;
  })();

  const handleChat = () => {
    if (chatCommand) onRun(chatCommand);
  };

  return (
    <div className="space-y-3">
      <StepHeader n={5} label="launch" />
      <p className="text-xs text-text-secondary">
        Start the gateway (if needed) and launch a chat to confirm {framework.name} is working.
      </p>
      <div className="flex flex-wrap gap-2">
        {framework.start_cmd && (
          <button
            onClick={handleGateway}
            className="flex items-center gap-1.5 rounded bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent-hover"
          >
            <Play className="h-3 w-3" />
            start gateway
          </button>
        )}
        <button
          onClick={handleChat}
          disabled={!chatCommand}
          title={chatCommand ? undefined : "no chat command known for this framework"}
          className="flex items-center gap-1.5 rounded bg-accent px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-accent-hover disabled:opacity-40 disabled:cursor-not-allowed"
        >
          <TerminalIcon className="h-3 w-3" />
          launch chat
        </button>
      </div>
      {!chatCommand && (
        <p className="text-[10px] text-text-muted">
          No chat command is registered for {framework.name}. Install and
          configure first, or launch the gateway and chat via your own client.
        </p>
      )}

      {framework.health_url && safeId && <HealthCheck url={framework.health_url} frameworkId={safeId} onHealthChange={onHealthChange} />}
    </div>
  );
}

/** Checks gateway health via Eyrie's backend proxy to avoid CORS issues. */
function HealthCheck({ url, frameworkId, onHealthChange }: { url: string; frameworkId: string; onHealthChange?: (healthy: boolean) => void }) {
  const [status, setStatus] = useState<"pending" | "ok" | "down">("pending");
  const onHealthChangeRef = useRef(onHealthChange);
  onHealthChangeRef.current = onHealthChange;
  useEffect(() => {
    let cancelled = false;
    const check = () => {
      fetch(`/api/registry/frameworks/${frameworkId}/health`)
        .then((r) => r.ok ? r.json() : { status: "down" })
        .then((data: { status: string }) => {
          if (cancelled) return;
          const healthy = data.status === "ok";
          setStatus(healthy ? "ok" : "down");
          onHealthChangeRef.current?.(healthy);
        })
        .catch(() => {
          if (!cancelled) { setStatus("down"); onHealthChangeRef.current?.(false); }
        });
    };
    check();
    const interval = setInterval(check, 5000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [frameworkId, url]);

  const dot =
    status === "ok" ? "bg-green" : status === "down" ? "bg-red" : "bg-yellow animate-pulse";
  const label =
    status === "ok"
      ? "gateway healthy"
      : status === "down"
        ? "gateway not responding"
        : "checking…";

  return (
    <div className="flex items-center gap-2 text-[10px] text-text-muted">
      <span className={`h-1.5 w-1.5 rounded-full ${dot}`} />
      <span>
        {label} <code className="text-text-muted">{url}</code>
      </span>
    </div>
  );
}

// ── shared bits ─────────────────────────────────────────────────────────
function StepHeader({ n, label }: { n: number; label: string }) {
  return (
    <div>
      <p className="text-[10px] uppercase tracking-wider text-text-muted">step {n} of 5</p>
      <h3 className="mt-0.5 text-sm font-semibold text-text capitalize">{label}</h3>
    </div>
  );
}

function WaitingForFramework() {
  return (
    <div className="text-center py-4 text-xs text-text-muted">
      pick a framework first (step 1)
    </div>
  );
}

function OptionCard({
  icon,
  title,
  hint,
  description,
  onAction,
  actionLabel,
  actionStyle,
}: {
  icon: React.ReactNode;
  title: string;
  hint: React.ReactNode;
  description: string;
  onAction: () => void;
  actionLabel: string;
  actionStyle: "primary" | "secondary";
}) {
  return (
    <div className="rounded border border-border bg-bg p-3 flex flex-col gap-2">
      <div className="flex items-center gap-2">
        <span className="text-text-muted">{icon}</span>
        <h4 className="text-xs font-medium text-text">{title}</h4>
      </div>
      <div className="text-text-muted">{hint}</div>
      <p className="text-[10px] text-text-muted flex-1">{description}</p>
      <button
        onClick={onAction}
        className={`rounded px-3 py-1.5 text-xs font-medium transition-colors ${
          actionStyle === "primary"
            ? "bg-accent text-white hover:bg-accent-hover"
            : "border border-border text-text-secondary hover:text-text hover:border-accent/30"
        }`}
      >
        {actionLabel}
      </button>
    </div>
  );
}

function TabButton({
  children,
  active,
  onClick,
}: {
  children: React.ReactNode;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={`px-3 py-1.5 text-xs transition-colors border-b-2 -mb-px ${
        active
          ? "border-accent text-text"
          : "border-transparent text-text-muted hover:text-text"
      }`}
    >
      {children}
    </button>
  );
}

/** Pre-fill a question in the commander chat panel. */
export function askCommander(question: string): void {
  window.dispatchEvent(new CustomEvent(COMMANDER_PREFILL_EVENT, { detail: question }));
}

function AskCommander({ question, label }: { question: string; label: string }) {
  return (
    <button
      onClick={() => askCommander(question)}
      className="flex items-center gap-1.5 text-[10px] text-purple hover:text-purple/80 transition-colors"
    >
      <MessageSquare className="h-3 w-3" />
      {label}
    </button>
  );
}
