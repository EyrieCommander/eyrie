import { useEffect, useState, useCallback, useRef } from "react";
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
  fetchAgentConfig,
  fetchAgentModels,
  type AgentConfig,
  streamLogs,
  updateAgentConfig,
  validateAgentConfig,
  getFrameworkDetail,
} from "../lib/api";
import ConfigEditor from "./ConfigEditor";
import Terminal from "./Terminal";
import { ChatPanel } from "./ChatPanel";

function formatBytes(bytes: number): string {
  if (!bytes) return "-";
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)}KB`;
  if (bytes < 1024 * 1024 * 1024)
    return `${(bytes / (1024 * 1024)).toFixed(0)}MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)}GB`;
}

interface AgentDetailProps {
  agent: AgentInfo;
  onRefresh?: () => Promise<void> | void;
}

const validTabs = ["status", "chat", "logs", "config"] as const;
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

  const handleAction = useCallback(
    async (action: "start" | "stop" | "restart") => {
      setActionPending(action);
      try {
        await agentAction(agent.name, action);
        if (onRefresh) {
          if (action === "start" || action === "restart") {
            // Poll until alive or timeout (daemon takes a moment to start)
            for (let i = 0; i < 10; i++) {
              await new Promise((r) => setTimeout(r, 1000));
              await onRefresh();
              // agent prop updates on next render, but we can't read it here.
              // Break early by checking the API directly.
              try {
                const agents = await (await fetch("/api/agents")).json();
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
      }
    },
    [agent.name, onRefresh],
  );

  useEffect(() => {
    if (tab === "logs") {
      setLogs([]);
      const close = streamLogs(agent.name, (entry) => {
        setLogs((prev) => [...prev.slice(-200), entry]);
      });
      return close;
    }
  }, [tab, agent.name, agent.alive]);

  useEffect(() => {
    if (tab === "config") {
      setConfig(null);
      setConfigError(null);
      fetchAgentConfig(agent.name)
        .then(setConfig)
        .catch((err) => setConfigError(err.message ?? "Failed to load config"));
    }
  }, [tab, agent.name, agent.alive]);

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
          <h2 className="text-xl font-bold">{agent.name}</h2>
          <span className="rounded border border-border-strong bg-surface-hover px-2 py-0.5 text-[11px] text-text-secondary">
            {agent.framework}
          </span>
        </div>
        <p className="mt-1 text-xs text-text-muted">
          // gateway: {agent.host}:{agent.port}
        </p>
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

      {tab === "status" && <OverviewTab agent={agent} framework={framework} onConfigChange={() => {
        // Refresh agent data after config change (targeted re-fetch, not full page reload)
        if (onRefresh) onRefresh();
      }} />}
      {tab === "chat" && (
        <ChatPanel alive={agent.alive} framework={agent.framework} agentName={agent.name} />
      )}
      {tab === "logs" && <LogsTab logs={logs} alive={agent.alive} />}
      {tab === "config" && <ConfigTab config={config} error={configError} alive={agent.alive} agentName={agent.name} onConfigSaved={() => {
        // Refresh config after save
        fetchAgentConfig(agent.name)
          .then(setConfig)
          .catch((err) => setConfigError(err.message ?? "Failed to load config"));
      }} />}

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
  onConfigChange
}: {
  agent: AgentInfo;
  framework: Framework | null;
  onConfigChange: () => void;
}) {
  const health = agent.health;
  const status = agent.status;

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
    </div>
  );
}

function LogsTab({ logs, alive }: { logs: LogEntry[]; alive: boolean }) {
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [logs.length]);

  return (
    <div
      ref={scrollRef}
      className="max-h-[calc(100vh-320px)] overflow-y-auto rounded border border-border bg-surface p-4 text-xs"
    >
      {logs.length === 0 ? (
        <p className="text-text-muted">
          {alive ? "Waiting for log entries..." : "No log history available."}
        </p>
      ) : (
        logs.map((entry, i) => (
          <div key={i} className="py-0.5">
            <span className="text-text-muted">
              {new Date(entry.timestamp).toLocaleTimeString()}
            </span>{" "}
            <span
              className={`font-medium ${
                entry.level === "error"
                  ? "text-red"
                  : entry.level === "warn"
                    ? "text-yellow"
                    : "text-green"
              }`}
            >
              [{entry.level}]
            </span>{" "}
            <span className="text-text">{entry.message}</span>
          </div>
        ))
      )}
    </div>
  );
}

function ConfigTab({
  config,
  error,
  alive,
  agentName,
  onConfigSaved,
}: {
  config: AgentConfig | null;
  error: string | null;
  alive: boolean;
  agentName: string;
  onConfigSaved: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [editedContent, setEditedContent] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveSuccess, setSaveSuccess] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  useEffect(() => {
    if (config) {
      setEditedContent(config.content);
    }
  }, [config]);

  const handleSave = async () => {
    try {
      setSaving(true);
      setSaveError(null);

      // Validate
      const validation = await validateAgentConfig(agentName, editedContent);
      if (!validation.valid) {
        setSaveError(validation.error || "Configuration is invalid");
        return;
      }

      // Save
      await updateAgentConfig(agentName, editedContent);

      setSaveSuccess(true);
      setEditing(false);
      setTimeout(() => setSaveSuccess(false), 3000);
      onConfigSaved();
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "Failed to save configuration");
    } finally {
      setSaving(false);
    }
  };

  if (error) {
    return (
      <p className="text-xs text-red">
        Failed to load config: {error}
      </p>
    );
  }

  if (!config) {
    return (
      <p className="text-xs text-text-muted">Loading...</p>
    );
  }

  if (editing) {
    return (
      <div className="space-y-4">
        {saveError && (
          <div className="p-3 bg-red/10 border border-red/20 rounded text-sm text-red">
            {saveError}
          </div>
        )}

        <ConfigEditor
          value={editedContent}
          format={config.format}
          onChange={setEditedContent}
        />

        <div className="flex items-center gap-3">
          <button
            onClick={handleSave}
            disabled={saving}
            className="flex items-center gap-2 px-4 py-2 bg-accent hover:bg-accent-hover
              text-white rounded text-sm font-medium transition-colors disabled:opacity-50"
          >
            {saving ? (
              <>
                <span className="animate-spin">⟳</span>
                saving...
              </>
            ) : (
              <>
                <Save className="w-4 h-4" />
                save changes
              </>
            )}
          </button>
          <button
            onClick={() => {
              setEditing(false);
              setEditedContent(config.content);
              setSaveError(null);
            }}
            disabled={saving}
            className="flex items-center gap-2 px-4 py-2 bg-bg-subtle hover:bg-bg-muted
              border border-border text-fg rounded text-sm font-medium transition-colors"
          >
            <X className="w-4 h-4" />
            cancel
          </button>
        </div>
      </div>
    );
  }

  let displayContent = config.content;
  let isJson = false;

  if (config.format === "json") {
    try {
      displayContent = JSON.stringify(JSON.parse(config.content), null, 2);
      isJson = true;
    } catch { /* fall through to raw */ }
  }

  return (
    <div className="space-y-4">
      {saveSuccess && (
        <div className="p-3 bg-green/10 border border-green/20 rounded text-sm text-green flex items-center gap-2">
          <CheckCircle className="w-4 h-4" />
          configuration saved successfully
        </div>
      )}

      <div className="flex items-center justify-between">
        <p className="text-xs text-text-muted">
          {config.format} configuration{!alive && " (read-only while agent is stopped)"}
        </p>
        {alive && (
          <button
            onClick={() => setEditing(true)}
            className="flex items-center gap-2 px-3 py-1.5 bg-accent hover:bg-accent-hover
              text-white rounded text-xs font-medium transition-colors"
          >
            <Edit3 className="w-3.5 h-3.5" />
            edit config
          </button>
        )}
      </div>

      <pre className="max-h-[calc(100vh-320px)] overflow-auto rounded border border-border bg-surface p-5 text-xs leading-relaxed">
        {isJson ? highlightJson(displayContent) : highlightToml(displayContent)}
      </pre>
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
  onSave,
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
      } else {
        throw new Error(`Unsupported config format: ${config.format}`);
      }

      // Send as raw string so backend writes it directly (no re-encoding)
      await updateAgentConfig(agentName, updated);

      setEditing(false);
      setSaved(true);
      // Don't call onSave() (which reloads the page) — the runtime status
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
              {providerModels.map((m) => (
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

  // Format the replacement value
  const formatted = fieldType === "number" ? newValue : `"${newValue.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;


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

    for (let i = 0; i < lines.length; i++) {
      const trimmed = lines[i].trim();
      if (trimmed === sectionHeader) {
        inSection = true;
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
    if (inSection) {
      let insertAt = lines.length;
      for (let j = lines.indexOf(sectionHeader) + 1; j < lines.length; j++) {
        if (lines[j].trim().startsWith("[")) {
          insertAt = j;
          break;
        }
      }
      lines.splice(insertAt, 0, `${key} = ${formatted}`);
      return lines.join("\n");
    }
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
