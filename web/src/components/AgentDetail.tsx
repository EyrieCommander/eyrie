import { useEffect, useState, useCallback, useRef } from "react";
import { useAutoScroll } from "../lib/useAutoScroll";
import { useParams, Link } from "react-router-dom";
import {
  Play,
  Square,
  RotateCcw,
  Edit3,
  Save,
  X,
  CheckCircle,
  Settings,
  Terminal as TerminalIcon,
} from "lucide-react";
import type {
  AgentInfo,
  LogEntry,
  Framework,
  ConfigField,
} from "../lib/types";
import {
  agentAction,
  fetchAgents,
  fetchAgentConfig,
  fetchAgentModels,
  type AgentConfig,
  streamLogs,
  updateAgentConfig,
  updateDisplayName,
  validateAgentConfig,
  getFrameworkDetail,
} from "../lib/api";
import ConfigEditor from "./ConfigEditor";
import Terminal from "./Terminal";
import { ChatPanel } from "./ChatPanel";
import { formatBytes } from "../lib/format";
import { useData } from "../lib/DataContext";

interface AgentDetailProps {
  agent: AgentInfo;
  onRefresh?: () => Promise<void> | void;
}

const validTabs = ["chat", "config"] as const;
type Tab = (typeof validTabs)[number];

export default function AgentDetail({ agent, onRefresh }: AgentDetailProps) {
  const { tab: tabParam } = useParams<{ tab?: string }>();

  const tab: Tab = validTabs.includes(tabParam as Tab)
    ? (tabParam as Tab)
    : "chat";

  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [config, setConfig] = useState<AgentConfig | null>(null);
  const [configError, setConfigError] = useState<string | null>(null);
  const [actionPending, setActionPending] = useState<string | false>(false);
  const [framework, setFramework] = useState<Framework | null>(null);
  const [showTerminal, setShowTerminal] = useState(false);
  const [editingName, setEditingName] = useState(false);
  const [nameInput, setNameInput] = useState("");
  const [nameSaving, setNameSaving] = useState(false);
  const [nameError, setNameError] = useState<string | null>(null);
  const { setPendingAction } = useData();

  const actionControllerRef = useRef<AbortController | null>(null);

  // cleanup on unmount
  useEffect(() => {
    return () => { actionControllerRef.current?.abort(); };
  }, []);

  const handleAction = useCallback(
    async (action: "start" | "stop" | "restart") => {
      setActionPending(action);
      setPendingAction(agent.name, action);
      const controller = new AbortController();
      actionControllerRef.current = controller;
      try {
        await agentAction(agent.name, action);
        if (onRefresh) {
          if (action === "start" || action === "restart") {
            for (let i = 0; i < 10; i++) {
              if (controller.signal.aborted) break;
              await new Promise((r) => setTimeout(r, 1000));
              if (controller.signal.aborted) break;
              await onRefresh();
              try {
                const agents = await fetchAgents();
                const a = agents.find((x: any) => x.name === agent.name);
                if (a?.alive) break;
              } catch { /* ignore */ }
            }
          } else {
            await onRefresh();
          }
        }
      } catch (e) {
        console.error(e);
      } finally {
        setActionPending(false);
        setPendingAction(agent.name, null);
      }
    },
    [agent.name, onRefresh, setPendingAction],
  );

  useEffect(() => {
    if (tab === "config") {
      setLogs([]);
      const close = streamLogs(agent.name, (entry) => {
        setLogs((prev) => [...prev.slice(-200), entry]);
      });
      return close;
    }
  }, [tab, agent.name, agent.alive]);

  useEffect(() => {
    setConfig(null);
    setConfigError(null);
    fetchAgentConfig(agent.name)
      .then(setConfig)
      .catch((err) => setConfigError(err.message ?? "Failed to load config"));
  }, [agent.name, agent.alive]);

  useEffect(() => {
    // Fetch framework detail with schema for inline editing
    getFrameworkDetail(agent.framework)
      .then(setFramework)
      .catch((err) => console.error("Failed to load framework:", err));
  }, [agent.framework]);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <Link
          to="/agents/overview"
          className="text-xs text-text-muted transition-colors hover:text-text"
        >
          &lt; back
        </Link>

        <div className="flex gap-2">
          {!agent.alive ? (
            <ActionButton
              icon={<Play className="h-3.5 w-3.5" />}
              label={actionPending === "start" ? "starting..." : "start"}
              onClick={() => handleAction("start")}
              disabled={!!actionPending}
            />
          ) : (
            <>
              <ActionButton
                icon={<TerminalIcon className="h-3.5 w-3.5" />}
                label="terminal"
                onClick={() => setShowTerminal(true)}
                disabled={false}
              />
              <ActionButton
                icon={actionPending === "restart" ? <span className="h-3.5 w-3.5 animate-spin rounded-full border-2 border-yellow-400/30 border-t-yellow-400" /> : <RotateCcw className="h-3.5 w-3.5" />}
                label={actionPending === "restart" ? "restarting..." : "restart"}
                onClick={() => handleAction("restart")}
                disabled={!!actionPending}
              />
              <ActionButton
                icon={<Square className="h-3.5 w-3.5" />}
                label={actionPending === "stop" ? "stopping..." : "stop"}
                onClick={() => handleAction("stop")}
                disabled={!!actionPending}
                variant={actionPending === "stop" ? undefined : "danger"}
              />
            </>
          )}
        </div>
      </div>

      <div>
        <div className="flex items-center gap-3">
          <span
            className={`h-3 w-3 rounded-full ${actionPending ? "bg-yellow-400 animate-pulse" : !agent.alive ? "bg-red" : agent.status?.provider_status === "error" ? "bg-yellow" : "bg-green"}`}
          />
          {editingName ? (
            <form
              className="flex items-center gap-1.5"
              onSubmit={async (e) => {
                e.preventDefault();
                const cleaned = nameInput.replace(/[^a-zA-Z0-9 \-_]/g, "").trim();
                if (!cleaned) { setEditingName(false); return; }
                setNameSaving(true);
                try {
                  await updateDisplayName(agent.name, cleaned);
                  if (onRefresh) await onRefresh();
                } catch (err) {
                  setNameError(err instanceof Error ? err.message : "failed to save name");
                }
                setNameSaving(false);
                setEditingName(false);
              }}
            >
              <input
                autoFocus
                value={nameInput}
                onChange={(e) => setNameInput(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Escape") setEditingName(false); }}
                className="text-xl font-bold bg-transparent border-b border-accent text-text outline-none w-48"
                disabled={nameSaving}
              />
              <button type="submit" disabled={nameSaving} className="p-1 text-accent hover:text-accent-hover">
                <Save className="h-3.5 w-3.5" />
              </button>
              <button type="button" onClick={() => setEditingName(false)} className="p-1 text-text-muted hover:text-text">
                <X className="h-3.5 w-3.5" />
              </button>
            </form>
          ) : (
            <button
              onClick={() => { setNameInput(agent.display_name || agent.name); setEditingName(true); setNameError(null); }}
              className="group relative"
            >
              <h2 className="text-xl font-bold">{agent.display_name || agent.name}</h2>
              <Edit3 className="absolute -top-1 -right-3 h-3 w-3 text-text-muted opacity-0 group-hover:opacity-100 transition-opacity" />
            </button>
          )}
          <span className="rounded border border-border-strong bg-surface-hover px-2 py-0.5 text-[11px] text-text-secondary">
            {agent.framework}
          </span>
        </div>
        <p className="mt-1 text-xs text-text-muted">
          // gateway: {agent.host}:{agent.port}
        </p>
        {nameError && (
          <p className="mt-1 text-xs text-red">{nameError}</p>
        )}
      </div>

      <div className="flex border-b border-border">
        {validTabs.map((t) => (
          <TabLink
            key={t}
            to={`/agents/${agent.name}/${t}`}
            active={tab === t}
          >
            {t}
          </TabLink>
        ))}
      </div>

      {tab === "config" && <OverviewTab agent={agent} framework={framework} config={config} configError={configError} logs={logs} onConfigChange={() => {
        if (onRefresh) onRefresh();
      }} onConfigSaved={() => {
        fetchAgentConfig(agent.name)
          .then(setConfig)
          .catch((err) => setConfigError(err.message ?? "Failed to load config"));
      }} />}
      {tab === "chat" && (
        <ChatPanel key={agent.name} alive={agent.alive} framework={agent.framework} agentName={agent.name} />
      )}

      {/* Terminal Modal */}
      {showTerminal && (
        <Terminal agentName={agent.name} onClose={() => setShowTerminal(false)} />
      )}
    </div>
  );
}

function OverviewTab({
  agent,
  framework,
  config,
  configError,
  logs,
  onConfigChange,
  onConfigSaved,
}: {
  agent: AgentInfo;
  framework: Framework | null;
  config: AgentConfig | null;
  configError: string | null;
  logs: LogEntry[];
  onConfigChange: () => void;
  onConfigSaved: () => void;
}) {
  const logScrollRef = useAutoScroll([logs.length]);
  const health = agent.health;
  const status = agent.status;
  const [configEditing, setConfigEditing] = useState(false);
  const [editedContent, setEditedContent] = useState("");
  const [configSaving, setConfigSaving] = useState(false);
  const [configSaveError, setConfigSaveError] = useState<string | null>(null);
  const [configSaveSuccess, setConfigSaveSuccess] = useState(false);

  useEffect(() => {
    if (config) setEditedContent(config.content);
  }, [config]);

  // Find editable fields from framework schema.
  // Try exact key match first, then fall back to suffix match
  // (e.g., "model" matches "default_model" for ZeroClaw).
  const getEditableField = (key: string) => {
    const fields = framework?.config_schema?.common_fields;
    if (!fields) return undefined;
    return fields.find(f => f.key === key)
      ?? fields.find(f => f.key.endsWith("_" + key) || f.key.endsWith("." + key));
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <InfoCard
          label="STATUS"
          value={agent.alive ? "running" : "stopped"}
          highlight={agent.alive}
        />
        <InfoCard label="PID" value={health?.pid?.toString() ?? "-"} />
        <InfoCard
          label="UPTIME"
          value={
            health?.uptime
              ? (() => {
                  const s = health.uptime / 1e9;
                  const d = Math.floor(s / 86400);
                  const h = Math.floor((s % 86400) / 3600);
                  const m = Math.floor((s % 3600) / 60);
                  if (d > 0) return `${d}d ${h}h`;
                  return `${h}h ${m}m`;
                })()
              : "-"
          }
        />
        <InfoCard
          label="MEMORY"
          value={health?.ram_bytes ? formatBytes(health.ram_bytes) : "-"}
        />
        <InfoCard
          label="CPU"
          value={
            health?.cpu_percent != null
              ? `${health.cpu_percent.toFixed(1)}%`
              : "-"
          }
        />
        <EditableInfoCard
          label="PROVIDER"
          value={status?.provider ?? "-"}
          field={getEditableField("provider")}
          agentName={agent.name}
          onSave={onConfigChange}
        />
        <InfoCard
          label="PROVIDER HEALTH"
          value={!agent.alive ? "-" : !status?.provider_status ? "unknown" : status.provider_status === "ok" ? "reachable" : "unreachable"}
          warn={agent.alive && status?.provider_status === "error"}
          success={agent.alive && status?.provider_status === "ok"}
        />
        <EditableInfoCard
          label="MODEL"
          value={status?.model ?? "-"}
          field={getEditableField("model")}
          agentName={agent.name}
          onSave={onConfigChange}
        />
        <InfoCard
          label="CHANNELS"
          value={status?.channels?.join(", ") || "-"}
        />
      </div>

      {health?.components && Object.keys(health.components).length > 0 && (
        <div>
          <h3 className="mb-2 text-[10px] font-medium uppercase tracking-wider text-text-muted">
            Components
          </h3>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {Object.entries(health.components).map(([name, comp]) => (
              <div
                key={name}
                className="rounded border border-border bg-surface p-3"
              >
                <div className="flex items-center justify-between">
                  <span className="text-xs font-medium">{name}</span>
                  <span
                    className={`text-[10px] ${comp.status === "ok" ? "text-green" : "text-red"}`}
                  >
                    {comp.status}
                  </span>
                </div>
                {comp.restart_count > 0 && (
                  <p className="mt-1 text-[10px] text-yellow">
                    restarts: {comp.restart_count}
                  </p>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Logs section */}
      <details className="group rounded border border-border bg-surface" open>
        <summary className="flex items-center justify-between px-4 py-3 cursor-pointer select-none text-[10px] font-medium uppercase tracking-wider text-text-muted hover:text-text transition-colors">
          <span>logs {logs.length > 0 ? `(${logs.length})` : ""}</span>
          <span className="text-text-muted group-open:rotate-90 transition-transform">▶</span>
        </summary>
        <div className="px-4 pb-4">
          <div ref={logScrollRef} className="max-h-48 overflow-y-auto rounded border border-border bg-bg p-3 text-xs">
            {logs.length === 0 ? (
              <p className="text-text-muted">
                {agent.alive ? "waiting for log entries..." : "no log history available."}
              </p>
            ) : (
              logs.map((entry, i) => (
                <div key={i} className="py-0.5">
                  <span className="text-text-muted">
                    {new Date(entry.timestamp).toLocaleTimeString()}
                  </span>{" "}
                  <span className={`font-medium ${
                    entry.level === "error" ? "text-red"
                    : entry.level === "warn" ? "text-yellow"
                    : "text-green"
                  }`}>
                    [{entry.level}]
                  </span>{" "}
                  <span className="text-text">{entry.message}</span>
                </div>
              ))
            )}
          </div>
        </div>
      </details>

      {/* Config section */}
      <details className="group rounded border border-border bg-surface">
        <summary className="flex items-center justify-between px-4 py-3 cursor-pointer select-none text-[10px] font-medium uppercase tracking-wider text-text-muted hover:text-text transition-colors">
          <span>configuration {config?.format ? `(${config.format})` : ""}</span>
          <span className="text-text-muted group-open:rotate-90 transition-transform">▶</span>
        </summary>

        {configError ? (
          <p className="px-4 pb-3 text-xs text-red">failed to load config: {configError}</p>
        ) : !config ? (
          <p className="px-4 pb-3 text-xs text-text-muted">loading...</p>
        ) : configEditing ? (
          <div className="px-4 pb-4 space-y-3">
            {configSaveError && (
              <div className="p-2 bg-red/10 border border-red/20 rounded text-xs text-red">
                {configSaveError}
              </div>
            )}
            <ConfigEditor
              value={editedContent}
              format={config.format}
              onChange={setEditedContent}
            />
            <div className="flex items-center gap-2">
              <button
                onClick={async () => {
                  try {
                    setConfigSaving(true);
                    setConfigSaveError(null);
                    const validation = await validateAgentConfig(agent.name, editedContent);
                    if (!validation.valid) {
                      setConfigSaveError(validation.error || "configuration is invalid");
                      return;
                    }
                    await updateAgentConfig(agent.name, editedContent);
                    setConfigEditing(false);
                    setConfigSaveSuccess(true);
                    setTimeout(() => setConfigSaveSuccess(false), 3000);
                    onConfigSaved();
                  } catch (err) {
                    setConfigSaveError(err instanceof Error ? err.message : "failed to save");
                  } finally {
                    setConfigSaving(false);
                  }
                }}
                disabled={configSaving}
                className="flex items-center gap-1.5 px-3 py-1.5 bg-accent hover:bg-accent-hover text-white rounded text-xs font-medium transition-colors disabled:opacity-50"
              >
                <Save className="h-3 w-3" />
                {configSaving ? "saving..." : "save"}
              </button>
              <button
                onClick={() => { setConfigEditing(false); setEditedContent(config.content); setConfigSaveError(null); }}
                disabled={configSaving}
                className="flex items-center gap-1.5 px-3 py-1.5 border border-border text-text-secondary rounded text-xs font-medium transition-colors hover:text-text disabled:opacity-50"
              >
                <X className="h-3 w-3" />
                cancel
              </button>
            </div>
          </div>
        ) : (
          <div className="px-4 pb-4 space-y-2">
            {configSaveSuccess && (
              <div className="p-2 bg-green/10 border border-green/20 rounded text-xs text-green flex items-center gap-1.5">
                <CheckCircle className="h-3 w-3" />
                saved — restart agent to apply
              </div>
            )}
            <div className="flex justify-end">
              {agent.alive && (
                <button
                  onClick={() => setConfigEditing(true)}
                  className="flex items-center gap-1.5 px-2.5 py-1 text-[10px] text-text-muted hover:text-accent transition-colors"
                >
                  <Edit3 className="h-3 w-3" />
                  edit
                </button>
              )}
            </div>
            <pre className="max-h-64 overflow-auto rounded border border-border bg-bg p-3 text-xs leading-relaxed">
              {config.format === "json"
                ? highlightJson((() => { try { return JSON.stringify(JSON.parse(config.content), null, 2); } catch { return config.content; } })())
                : highlightToml(config.content)}
            </pre>
          </div>
        )}
      </details>
    </div>
  );
}

function highlightToml(text: string) {
  return text.split("\n").map((line, i) => {
    const trimmed = line.trim();
    if (trimmed.startsWith("#")) {
      return <div key={i} className="text-text-muted">{line}</div>;
    }
    if (/^\[.*\]$/.test(trimmed)) {
      return <div key={i} className="text-accent font-semibold mt-3 first:mt-0">{line}</div>;
    }
    const eqIdx = line.indexOf("=");
    if (eqIdx > 0 && !trimmed.startsWith("[")) {
      const key = line.slice(0, eqIdx);
      const val = line.slice(eqIdx);
      return (
        <div key={i}>
          <span className="text-text">{key}</span>
          <span className="text-text-muted">=</span>
          <span className="text-green">{val.slice(1)}</span>
        </div>
      );
    }
    return <div key={i} className="text-text">{line}</div>;
  });
}

function highlightJson(text: string) {
  return text.split("\n").map((line, i) => {
    const keyMatch = line.match(/^(\s*)"([^"]+)"(\s*:\s*)(.*)/);
    if (keyMatch) {
      const [, indent, key, sep, rest] = keyMatch;
      return (
        <div key={i}>
          <span>{indent}</span>
          <span className="text-text">"{key}"</span>
          <span className="text-text-muted">{sep}</span>
          <span className="text-green">{rest}</span>
        </div>
      );
    }
    return <div key={i} className="text-text-muted">{line}</div>;
  });
}

function InfoCard({
  label,
  value,
  highlight,
  warn,
  success,
}: {
  label: string;
  value: string;
  highlight?: boolean;
  warn?: boolean;
  success?: boolean;
}) {
  return (
    <div className="rounded border border-border bg-surface p-4">
      <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
        {label}
      </p>
      <p
        className={`mt-1.5 text-lg font-semibold ${warn ? "text-yellow" : success ? "text-green" : highlight ? "text-accent" : "text-text"}`}
      >
        {value}
      </p>
    </div>
  );
}

function EditableInfoCard({
  label,
  value,
  field,
  agentName,
  onSave: _onSave,
}: {
  label: string;
  value: string;
  field: ConfigField | undefined;
  agentName: string;
  onSave: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [editValue, setEditValue] = useState(value);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);
  const [providerModels, setProviderModels] = useState<string[] | null>(null);

  useEffect(() => {
    setEditValue(value);
    setSaved(false);
  }, [value]);

  // Fetch available models from the provider when editing a model field
  const isModelField = field?.key.endsWith("model") || field?.key.endsWith("default_model");
  useEffect(() => {
    if (editing && isModelField) {
      fetchAgentModels(agentName).then((models) => {
        setProviderModels(models.length > 0 ? models : null);
      }).catch(() => setProviderModels(null));
    }
  }, [editing, isModelField, agentName]);

  const handleSave = async () => {
    if (!field) return;

    try {
      setSaving(true);
      setError(null);

      // Fetch current config as raw text
      const config = await fetchAgentConfig(agentName);

      let updated: string;
      if (config.format === "json") {
        // JSON: parse, modify, re-stringify (lossless for JSON)
        const parsed = JSON.parse(config.content);
        const parts = field.key.split(".");
        let current = parsed;
        for (let i = 0; i < parts.length - 1; i++) {
          if (!current[parts[i]]) current[parts[i]] = {};
          current = current[parts[i]];
        }
        if (field.type === "number") {
          const num = Number(editValue);
          if (isNaN(num)) {
            setError("Invalid number");
            return;
          }
          current[parts[parts.length - 1]] = num;
        } else {
          current[parts[parts.length - 1]] = editValue;
        }
        updated = JSON.stringify(parsed, null, 2);
      } else if (config.format === "toml") {
        // TOML: targeted string replacement to preserve formatting and types
        updated = replaceTomlValue(config.content, field.key, editValue, field.type);
      } else if (config.format === "yaml") {
        // YAML: targeted string replacement similar to TOML. Pass the
        // field type so numbers/booleans stay unquoted and strings get
        // properly escaped.
        updated = replaceYamlValue(config.content, field.key, editValue, field.type);
      } else {
        throw new Error(`Unsupported config format: ${config.format}`);
      }

      // Send as raw string so backend writes it directly (no re-encoding)
      await updateAgentConfig(agentName, updated);

      setEditing(false);
      setSaved(true);
      // Don't call _onSave() (which reloads the page) — the runtime status
      // won't reflect config changes until the agent is restarted.
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to save");
    } finally {
      setSaving(false);
    }
  };

  const handleCancel = () => {
    setEditValue(value);
    setEditing(false);
    setError(null);
  };

  if (!field) {
    // Not editable, render as normal InfoCard
    return <InfoCard label={label} value={value} />;
  }

  return (
    <div className="rounded border border-border bg-surface p-4">
      <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
        {label}
      </p>

      {editing ? (
        <div className="mt-2 space-y-2">
          {field.type === "select" ? (
            <select
              value={editValue}
              onChange={(e) => setEditValue(e.target.value)}
              disabled={saving}
              className="w-full px-3 py-1.5 bg-bg-subtle border border-border rounded text-sm text-fg
                focus:outline-none focus:ring-2 focus:ring-accent/50 disabled:opacity-50"
            >
              {field.options?.map((opt) => (
                <option key={opt} value={opt}>
                  {opt}
                </option>
              ))}
            </select>
          ) : field.type === "number" ? (
            <input
              type="number"
              value={editValue}
              onChange={(e) => setEditValue(e.target.value)}
              disabled={saving}
              min={field.min}
              max={field.max}
              className="w-full px-3 py-1.5 bg-bg-subtle border border-border rounded text-sm text-fg
                focus:outline-none focus:ring-2 focus:ring-accent/50 disabled:opacity-50"
            />
          ) : isModelField && providerModels ? (
            <select
              value={editValue}
              onChange={(e) => setEditValue(e.target.value)}
              disabled={saving}
              className="w-full px-3 py-1.5 bg-bg-subtle border border-border rounded text-sm text-fg
                focus:outline-none focus:ring-2 focus:ring-accent/50 disabled:opacity-50"
            >
              {!providerModels.includes(editValue) && (
                <option value={editValue}>{editValue}</option>
              )}
              {[...providerModels].sort().map((m) => (
                <option key={m} value={m}>{m}</option>
              ))}
            </select>
          ) : (
            <input
              type="text"
              value={editValue}
              onChange={(e) => setEditValue(e.target.value)}
              disabled={saving}
              className="w-full px-3 py-1.5 bg-bg-subtle border border-border rounded text-sm text-fg
                focus:outline-none focus:ring-2 focus:ring-accent/50 disabled:opacity-50"
            />
          )}

          {error && (
            <p className="text-xs text-red">{error}</p>
          )}

          <div className="flex gap-2">
            <button
              onClick={handleSave}
              disabled={saving}
              className="px-3 py-1 bg-accent hover:bg-accent-hover text-white rounded text-xs
                font-medium transition-colors disabled:opacity-50"
            >
              {saving ? "saving..." : "save"}
            </button>
            <button
              onClick={handleCancel}
              disabled={saving}
              className="px-3 py-1 bg-bg-subtle hover:bg-bg-muted border border-border text-fg
                rounded text-xs font-medium transition-colors disabled:opacity-50"
            >
              cancel
            </button>
          </div>
        </div>
      ) : (
        <div className="mt-1.5 flex items-center justify-between group">
          <div>
            <p className="text-lg font-semibold text-text">
              {saved ? editValue : value}
            </p>
            {saved && (
              <p className="text-[10px] text-green mt-0.5">saved — restart agent to apply</p>
            )}
          </div>
          <button
            onClick={() => setEditing(true)}
            className="opacity-0 group-hover:opacity-100 transition-opacity p-1 hover:bg-bg-muted
              rounded text-fg-muted hover:text-fg"
            title="edit"
          >
            <Settings className="w-4 h-4" />
          </button>
        </div>
      )}
    </div>
  );
}

// Replace a single value in raw TOML text without re-parsing the whole file.
// For top-level keys (e.g., "model"), finds the line before any [section].
// For nested keys (e.g., "gateway.port"), finds the key within its section.
function replaceTomlValue(content: string, fieldKey: string, newValue: string, fieldType?: string): string {
  const parts = fieldKey.split(".");
  const lines = content.split("\n");

  // Format the replacement value — numbers and booleans are unquoted in TOML
  const formatted = fieldType === "number" ? newValue
    : fieldType === "boolean" ? (newValue === "true" ? "true" : "false")
    : `"${escapeTomlString(newValue)}"`;


  if (parts.length === 1) {
    // Top-level key: replace the first matching `key = value` line
    const key = parts[0];
    const re = new RegExp(`^(\\s*${escapeRegex(key)}\\s*=\\s*).*$`);
    for (let i = 0; i < lines.length; i++) {
      // Stop at first section header — key must be in the global scope
      if (lines[i].trim().startsWith("[")) break;
      if (re.test(lines[i])) {
        lines[i] = lines[i].replace(re, `$1${formatted}`);
        return lines.join("\n");
      }
    }
  } else {
    // Nested key: find [section] then the key within it
    const section = parts.slice(0, -1).join(".");
    const key = parts[parts.length - 1];
    const sectionHeader = `[${section}]`;
    const re = new RegExp(`^(\\s*${escapeRegex(key)}\\s*=\\s*).*$`);
    let inSection = false;
    let sectionStartIndex = -1;

    for (let i = 0; i < lines.length; i++) {
      const trimmed = lines[i].trim();
      if (trimmed === sectionHeader) {
        inSection = true;
        sectionStartIndex = i;
        continue;
      }
      // Exit section when a new section starts
      if (inSection && trimmed.startsWith("[")) break;
      if (inSection && re.test(lines[i])) {
        lines[i] = lines[i].replace(re, `$1${formatted}`);
        return lines.join("\n");
      }
    }

    // Key not found in existing section — append it
    if (inSection && sectionStartIndex >= 0) {
      let insertAt = lines.length;
      for (let j = sectionStartIndex + 1; j < lines.length; j++) {
        if (lines[j].trim().startsWith("[")) {
          insertAt = j;
          break;
        }
      }
      lines.splice(insertAt, 0, `${key} = ${formatted}`);
      return lines.join("\n");
    }

    // Section not found — create it at the end of the file
    lines.push("", `[${section}]`, `${key} = ${formatted}`);
    return lines.join("\n");
  }

  // Field not found — append it (top-level or to section)
  if (parts.length === 1) {
    // Prepend to file (before first section)
    const firstSection = lines.findIndex((l) => l.trim().startsWith("["));
    const insertAt = firstSection === -1 ? lines.length : firstSection;
    lines.splice(insertAt, 0, `${parts[0]} = ${formatted}`);
  }
  return lines.join("\n");
}

function escapeTomlString(s: string): string {
  return s
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\n/g, "\\n")
    .replace(/\r/g, "\\r")
    .replace(/\t/g, "\\t");
}

/** Format a value for YAML based on its field type. Strings are quoted and
 *  escaped to avoid corruption when they contain special YAML characters
 *  (colons, hashes, leading dashes, etc). Numbers and booleans are emitted
 *  unquoted. Unknown types default to strings for safety. */
function formatYamlValue(value: string, fieldType?: string): string {
  if (fieldType === "number") {
    if (value.trim() === "") throw new Error("Number field cannot be empty");
    const n = Number(value);
    if (!Number.isFinite(n)) throw new Error("Invalid number");
    return String(n);
  }
  if (fieldType === "boolean") {
    if (value.trim() === "") throw new Error("Boolean field cannot be empty");
    return value === "true" ? "true" : "false";
  }
  // String: always double-quote + escape. This sidesteps every edge case
  // with special characters like ":", "#", leading "-", "!", etc.
  const escaped = value
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"')
    .replace(/\n/g, "\\n")
    .replace(/\r/g, "\\r")
    .replace(/\t/g, "\\t");
  return `"${escaped}"`;
}

/** Replace a value in YAML content. Supports nested keys like "gateway.port".
 *  Only enters a parent block when the parent line ends with a colon and has
 *  no inline value (i.e., it's actually a block parent, not a scalar). */
function replaceYamlValue(content: string, fieldKey: string, newValue: string, fieldType?: string): string {
  const parts = fieldKey.split(".");
  const lines = content.split("\n");
  const key = parts[parts.length - 1];
  const parentPath = parts.slice(0, -1);
  const formatted = formatYamlValue(newValue, fieldType);

  // Find the line matching the key at the correct indentation depth.
  // YAML nesting uses 2-space indentation per level.
  const expectedIndent = parentPath.length * 2;
  const re = new RegExp(`^(\\s{${expectedIndent}}${escapeRegex(key)}:\\s*)(.*)$`);

  // A line qualifies as a block parent only when it's "key:" or "key: # comment"
  // (i.e., the colon is followed by nothing but whitespace and/or a comment).
  const isBlockParent = (trimmed: string, parentKey: string): boolean => {
    const prefix = parentKey + ":";
    if (!trimmed.startsWith(prefix)) return false;
    const rest = trimmed.slice(prefix.length).trim();
    return rest === "" || rest.startsWith("#");
  };

  // Track which parent keys we've entered.
  let depth = 0;
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const trimmed = line.trimStart();
    const indent = line.length - trimmed.length;

    // Exit ancestor blocks when indentation decreases so we don't keep
    // thinking we're inside a block we've already left.
    while (depth > 0 && trimmed !== "" && !trimmed.startsWith("#") && indent < depth * 2) {
      depth--;
    }

    // Descend into the next parent block when we see it.
    if (depth < parentPath.length) {
      const parentKey = parentPath[depth];
      if (indent === depth * 2 && isBlockParent(trimmed, parentKey)) {
        depth++;
        continue;
      }
    }

    // Found our target key at the right depth
    if (depth === parentPath.length && re.test(line)) {
      const match = line.match(re);
      const existingValue = match?.[2]?.trim() ?? "";
      // Detect block scalar indicators (| or >) — the value spans multiple
      // lines with greater indentation. Remove the header + all continuation
      // lines before inserting the new inline value.
      if (existingValue.startsWith("|") || existingValue.startsWith(">")) {
        const blockIndent = indent + 2; // continuation lines have at least this indent
        let endOfBlock = i + 1;
        while (endOfBlock < lines.length) {
          const nextLine = lines[endOfBlock];
          const nextTrimmed = nextLine.trimStart();
          const nextIndent = nextLine.length - nextTrimmed.length;
          // Blank lines are part of the block; non-blank lines with
          // less indentation terminate it.
          if (nextTrimmed !== "" && nextIndent < blockIndent) break;
          endOfBlock++;
        }
        // Replace header + continuation range with a single inline value
        lines.splice(i, endOfBlock - i, line.replace(re, `$1${formatted}`));
      } else {
        lines[i] = line.replace(re, `$1${formatted}`);
      }
      return lines.join("\n");
    }
  }

  // Key not found — find the deepest existing ancestor and insert under it.
  // Blindly appending every parent would duplicate sections; refusing to do
  // anything would leave the value unset. Compromise: walk existing lines to
  // locate the deepest prefix of parentPath that's already present, then
  // append the remaining nested blocks below that.
  if (parentPath.length === 0) {
    lines.push(`${key}: ${formatted}`);
    return lines.join("\n");
  }

  let deepestMatchedDepth = 0; // how many of parentPath[] were found in order
  let deepestMatchedLine = -1; // last line index inside the matched block
  {
    let d = 0;
    for (let i = 0; i < lines.length; i++) {
      if (d >= parentPath.length) break;
      const line = lines[i];
      const trimmed = line.trimStart();
      const indent = line.length - trimmed.length;
      if (indent !== d * 2) continue;
      // Require "key:" with nothing else on the line (a block parent)
      const expected = parentPath[d] + ":";
      if (trimmed === expected || trimmed.startsWith(expected + " ") || trimmed.startsWith(expected + "\t")) {
        const rest = trimmed.slice(expected.length).trim();
        if (rest === "" || rest.startsWith("#")) {
          d++;
          deepestMatchedDepth = d;
          deepestMatchedLine = i;
        }
      }
    }
  }

  if (deepestMatchedDepth === 0) {
    // Not even the top-level parent is present. Rather than silently
    // fabricating the whole hierarchy (which risks corrupting the file),
    // throw — the caller should decide whether to create the section.
    throw new Error(`cannot locate YAML parent "${parentPath[0]}" for key "${fieldKey}"`);
  }

  // Insert after the last line of the matched block. Append any remaining
  // parents (those we couldn't match) as nested blocks under it.
  const insertLines: string[] = [];
  for (let d = deepestMatchedDepth; d < parentPath.length; d++) {
    insertLines.push(`${"  ".repeat(d)}${parentPath[d]}:`);
  }
  insertLines.push(`${"  ".repeat(parentPath.length)}${key}: ${formatted}`);
  lines.splice(deepestMatchedLine + 1, 0, ...insertLines);
  return lines.join("\n");
}

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function ActionButton({
  icon,
  label,
  onClick,
  disabled,
  variant = "default",
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  disabled: boolean;
  variant?: "default" | "danger";
}) {
  const base =
    "flex items-center gap-1.5 rounded px-3 py-1.5 text-xs font-medium transition-colors disabled:opacity-50";
  const styles =
    variant === "danger"
      ? "border border-red/30 text-red hover:bg-red/10"
      : "border border-border text-text-secondary hover:bg-surface-hover hover:text-text";

  return (
    <button onClick={onClick} disabled={disabled} className={`${base} ${styles}`}>
      {icon}
      $ {label}
    </button>
  );
}

function TabLink({
  to,
  active,
  children,
}: {
  to: string;
  active: boolean;
  children: React.ReactNode;
}) {
  return (
    <Link
      to={to}
      replace
      className={`px-5 py-2.5 text-xs font-medium transition-colors ${
        active
          ? "border-b-2 border-accent text-accent"
          : "text-text-secondary hover:text-text"
      }`}
    >
      {children}
    </Link>
  );
}
