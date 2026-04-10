// FrameworkCompare.tsx — Unified frameworks page: install + compare.
//
// WHY merged: The install page and comparison page showed overlapping data
// about the same frameworks. Merging keeps context together — you see
// capabilities, security posture, and install status in one place.
//
// WHY static capability data: Framework features (interrupt support,
// shell sandboxing, etc.) are properties of the codebase, not runtime state.
// They change only when a new version ships.

import { useEffect, useState, useRef } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { RefreshCw, AlertCircle, ChevronDown, ChevronRight, Package, Settings, Terminal as TerminalIcon } from "lucide-react";
import { useData } from "../lib/DataContext";
import { FRAMEWORK_EMOJI } from "../lib/types";
import { formatBytes } from "../lib/format";
import type { Framework, InstallProgress } from "../lib/types";
import { fetchFrameworks, fetchInstallStatus, streamInstall } from "../lib/api";
import FrameworkCard from "./FrameworkCard";
import Terminal from "./Terminal";

// ── Static capability data ───────────────────────────────────────────────

type SupportLevel = "full" | "partial" | "none" | "planned";

interface FrameworkCapabilities {
  features: Record<string, SupportLevel>;
  notes: Record<string, string>;
  security: Record<string, SupportLevel>;
  securityNotes: Record<string, string>;
  architecture: string;
  /** CLI command for interactive chat (pre-populated in shell terminal) */
  chatCommand: string;
}

const CAPABILITIES: Record<string, FrameworkCapabilities> = {
  zeroclaw: {
    architecture: "persistent gateway",
    chatCommand: "zeroclaw agent",
    features: {
      "streaming responses": "full",
      "named sessions": "full",
      "tool execution": "full",
      "skill/plugin ecosystem": "partial",
      "shell sandboxing": "full",
      "interrupt in-flight": "planned",
      "multi-agent delegation": "full",
      "memory system": "full",
      "cron scheduling": "full",
      "channels (telegram, discord)": "full",
      "canvas rendering": "full",
      "web search": "full",
      "instance provisioning": "full",
    },
    notes: {
      "interrupt in-flight": "internal CancellationToken exists, REST endpoint pending",
      "shell sandboxing": "seatbelt (macOS) / bubblewrap (Linux)",
      "multi-agent delegation": "native delegate tool with sub-agent loops",
      "skill/plugin ecosystem": "built-in tools only, no plugin registry",
    },
    security: {
      "shell sandbox": "full",
      "workspace isolation": "full",
      "API key encryption": "full",
      "auth token (pairing)": "full",
      "SSRF protection": "partial",
      "tool output delimiters": "full",
    },
    securityNotes: {
      "shell sandbox": "seatbelt/bubblewrap with per-tool policies (disabled by default in provisioned instances on macOS)",
      "API key encryption": "encrypted on disk with .secret_key",
      "SSRF protection": "allowlist-based (allowed_private_hosts), not blocked by default",
    },
  },
  openclaw: {
    architecture: "persistent gateway",
    chatCommand: "openclaw tui",
    features: {
      "streaming responses": "full",
      "named sessions": "full",
      "tool execution": "full",
      "skill/plugin ecosystem": "full",
      "shell sandboxing": "partial",
      "interrupt in-flight": "partial",
      "multi-agent delegation": "none",
      "memory system": "full",
      "cron scheduling": "full",
      "channels (telegram, discord)": "full",
      "canvas rendering": "none",
      "web search": "full",
      "instance provisioning": "full",
    },
    notes: {
      "interrupt in-flight": "emits 'aborted' events internally, no public API yet",
      "shell sandboxing": "allowlist-based command filtering",
      "skill/plugin ecosystem": "large community skill library with npm-based installation",
    },
    security: {
      "shell sandbox": "partial",
      "workspace isolation": "full",
      "API key encryption": "none",
      "auth token (pairing)": "none",
      "SSRF protection": "full",
      "tool output delimiters": "none",
    },
    securityNotes: {
      "shell sandbox": "regex allowlist, no OS-level isolation",
      "SSRF protection": "blocks all private IPs by default in web_fetch — no config needed",
    },
  },
  picoclaw: {
    architecture: "persistent gateway",
    chatCommand: "picoclaw agent",
    features: {
      "streaming responses": "full",
      "named sessions": "full",
      "tool execution": "full",
      "skill/plugin ecosystem": "partial",
      "shell sandboxing": "partial",
      "interrupt in-flight": "none",
      "multi-agent delegation": "none",
      "memory system": "partial",
      "cron scheduling": "none",
      "channels (telegram, discord)": "partial",
      "canvas rendering": "none",
      "web search": "full",
      "instance provisioning": "full",
    },
    notes: {
      "shell sandboxing": "workspace-restricted execution",
      "memory system": "basic key-value, no semantic search",
      "channels (telegram, discord)": "telegram only",
      "skill/plugin ecosystem": "channel-based plugins",
    },
    security: {
      "shell sandbox": "partial",
      "workspace isolation": "full",
      "API key encryption": "none",
      "auth token (pairing)": "full",
      "SSRF protection": "none",
      "tool output delimiters": "none",
    },
    securityNotes: {},
  },
  hermes: {
    architecture: "process-per-message",
    chatCommand: "hermes",
    features: {
      "streaming responses": "full",
      "named sessions": "full",
      "tool execution": "full",
      "skill/plugin ecosystem": "none",
      "shell sandboxing": "none",
      "interrupt in-flight": "full",
      "multi-agent delegation": "none",
      "memory system": "full",
      "cron scheduling": "none",
      "channels (telegram, discord)": "partial",
      "canvas rendering": "none",
      "web search": "partial",
      "instance provisioning": "none",
    },
    notes: {
      "interrupt in-flight": "process killed on cancel — clean stop, no stale context",
      "channels (telegram, discord)": "telegram only",
    },
    security: {
      "shell sandbox": "none",
      "workspace isolation": "partial",
      "API key encryption": "none",
      "auth token (pairing)": "none",
      "SSRF protection": "none",
      "tool output delimiters": "none",
    },
    securityNotes: {
      "workspace isolation": "configurable working directory, no OS enforcement",
    },
  },
};

