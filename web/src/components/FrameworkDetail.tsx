// FrameworkDetail.tsx — Dedicated framework page centred on a persistent tmux terminal.
//
// All actions (install, setup, uninstall, chat) run as commands in the same
// tmux shell session. No SSE, no log mode — one terminal for everything.
// tmux keeps the session alive across page navigations and reloads.

import { useEffect, useState, useRef, useCallback } from "react";
import { useParams, useNavigate, Link } from "react-router-dom";
import { ArrowLeft, ExternalLink, Download, Settings, Terminal as TerminalIcon, RefreshCw, ChevronRight, Search, Trash2 } from "lucide-react";
import { FRAMEWORK_EMOJI } from "../lib/types";
import type { Framework } from "../lib/types";
import { getFrameworkDetail } from "../lib/api";
import { getFrameworkStatus } from "../lib/frameworkStatus";
import { useData } from "../lib/DataContext";
import Terminal, { TerminalHandle } from "./Terminal";
import { shellQuote } from "../lib/shell";

const CHAT_COMMANDS: Record<string, string> = {
  zeroclaw: "zeroclaw agent",
  openclaw: "openclaw tui",
  picoclaw: "picoclaw agent",
  hermes: "hermes",
};

function statusDotClass(alive: boolean, providerStatus?: string): string {
  if (!alive) return "bg-red";
  if (providerStatus === "error") return "bg-yellow";
  return "bg-green";
}