const FEATURE_KEYS = Object.keys(CAPABILITIES.zeroclaw.features);
const SECURITY_KEYS = Object.keys(CAPABILITIES.zeroclaw.security);

// ── Support level rendering ──────────────────────────────────────────────

function SupportBadge({ level }: { level: SupportLevel }) {
  const styles: Record<SupportLevel, { bg: string; text: string; label: string }> = {
    full:    { bg: "bg-green/10", text: "text-green", label: "full" },
    partial: { bg: "bg-yellow/10", text: "text-yellow", label: "partial" },
    none:    { bg: "bg-red/10", text: "text-red", label: "none" },
    planned: { bg: "bg-purple-400/10", text: "text-purple-400", label: "planned" },
  };
  const s = styles[level];
  return (
    <span className={`inline-block rounded px-1.5 py-0.5 text-[9px] font-medium ${s.bg} ${s.text}`}>
      {s.label}
    </span>
  );
}

// ── Note tooltip (click + hover) ─────────────────────────────────────────

function NoteIndicator({ note }: { note: string }) {
  const [open, setOpen] = useState(false);

  return (
    <span className="relative inline-block ml-1">
      <button
        type="button"
        onClick={(e) => { e.stopPropagation(); setOpen(!open); }}
        onKeyDown={(e) => { if (e.key === "Escape") { setOpen(false); e.stopPropagation(); } }}
        className="text-[9px] text-text-muted hover:text-accent cursor-help"
        aria-label={`Info: ${note}`}
        aria-expanded={open}
        aria-controls="note-tooltip"
      >
        ?
      </button>
      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div id="note-tooltip" role="tooltip" className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1 z-50 w-48 rounded border border-border bg-bg p-2 text-[10px] text-text-secondary shadow-lg">
            {note}
          </div>
        </>
      )}
    </span>
  );
}

// ── Feature matrix table ─────────────────────────────────────────────────

function FeatureMatrix({
  title: _title,
  featureKeys,
  frameworks,
  getLevel,
  getNote,
}: {
  title: string;
  featureKeys: string[];
  frameworks: Framework[];
  getLevel: (fwId: string, feature: string) => SupportLevel;
  getNote: (fwId: string, feature: string) => string | undefined;
}) {
  return (
    <div>
      <div className="overflow-x-auto rounded border border-border">
        <table className="w-full text-xs">
          <thead>
            <tr className="border-b border-border bg-surface text-left text-text-muted">
              <th className="px-4 py-2.5 font-medium">feature</th>
              {frameworks.map((fw) => (
                <th key={fw.id} className="px-4 py-2.5 font-medium text-center">
                  {FRAMEWORK_EMOJI[fw.id] || ""} {fw.name}
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="[&>tr+tr]:border-t [&>tr+tr]:border-border">
            {featureKeys.map((feature) => (
              <tr key={feature} className="hover:bg-surface-hover/30 transition-colors">
                <td className="px-4 py-2 text-text-secondary">{feature}</td>
                {frameworks.map((fw) => {
                  const note = getNote(fw.id, feature);
                  return (
                    <td key={fw.id} className="px-4 py-2 text-center">
                      <SupportBadge level={getLevel(fw.id, feature)} />
                      {note && <NoteIndicator note={note} />}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ── Main page ────────────────────────────────────────────────────────────

export default function FrameworkCompare() {
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { agents } = useData();
  const highlightId = searchParams.get("highlight");
  const compareMode = searchParams.get("compare") === "true";
  const highlightRef = useRef<HTMLDivElement>(null);
  const compareRef = useRef<HTMLDivElement>(null);
  // Auto-clear highlight after 3s so it doesn't stick permanently
  useEffect(() => {
    if (!highlightId) return;
    const timer = setTimeout(() => {
      setSearchParams((prev) => { const next = new URLSearchParams(prev); next.delete("highlight"); return next; }, { replace: true });
    }, 3000);
    return () => clearTimeout(timer);
  }, [highlightId, setSearchParams]);

  // ── Install state (from InstallPage) ─────────────────────────────────
  const [frameworks, setFrameworks] = useState<Framework[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [installProgress, setInstallProgress] = useState<Record<string, InstallProgress>>({});
  const [installLogs, setInstallLogs] = useState<Record<string, string[]>>({});
  const [selectedFramework, setSelectedFramework] = useState<string | null>(null);
  const selectedFrameworkRef = useRef<string | null>(null);
  const [showLogs, setShowLogs] = useState(false);
  const [showTerminal, setShowTerminal] = useState(false);
  const abortControllers = useRef<Record<string, AbortController>>({});
  const logEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => { selectedFrameworkRef.current = selectedFramework; }, [selectedFramework]);
  // Scroll to highlighted framework card when navigated from mission control
  useEffect(() => {
    if (highlightId && highlightRef.current && !loading) {
      highlightRef.current.scrollIntoView({ behavior: "smooth", block: "center" });
    }
  }, [highlightId, loading]);
  // Scroll to comparison section when arriving from "I'm not sure"
  useEffect(() => {
    if (compareMode && compareRef.current && !loading) {
      compareRef.current.scrollIntoView({ behavior: "smooth", block: "start" });
    }
  }, [compareMode, loading]);
  useEffect(() => {
    loadFrameworks(); loadInstallStatus();
    return () => {
      // Abort in-flight install streams on unmount
      Object.values(abortControllers.current).forEach((c) => c.abort());
    };
  }, []);
  useEffect(() => {
    if (showLogs && logEndRef.current) logEndRef.current.scrollIntoView({ behavior: "smooth" });
  }, [installLogs, showLogs]);

  const loadInstallStatus = async () => {
    try { setInstallProgress(await fetchInstallStatus()); } catch { /* silent */ }
  };

  const loadFrameworks = async (refresh = false) => {
    try {
      setLoading(true); setError(null);
      setFrameworks(await fetchFrameworks(refresh));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load frameworks");
    } finally { setLoading(false); }
  };

  const [setupCommand, setSetupCommand] = useState<string | undefined>();
  const [showApiKeyPrompt, setShowApiKeyPrompt] = useState<string | null>(null); // framework ID
  const [featuresExpanded, setFeaturesExpanded] = useState(compareMode);
  const [securityExpanded, setSecurityExpanded] = useState(compareMode);
  const [archExpanded, setArchExpanded] = useState(compareMode);

  const handleSetup = (frameworkId: string) => {
    const fw = frameworks.find((f) => f.id === frameworkId);
    // Use full path — the binary may not be in $PATH yet
    const binaryPath = fw?.binary_path || frameworkId;
    setSelectedFramework(frameworkId);
    setSetupCommand(`${binaryPath} onboard`);
    setShowTerminal(true);
  };

  const handleManage = (frameworkId: string) => {
    setSelectedFramework(frameworkId);
    setInstallProgress((prev) => ({
      ...prev,
      [frameworkId]: { framework_id: frameworkId, phase: "complete", status: "success" as const, progress: 100, message: "installed", started_at: new Date().toISOString() },
    }));
    if (!installLogs[frameworkId]?.length) {
      setInstallLogs((prev) => ({ ...prev, [frameworkId]: [`${frameworkId} is installed and ready.`] }));
    }
    setShowLogs(true);
  };

  const handleInstall = (frameworkId: string) => {
    if (abortControllers.current[frameworkId]) abortControllers.current[frameworkId].abort();
    setSelectedFramework(frameworkId);
    setInstallLogs((prev) => ({ ...prev, [frameworkId]: [] }));
    setShowLogs(true);
    const shouldForce = installProgress[frameworkId]?.status === "error";
    const controller = streamInstall(frameworkId, undefined,
      (progress) => {
        setInstallProgress((prev) => ({ ...prev, [frameworkId]: progress }));
        if (progress.status === "success") setTimeout(() => { if (selectedFrameworkRef.current === frameworkId) setShowLogs(false); }, 2000);
      },
      (log) => { setInstallLogs((prev) => ({ ...prev, [frameworkId]: [...(prev[frameworkId] || []), log] })); },
      shouldForce,
    );
    abortControllers.current[frameworkId] = controller;
  };

  const currentLogs = selectedFramework ? installLogs[selectedFramework] || [] : [];
  const currentProgress = selectedFramework ? installProgress[selectedFramework] : undefined;

  // ── Live stats ───────────────────────────────────────────────────────
  const agentCounts: Record<string, number> = {};
  const memoryByFramework: Record<string, number> = {};
  for (const a of agents) {
    if (a.alive) agentCounts[a.framework] = (agentCounts[a.framework] || 0) + 1;
    if (a.health?.ram_bytes) memoryByFramework[a.framework] = (memoryByFramework[a.framework] || 0) + a.health.ram_bytes;
  }

  return (
    <div className="flex flex-col h-full space-y-6">
      <div className="text-xs text-text-muted">~/frameworks</div>

      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-bold">
            <span className="text-accent">&gt;</span> frameworks
          </h1>
          <p className="mt-1 text-xs text-text-muted">
            // install, compare capabilities, and evaluate trade-offs
          </p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => { setSelectedFramework("shell"); setSetupCommand(undefined); setShowTerminal(true); }}
            className="flex items-center gap-1.5 text-xs text-text-muted transition-colors hover:text-text"
          >
            <TerminalIcon className="h-3.5 w-3.5" />
            $ terminal
          </button>
          <button
            onClick={() => loadFrameworks(true)}
            disabled={loading}
            className="flex items-center gap-2 text-xs text-text-muted transition-colors hover:text-text disabled:opacity-50"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
            $ refresh
          </button>
        </div>
      </div>

      {error && (
        <div className="rounded border border-red/30 bg-red/5 px-4 py-3 flex items-start gap-2">
          <AlertCircle className="h-3.5 w-3.5 text-red mt-0.5 flex-shrink-0" />
          <div>
            <p className="text-xs text-red font-medium">failed to load frameworks</p>
            <p className="text-[10px] text-red/80 mt-0.5">{error}</p>
          </div>
        </div>
      )}

      {loading && !frameworks.length && (
        <div className="py-12 text-center text-xs text-text-muted">loading frameworks...</div>
      )}

      {/* Framework cards with install + metadata */}
      {!loading && frameworks.length > 0 && (
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
          {frameworks.map((fw) => {
            const caps = CAPABILITIES[fw.id];
            const isHighlighted = highlightId === fw.id;
            return (
              <div key={fw.id} ref={isHighlighted ? highlightRef : undefined} className={`flex flex-col space-y-0 rounded transition-all duration-700 ${isHighlighted ? "ring-2 ring-accent ring-offset-2 ring-offset-bg" : ""}`}>
                <FrameworkCard
                  framework={fw}
                  installProgress={installProgress[fw.id]}
                  onInstall={() => handleInstall(fw.id)}
                  onManage={() => handleManage(fw.id)}
                  onSetup={() => handleSetup(fw.id)}
                  disabled={loading}
                />
                {/* Extra metadata below card */}
                {caps && (
                  <div className="rounded-b border border-t-0 border-border bg-surface/50 px-4 py-2 space-y-1 text-[10px] text-text-secondary">
                    <div className="flex justify-between">
                      <span className="text-text-muted">architecture</span>
                      <span>{caps.architecture}</span>
                    </div>
                    <div className="flex justify-between">
                      <span className="text-text-muted">agents running</span>
                      <span>{agentCounts[fw.id] || 0}</span>
                    </div>
                    {memoryByFramework[fw.id] != null && (
                      <div className="flex justify-between">
                        <span className="text-text-muted">total memory</span>
                        <span>{formatBytes(memoryByFramework[fw.id])}</span>
                      </div>
                    )}
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {!loading && !error && frameworks.length === 0 && (
        <div className="rounded border border-border bg-surface p-8 text-center text-xs text-text-muted">
          <Package className="h-8 w-8 text-text-muted/30 mx-auto mb-2" />
          no frameworks available — check registry configuration
        </div>
      )}

      {/* Feature comparison matrices (collapsed by default) */}
      <div ref={compareRef} />
      {frameworks.length > 0 && (
        <>
          <div>
            <button
              onClick={() => setFeaturesExpanded((prev) => !prev)}
              className="flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-text-muted hover:text-text transition-colors"
            >
              {featuresExpanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
              feature comparison
            </button>
            {featuresExpanded && (
              <div className="mt-3">
                <FeatureMatrix
                  title="features"
                  featureKeys={FEATURE_KEYS}
                  frameworks={frameworks}
                  getLevel={(id, f) => CAPABILITIES[id]?.features[f] || "none"}
                  getNote={(id, f) => CAPABILITIES[id]?.notes[f]}
                />
              </div>
            )}
          </div>
          <div>
            <button
              onClick={() => setSecurityExpanded((prev) => !prev)}
              className="flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-text-muted hover:text-text transition-colors"
            >
              {securityExpanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
              security comparison
            </button>
            {securityExpanded && (
              <div className="mt-3">
                <FeatureMatrix
                  title="security"
                  featureKeys={SECURITY_KEYS}
                  frameworks={frameworks}
                  getLevel={(id, f) => CAPABILITIES[id]?.security[f] || "none"}
                  getNote={(id, f) => CAPABILITIES[id]?.securityNotes[f]}
                />
              </div>
            )}
          </div>

          {/* Architecture trade-offs */}
          <div>
            <button
              onClick={() => setArchExpanded((prev) => !prev)}
              className="flex items-center gap-2 text-xs font-medium uppercase tracking-wider text-text-muted hover:text-text transition-colors"
            >
              {archExpanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
              architecture trade-offs
            </button>
            {archExpanded && <div className="mt-3 grid grid-cols-2 gap-3">
              <div className="rounded border border-border bg-surface p-4">
                <h3 className="text-xs font-medium text-text mb-2">persistent gateway</h3>
                <p className="text-[10px] text-text-muted mb-2">ZeroClaw, OpenClaw, PicoClaw</p>
                <ul className="space-y-1 text-[10px] text-text-secondary">
                  <li className="flex gap-1.5"><span className="text-green shrink-0">+</span> fast per-message latency (process already warm)</li>
                  <li className="flex gap-1.5"><span className="text-green shrink-0">+</span> session state in memory (no disk round-trip)</li>
                  <li className="flex gap-1.5"><span className="text-green shrink-0">+</span> real-time channels (telegram, discord) via long-lived connections</li>
                  <li className="flex gap-1.5"><span className="text-red shrink-0">-</span> constant memory usage even when idle</li>
                  <li className="flex gap-1.5"><span className="text-red shrink-0">-</span> interrupting requires framework-specific API</li>
                  <li className="flex gap-1.5"><span className="text-red shrink-0">-</span> crash = lost in-memory state until restart</li>
                </ul>
              </div>
              <div className="rounded border border-border bg-surface p-4">
                <h3 className="text-xs font-medium text-text mb-2">process-per-message</h3>
                <p className="text-[10px] text-text-muted mb-2">Hermes</p>
                <ul className="space-y-1 text-[10px] text-text-secondary">
                  <li className="flex gap-1.5"><span className="text-green shrink-0">+</span> clean interrupt (kill process = everything stops)</li>
                  <li className="flex gap-1.5"><span className="text-green shrink-0">+</span> zero memory when idle (no background process)</li>
                  <li className="flex gap-1.5"><span className="text-green shrink-0">+</span> crash isolation (one bad message can't poison the process)</li>
                  <li className="flex gap-1.5"><span className="text-red shrink-0">-</span> cold start on every message (Python startup + imports)</li>
                  <li className="flex gap-1.5"><span className="text-red shrink-0">-</span> session state read from disk each time</li>
                  <li className="flex gap-1.5"><span className="text-red shrink-0">-</span> no real-time channels (no long-lived process to receive events)</li>
                </ul>
              </div>
            </div>}
          </div>
        </>
      )}

      {/* Install logs panel */}
      {showLogs && selectedFramework && (
        <div className="fixed bottom-0 left-0 right-0 bg-bg border-t border-border shadow-lg z-50">
          <div className="max-w-5xl mx-auto px-8 py-4">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <h3 className="text-xs font-semibold text-text">
                  {currentProgress?.status === "success" ? `${selectedFramework} installed` : `installing ${selectedFramework}`}
                </h3>
                {currentProgress?.status === "running" && (
                  <span className="text-[10px] text-text-muted">
                    {currentProgress.phase}
                    {currentProgress.phase === "binary" && " (compiling...)"}
                  </span>
                )}
              </div>
              <div className="flex items-center gap-2">
                {currentProgress?.status === "success" && (
                  <>
                    <button onClick={() => {
                      const fw = selectedFramework ? frameworks.find((f) => f.id === selectedFramework) : null;
                      const caps = selectedFramework ? CAPABILITIES[selectedFramework] : null;
                      // Use full binary_path from registry + subcommand from capabilities
                      const sub = caps?.chatCommand?.split(" ").slice(1).join(" ") || "";
                      const cmd = fw?.binary_path ? `${fw.binary_path}${sub ? " " + sub : ""}` : caps?.chatCommand;
                      setSetupCommand(cmd);
                      setShowTerminal(true);
                      setShowLogs(false);
                    }} className="flex items-center gap-1.5 px-3 py-1.5 bg-accent hover:bg-accent-hover text-white rounded text-xs font-medium transition-colors">
                      <TerminalIcon className="h-3 w-3" /> launch terminal
                    </button>
                    <button onClick={() => navigate(`/agents/${selectedFramework}/config`)} className="flex items-center gap-1.5 px-3 py-1.5 border border-border text-text-secondary hover:text-text rounded text-xs font-medium transition-colors">
                      <Settings className="h-3 w-3" /> configure
                    </button>
                  </>
                )}
                <button onClick={() => setShowLogs(false)} className="text-xs text-text-muted hover:text-text transition-colors">close</button>
              </div>
            </div>
            <div className="rounded border border-border bg-surface p-3 max-h-48 overflow-y-auto font-mono text-[10px]">
              {currentLogs.length === 0 ? (
                <p className="text-text-muted">starting installation...</p>
              ) : currentLogs.map((log, i) => (
                <div key={i} className="text-text-secondary whitespace-pre-wrap">{log}</div>
              ))}
              <div ref={logEndRef} />
            </div>
          </div>
        </div>
      )}

      {showTerminal && selectedFramework && (
        <Terminal
          agentName={selectedFramework}
          onClose={() => {
            const wasSetup = !!setupCommand;
            const fwId = selectedFramework;
            setShowTerminal(false);
            setSetupCommand(undefined);
            loadFrameworks();
            // After setup onboarding, prompt for API key configuration
            if (wasSetup && fwId !== "shell") {
              setShowApiKeyPrompt(fwId);
            }
          }}
          initialCommand={setupCommand}
          useShell={!!setupCommand || selectedFramework === "shell"}
        />
      )}

      {/* API key prompt after setup */}
      {showApiKeyPrompt && (() => {
        const fw = frameworks.find((f) => f.id === showApiKeyPrompt);
        const hint = fw?.config_schema?.api_key_hint;
        return (
          <div className="fixed inset-0 bg-black/60 z-50 flex items-center justify-center p-4" onClick={() => setShowApiKeyPrompt(null)}>
            <div className="bg-bg border border-border rounded-lg shadow-2xl w-full max-w-md p-6 space-y-4" onClick={(e) => e.stopPropagation()}>
              <div>
                <h3 className="text-sm font-bold text-text">{fw?.name || showApiKeyPrompt} — API key setup</h3>
                <p className="mt-1 text-xs text-text-muted">onboarding complete. configure an API key to start using the framework.</p>
              </div>
              {hint && (
                <div className="rounded border border-border bg-surface p-3 text-xs text-text-secondary whitespace-pre-wrap">
                  {hint}
                </div>
              )}
              <div className="flex justify-end gap-2">
                <button
                  onClick={() => setShowApiKeyPrompt(null)}
                  className="px-3 py-1.5 text-xs text-text-muted hover:text-text transition-colors"
                >
                  later
                </button>
                <button
                  onClick={() => {
                    setShowApiKeyPrompt(null);
                    navigate(`/agents/${showApiKeyPrompt}/config`);
                  }}
                  className="px-3 py-1.5 text-xs font-medium bg-accent text-white rounded hover:bg-accent/80 transition-colors"
                >
                  open config editor
                </button>
              </div>
            </div>
          </div>
        );
      })()}
    </div>
  );
}