export default function FrameworkDetail() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { agents, refresh: refreshGlobal } = useData();
  const termRef = useRef<TerminalHandle>(null);

  // ── Framework data ──────────────────────────────────────────────────
  const [framework, setFramework] = useState<Framework | null>(null);
  const [error, setError] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const [pendingUninstall, setPendingUninstall] = useState(false);

  const loadFramework = useCallback(async () => {
    if (!id) return;
    try {
      setFramework(await getFrameworkDetail(id));
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load framework");
    }
  }, [id]);

  useEffect(() => { loadFramework(); }, [loadFramework]);

  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      await loadFramework();
    } finally {
      // Always reset — otherwise a transient loadFramework error would
      // leave the refresh spinner stuck forever.
      setRefreshing(false);
    }
  };

  // Sanitize id before interpolating into shell commands. Framework ids
  // come from the URL path and are passed to a local tmux terminal, so
  // validate against a strict allowlist to eliminate command-injection
  // surface even though the terminal is local.
  const safeId = id && /^[a-zA-Z0-9_-]+$/.test(id) ? id : null;

  // ── Background status refresh ──────────────────────────────────────
  const frameworkRef = useRef(framework);
  frameworkRef.current = framework;

  // Poll when the framework is in a transitional state (install/configure
  // running) OR when the user just clicked uninstall and we're waiting for
  // it to finish. Skip polling when stable (ready or fully uninstalled).
  const needsPolling =
    (framework && (!framework.installed || !framework.configured)) || pendingUninstall;

  useEffect(() => {
    if (!id || !needsPolling) return;
    const interval = setInterval(async () => {
      try {
        const updated = await getFrameworkDetail(id);
        const current = frameworkRef.current;
        if (current && (updated.installed !== current.installed || updated.configured !== current.configured)) {
          setFramework(updated);
          // Uninstall just completed — refresh the sidebar immediately
          if (current.installed && !updated.installed) {
            setPendingUninstall(false);
            refreshGlobal(false);
          }
        }
      } catch { /* silent */ }
    }, 3000);
    return () => clearInterval(interval);
  }, [id, needsPolling, pendingUninstall, refreshGlobal]);

  // ── Uninstall ───────────────────────────────────────────────────────
  const [showUninstallConfirm, setShowUninstallConfirm] = useState(false);
  const [uninstallPurge, setUninstallPurge] = useState(false);

  // ── Derived state ──────────────────────────────────────────────────
  const fwAgents = agents.filter((a) => a.framework === id);
  const emoji = FRAMEWORK_EMOJI[id || ""] || "";
  const status = framework ? getFrameworkStatus(framework) : null;

  // Binary name for locate/which commands (basename only, no path).
  const SAFE_BASENAME_RE = /^[A-Za-z0-9._-]+$/;
  const rawBinaryName = framework?.binary_path?.split("/").pop() || id || "";
  const safeBinaryName = SAFE_BASENAME_RE.test(rawBinaryName) ? rawBinaryName : safeId;

  // ── Terminal command helpers ────────────────────────────────────────
  const sendToTerminal = (cmd: string) => {
    termRef.current?.runCommand(cmd);
  };

  const handleInstall = () => {
    if (!framework || !safeId) return;
    sendToTerminal(`eyrie install ${safeId} -y`);
  };

  const handleUninstall = () => {
    if (!safeId) return;
    setShowUninstallConfirm(false);
    setPendingUninstall(true);
    const purgeFlag = uninstallPurge ? " --purge" : "";
    sendToTerminal(`eyrie uninstall ${safeId} -y${purgeFlag}`);
    setUninstallPurge(false);
  };

  if (error && !framework) {
    return (
      <div className="space-y-4">
        <Link to="/frameworks" className="text-xs text-text-muted hover:text-text">&lt; back</Link>
        <p className="text-xs text-red">{error}</p>
      </div>
    );
  }

  if (!framework) {
    return <p className="py-20 text-center text-xs text-text-muted">loading framework...</p>;
  }

  return (
    <div className="flex flex-col h-full space-y-4">
      <div className="text-xs text-text-muted">~/frameworks/{id}</div>

      {/* Header */}
      <div className="flex items-center gap-3">
        <button
          onClick={() => navigate("/frameworks")}
          className="rounded p-1 text-text-muted transition-colors hover:bg-surface-hover hover:text-text"
        >
          <ArrowLeft className="h-4 w-4" />
        </button>
        <h1 className="text-xl font-bold">
          <span className="text-accent">&gt;</span> {framework.name} {emoji}
        </h1>
        {framework.version && (
          <span className="text-xs text-text-muted font-mono">{framework.version}</span>
        )}
        {status?.badge && (
          <span className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${
            status.badge.color === "green" ? "bg-green/10 text-green" :
            status.badge.color === "red" ? "bg-red/10 text-red" :
            status.badge.color === "blue" ? "bg-blue/10 text-blue" :
            "bg-yellow/10 text-yellow"
          }`}>{status.badge.label}</span>
        )}
        <div className="flex-1" />
        <button
          onClick={handleRefresh}
          disabled={refreshing}
          className="flex items-center gap-1.5 text-xs text-text-muted hover:text-text transition-colors disabled:opacity-50"
        >
          <RefreshCw className={`h-3.5 w-3.5 ${refreshing ? "animate-spin" : ""}`} />
          refresh
        </button>
      </div>

      <p className="text-xs text-text-secondary">{framework.description}</p>

      {/* Status info */}
      {status?.isBinaryMissing && (
        <div className="rounded border border-yellow/30 bg-yellow/5 px-4 py-3 text-xs text-text-secondary space-y-1">
          <p>Config exists but binary not found at <code className="font-mono text-text-muted">{framework.binary_path}</code></p>
          <p className="text-text-muted">Use "locate binary" to check if it's installed elsewhere, or "reinstall" to install from scratch.</p>
        </div>
      )}
      {status?.needsSetup && (
        <div className="rounded border border-yellow/30 bg-yellow/5 px-4 py-3 text-xs text-text-secondary">
          Binary installed at <code className="font-mono text-text-muted">{framework.binary_path}</code> but no config found at <code className="font-mono text-text-muted">{framework.config_path}</code>. Run setup to create it.
        </div>
      )}

      {/* Action bar */}
      <div className="flex flex-wrap items-center gap-2">
        {status?.isBinaryMissing && (
          <>
            <button
              onClick={() => {
                if (!safeBinaryName) return;
                // Use command -v (POSIX) rather than which, and quote the
                // subshell so paths with spaces are handled correctly.
                sendToTerminal(`p=$(command -v ${safeBinaryName}) && ls -la "$p" || echo "${safeBinaryName} not found in PATH"`);
              }}
              disabled={!safeBinaryName}
              className="flex items-center gap-1.5 px-3 py-1.5 bg-accent hover:bg-accent-hover text-white rounded text-xs font-medium transition-colors disabled:opacity-50"
            >
              <Search className="h-3 w-3" /> locate binary
            </button>
            <button
              onClick={handleInstall}
              className="flex items-center gap-1.5 px-3 py-1.5 border border-border text-text-secondary hover:text-text rounded text-xs font-medium transition-colors"
            >
              <Download className="h-3 w-3" /> reinstall with defaults
            </button>
          </>
        )}
        {!status?.isInstalled && !status?.isBinaryMissing && (
          <>
            <button
              onClick={handleInstall}
              className="flex items-center gap-1.5 px-3 py-1.5 bg-accent hover:bg-accent-hover text-white rounded text-xs font-medium transition-colors"
            >
              <Download className="h-3 w-3" /> install with defaults
            </button>
            <button
              onClick={() => {
                if (!safeId) return;
                // install_cmd comes from the registry (trusted but not
                // hermetic). Only honour it if it starts with a known
                // package-manager prefix; otherwise fall back to the
                // safe `eyrie install` command. This prevents a compromised
                // or mistyped registry entry from running arbitrary shell
                // against the user's tmux.
                const INSTALL_ALLOWLIST = /^\s*(cargo|pip|pip3|npm|pnpm|yarn|brew|apt|apt-get|dnf|choco|go|uv)\b/;
                const raw = framework.install_cmd;
                const cmd = raw && INSTALL_ALLOWLIST.test(raw)
                  ? raw
                  : `eyrie install ${safeId}`;
                sendToTerminal(cmd);
              }}
              className="flex items-center gap-1.5 px-3 py-1.5 border border-border text-text-secondary hover:text-text rounded text-xs font-medium transition-colors"
            >
              <TerminalIcon className="h-3 w-3" /> install manually
            </button>
          </>
        )}
        {status?.needsSetup && (
          <button
            onClick={() => {
              const bin = framework?.binary_path;
              const cmd = bin ? shellQuote(bin) : safeId;
              if (!cmd) return;
              sendToTerminal(`${cmd} onboard`);
            }}
            disabled={!framework?.binary_path && !safeId}
            className="flex items-center gap-1.5 px-3 py-1.5 bg-accent hover:bg-accent-hover text-white rounded text-xs font-medium transition-colors disabled:opacity-50"
          >
            <Settings className="h-3 w-3" /> set up
          </button>
        )}
        {status?.isInstalled && (
          <>
            <button
              onClick={() => {
                if (!safeId) return;
                const sub = CHAT_COMMANDS[safeId]?.split(" ").slice(1).join(" ") || "";
                const bin = framework?.binary_path;
                const cmd = bin
                  ? `${shellQuote(bin)}${sub ? " " + sub : ""}`
                  : CHAT_COMMANDS[safeId];
                if (cmd) sendToTerminal(cmd);
              }}
              className="flex items-center gap-1.5 px-3 py-1.5 border border-border text-text-secondary hover:text-text rounded text-xs font-medium transition-colors"
            >
              <TerminalIcon className="h-3 w-3" /> chat
            </button>
            <button
              onClick={() => navigate(`/agents/${id}/config`)}
              className="flex items-center gap-1.5 px-3 py-1.5 border border-border text-text-secondary hover:text-text rounded text-xs font-medium transition-colors"
            >
              <Settings className="h-3 w-3" /> configure
            </button>
          </>
        )}
        {/* Uninstall */}
        {(status?.isInstalled || status?.isConfigured) && !showUninstallConfirm && (
          <button
            onClick={() => setShowUninstallConfirm(true)}
            className="flex items-center gap-1.5 px-3 py-1.5 border border-red/30 text-red/70 hover:text-red hover:border-red/50 rounded text-xs font-medium transition-colors"
          >
            <Trash2 className="h-3 w-3" /> uninstall
          </button>
        )}
        {showUninstallConfirm && (
          <>
            <span className="text-xs text-text-muted">remove?</span>
            <label className="flex items-center gap-1.5 text-[10px] text-text-secondary cursor-pointer">
              <input
                type="checkbox"
                checked={uninstallPurge}
                onChange={(e) => setUninstallPurge(e.target.checked)}
                className="rounded border-border"
              />
              + config
            </label>
            <button
              onClick={handleUninstall}
              className="flex items-center gap-1.5 px-3 py-1.5 bg-red text-white rounded text-xs font-medium hover:bg-red/80 transition-colors"
            >
              <Trash2 className="h-3 w-3" /> confirm
            </button>
            <button
              onClick={() => { setShowUninstallConfirm(false); setUninstallPurge(false); }}
              className="text-xs text-text-muted hover:text-text transition-colors"
            >
              cancel
            </button>
          </>
        )}
      </div>

      {/* ── Terminal (always visible, persistent tmux session) ────────────── */}
      <div className="flex-1 min-h-0">
        <Terminal
          key={`shell-${safeId ?? "shell"}`}
          ref={termRef}
          agentName={safeId || "shell"}
          useShell
          inline
          session={safeId ? `eyrie-${safeId}` : undefined}
        />
      </div>

      {/* Agents on this framework */}
      <div>
        <h3 className="mb-2 text-[10px] font-medium uppercase tracking-wider text-text-muted">
          agents ({fwAgents.length})
        </h3>
        {fwAgents.length === 0 ? (
          <div className="rounded border border-dashed border-border px-4 py-4 text-center text-xs text-text-muted">
            no agents discovered on this framework
          </div>
        ) : (
          <div className="overflow-hidden rounded border border-border">
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-border bg-surface text-left text-text-muted">
                  <th className="px-4 py-2 font-medium">name</th>
                  <th className="px-4 py-2 font-medium">status</th>
                  <th className="px-4 py-2 font-medium">port</th>
                  <th className="px-4 py-2 font-medium">provider</th>
                  <th className="px-4 py-2 font-medium">model</th>
                </tr>
              </thead>
              <tbody className="[&>tr+tr]:border-t [&>tr+tr]:border-border">
                {fwAgents.map((agent) => (
                  <tr
                    key={agent.name}
                    onClick={() => navigate(`/agents/${agent.name}/chat`)}
                    className="group cursor-pointer transition-colors hover:bg-surface-hover/50"
                  >
                    <td className="px-4 py-2 transition-colors group-hover:text-accent">
                      <span className="flex items-center gap-2">
                        <span className={`h-1.5 w-1.5 rounded-full ${statusDotClass(agent.alive, agent.status?.provider_status)}`} />
                        {agent.display_name || agent.name}
                      </span>
                    </td>
                    <td className="px-4 py-2">
                      <span className={`rounded px-1.5 py-0.5 text-[10px] font-medium ${agent.alive ? "bg-green/10 text-green" : "bg-red/10 text-red"}`}>
                        {agent.alive ? "running" : "stopped"}
                      </span>
                    </td>
                    <td className="px-4 py-2 text-text-secondary">:{agent.port}</td>
                    <td className="px-4 py-2 text-text-secondary">{agent.status?.provider || "-"}</td>
                    <td className="px-4 py-2 text-text-secondary truncate max-w-48">{agent.status?.model || "-"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* API key hint */}
      {status?.isReady && framework.config_schema?.api_key_hint && (
        <div className="rounded border border-border bg-surface p-3 text-xs text-text-secondary">
          <span className="font-medium text-text-muted">api key: </span>
          {framework.config_schema.api_key_hint}
        </div>
      )}

      {/* ── Details (collapsible) ────────────────────────────────────────── */}
      <details className="group">
        <summary className="flex items-center gap-2 text-[10px] font-medium uppercase tracking-wider text-text-muted hover:text-text cursor-pointer transition-colors">
          <ChevronRight className="h-3 w-3 group-open:rotate-90 transition-transform" />
          framework details
        </summary>
        <div className="mt-3 space-y-4">
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <InfoItem label="language" value={framework.language} />
            <InfoItem label="install method" value={framework.install_method} />
            <InfoItem label="config format" value={framework.config_format} />
            <InfoItem label="binary" value={framework.binary_path} mono />
            <InfoItem label="config path" value={framework.config_path} mono />
            <InfoItem label="default port" value={framework.default_port ? `:${framework.default_port}` : "-"} />
            <InfoItem label="adapter" value={framework.adapter_type} />
            <InfoItem label="log directory" value={framework.log_dir} mono />
            <InfoItem label="log format" value={framework.log_format} />
          </div>
          {(framework.repository || framework.website) && (
            <div className="flex gap-3">
              {framework.repository && (
                <a href={framework.repository} target="_blank" rel="noopener noreferrer" className="flex items-center gap-1.5 text-xs text-text-secondary hover:text-accent transition-colors">
                  <ExternalLink className="h-3 w-3" /> repository
                </a>
              )}
              {framework.website && (
                <a href={framework.website} target="_blank" rel="noopener noreferrer" className="flex items-center gap-1.5 text-xs text-text-secondary hover:text-accent transition-colors">
                  <ExternalLink className="h-3 w-3" /> website
                </a>
              )}
            </div>
          )}
        </div>
      </details>
    </div>
  );
}

function InfoItem({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="rounded border border-border bg-surface p-3">
      <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">{label}</p>
      <p className={`mt-1 text-xs text-text truncate ${mono ? "font-mono" : ""}`} title={value}>
        {value || "-"}
      </p>
    </div>
  );
}
