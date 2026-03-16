import { useEffect, useState, useCallback, useRef } from "react";
import { useParams, Link } from "react-router-dom";
import {
  Play,
  Square,
  RotateCcw,
  Plus,
  X,
} from "lucide-react";
import type {
  AgentInfo,
  LogEntry,
  ChatMessage,
  Session,
} from "../lib/types";
import {
  agentAction,
  fetchAgentConfig,
  type AgentConfig,
  streamLogs,
  fetchSessions,
  fetchChatMessages,
  sendMessage,
  deleteSession,
} from "../lib/api";

function formatBytes(bytes: number): string {
  if (!bytes) return "-";
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)}KB`;
  if (bytes < 1024 * 1024 * 1024)
    return `${(bytes / (1024 * 1024)).toFixed(0)}MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)}GB`;
}

interface AgentDetailProps {
  agent: AgentInfo;
}

const validTabs = ["overview", "activity", "logs", "config"] as const;
type Tab = (typeof validTabs)[number];

export default function AgentDetail({ agent }: AgentDetailProps) {
  const { tab: tabParam } = useParams<{ tab?: string }>();

  const tab: Tab = validTabs.includes(tabParam as Tab)
    ? (tabParam as Tab)
    : "overview";

  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [config, setConfig] = useState<AgentConfig | null>(null);
  const [configError, setConfigError] = useState<string | null>(null);
  const [actionPending, setActionPending] = useState(false);

  const handleAction = useCallback(
    async (action: "start" | "stop" | "restart") => {
      setActionPending(true);
      try {
        await agentAction(agent.name, action);
      } catch (e) {
        console.error(e);
      } finally {
        setActionPending(false);
      }
    },
    [agent.name],
  );

  useEffect(() => {
    if (tab === "logs" && agent.alive) {
      setLogs([]);
      const close = streamLogs(agent.name, (entry) => {
        setLogs((prev) => [...prev.slice(-200), entry]);
      });
      return close;
    }
  }, [tab, agent.name, agent.alive]);

  useEffect(() => {
    if (tab === "config" && agent.alive) {
      setConfig(null);
      setConfigError(null);
      fetchAgentConfig(agent.name)
        .then(setConfig)
        .catch((err) => setConfigError(err.message ?? "Failed to load config"));
    }
  }, [tab, agent.name, agent.alive]);

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
              label="start"
              onClick={() => handleAction("start")}
              disabled={actionPending}
            />
          ) : (
            <>
              <ActionButton
                icon={<RotateCcw className="h-3.5 w-3.5" />}
                label="restart"
                onClick={() => handleAction("restart")}
                disabled={actionPending}
              />
              <ActionButton
                icon={<Square className="h-3.5 w-3.5" />}
                label="stop"
                onClick={() => handleAction("stop")}
                disabled={actionPending}
                variant="danger"
              />
            </>
          )}
        </div>
      </div>

      <div>
        <div className="flex items-center gap-3">
          <span
            className={`h-3 w-3 rounded-full ${agent.alive ? "bg-green" : "bg-red"}`}
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

      {tab === "overview" && <OverviewTab agent={agent} />}
      {tab === "activity" && (
        <ActivityTab alive={agent.alive} framework={agent.framework} agentName={agent.name} />
      )}
      {tab === "logs" && <LogsTab logs={logs} alive={agent.alive} />}
      {tab === "config" && <ConfigTab config={config} error={configError} alive={agent.alive} />}
    </div>
  );
}

function OverviewTab({ agent }: { agent: AgentInfo }) {
  const health = agent.health;
  const status = agent.status;

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
        <InfoCard label="PROVIDER" value={status?.provider ?? "-"} />
        <InfoCard label="MODEL" value={status?.model ?? "-"} />
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

  if (!alive) {
    return (
      <p className="text-xs text-text-muted">
        Agent is not running. Start it to see logs.
      </p>
    );
  }

  return (
    <div
      ref={scrollRef}
      className="max-h-[calc(100vh-320px)] overflow-y-auto rounded border border-border bg-surface p-4 text-xs"
    >
      {logs.length === 0 ? (
        <p className="text-text-muted">Waiting for log entries...</p>
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
}: {
  config: AgentConfig | null;
  error: string | null;
  alive: boolean;
}) {
  if (!alive) {
    return (
      <p className="text-xs text-text-muted">
        Agent is not running. Start it to view config.
      </p>
    );
  }

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

  if (config.format === "json") {
    try {
      const pretty = JSON.stringify(JSON.parse(config.content), null, 2);
      return (
        <pre className="max-h-[calc(100vh-320px)] overflow-auto rounded border border-border bg-surface p-5 text-xs leading-relaxed">
          {highlightJson(pretty)}
        </pre>
      );
    } catch { /* fall through to raw */ }
  }

  return (
    <pre className="max-h-[calc(100vh-320px)] overflow-auto rounded border border-border bg-surface p-5 text-xs leading-relaxed">
      {highlightToml(config.content)}
    </pre>
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

function sessionDisplayName(key: string): string {
  if (!key) return "main";
  const parts = key.split(":");
  return parts[parts.length - 1] || key;
}

function ActivityTab({
  alive,
  framework,
  agentName,
}: {
  alive: boolean;
  framework: string;
  agentName: string;
}) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const newSessionRef = useRef<HTMLInputElement>(null);

  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeKey, setActiveKey] = useState("");
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [expandedIdx, setExpandedIdx] = useState<number | null>(null);

  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [chatError, setChatError] = useState<string | null>(null);
  const [pendingMsgs, setPendingMsgs] = useState<ChatMessage[]>([]);

  const [creatingSession, setCreatingSession] = useState(false);
  const [newSessionName, setNewSessionName] = useState("");

  const defaultSessionKey = framework === "zeroclaw" ? "main" : "agent:main:main";
  const activeSessionObj = sessions.find((s) => s.key === activeKey);
  const isReadOnly = activeSessionObj?.readonly === true;

  useEffect(() => {
    if (!alive) return;
    fetchSessions(agentName)
      .then((resp) => {
        setSessions(resp.sessions ?? []);
        const existing = resp.sessions?.find((s) => s.key === defaultSessionKey);
        setActiveKey(existing?.key ?? resp.sessions?.[0]?.key ?? defaultSessionKey);
      })
      .catch(() => {
        setActiveKey(defaultSessionKey);
      });
  }, [agentName, alive, defaultSessionKey]);

  const loadMessages = useCallback(
    (key: string) => {
      if (!key) return;
      setLoading(true);
      setExpandedIdx(null);
      setPendingMsgs([]);
      fetchChatMessages(agentName, key, 100)
        .then(setMessages)
        .catch(() => setMessages([]))
        .finally(() => setLoading(false));
    },
    [agentName],
  );

  useEffect(() => {
    if (activeKey && alive) loadMessages(activeKey);
  }, [activeKey, alive, loadMessages]);

  const allMessages = [...messages, ...pendingMsgs];

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [allMessages.length, sending]);

  const handleSend = useCallback(async () => {
    const text = input.trim();
    if (!text || sending) return;

    setInput("");
    setChatError(null);
    setSending(true);

    const userMsg: ChatMessage = {
      role: "user",
      content: text,
      timestamp: new Date().toISOString(),
    };
    setPendingMsgs((prev) => [...prev, userMsg]);

    try {
      const reply = await sendMessage(agentName, text, activeKey);
      setPendingMsgs((prev) => [...prev, reply]);
      // Re-fetch to get the authoritative transcript after a short delay
      setTimeout(() => loadMessages(activeKey), 500);
    } catch (e) {
      setChatError(e instanceof Error ? e.message : "Failed to send message");
    } finally {
      setSending(false);
      inputRef.current?.focus();
    }
  }, [input, sending, agentName, activeKey, loadMessages]);

  const handleCreateSession = () => {
    const name = newSessionName.trim().toLowerCase().replace(/\s+/g, "-");
    if (!name) return;
    const key =
      framework === "zeroclaw" ? name : `agent:main:${name}`;
    setSessions((prev) => [
      ...prev,
      { key, title: name, channel: "" },
    ]);
    setActiveKey(key);
    setCreatingSession(false);
    setNewSessionName("");
  };

  const handleDeleteSession = useCallback(
    async (key: string) => {
      if (!confirm(`Delete session "${sessionDisplayName(key)}"? The transcript will be archived.`)) return;
      try {
        await deleteSession(agentName, key);
        setSessions((prev) => prev.filter((s) => s.key !== key));
        if (activeKey === key) {
          const remaining = sessions.filter((s) => s.key !== key);
          setActiveKey(remaining[0]?.key ?? defaultSessionKey);
        }
      } catch (e) {
        setChatError(e instanceof Error ? e.message : "Failed to delete session");
      }
    },
    [agentName, activeKey, sessions, defaultSessionKey],
  );

  if (!alive) {
    return (
      <p className="text-xs text-text-muted">
        Agent is not running. Start it to see activity.
      </p>
    );
  }

  return (
    <div className="flex flex-col" style={{ height: "calc(100vh - 320px)" }}>
      {/* Session bar */}
      <div className="flex items-center gap-1 overflow-x-auto rounded-t border border-b-0 border-border bg-bg-sidebar px-3 py-2">
        {sessions.map((s) => (
          <div key={s.key} className="group relative shrink-0 flex items-center">
            <button
              onClick={() => setActiveKey(s.key)}
              className={`rounded px-3 py-1 text-[11px] font-medium transition-colors ${
                activeKey === s.key
                  ? s.readonly
                    ? "bg-surface-hover text-text-muted"
                    : "bg-surface-hover text-accent"
                  : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
              } ${s.readonly ? "italic" : ""}`}
            >
              {s.readonly ? s.title : sessionDisplayName(s.key)}
            </button>
            {s.key !== defaultSessionKey && framework !== "zeroclaw" && !s.readonly && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  handleDeleteSession(s.key);
                }}
                className="absolute -top-1 -right-1 hidden group-hover:flex h-3.5 w-3.5 items-center justify-center rounded-full bg-surface-hover text-text-muted hover:text-red hover:bg-red/10 transition-colors"
                title="Delete session"
              >
                <X className="h-2.5 w-2.5" />
              </button>
            )}
          </div>
        ))}

        {!sessions.some((s) => s.key === activeKey) && activeKey && (
          <button
            className="shrink-0 rounded bg-surface-hover px-3 py-1 text-[11px] font-medium text-accent"
          >
            {sessionDisplayName(activeKey)}
          </button>
        )}

        {framework === "zeroclaw" && sessions.length <= 1 && (
          <span className="shrink-0 text-[10px] text-text-muted ml-2">
            single session (zeroclaw)
          </span>
        )}

        {framework !== "zeroclaw" && (
          creatingSession ? (
            <div className="flex items-center gap-1 ml-1">
              <input
                ref={newSessionRef}
                type="text"
                value={newSessionName}
                onChange={(e) => setNewSessionName(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") handleCreateSession();
                  if (e.key === "Escape") {
                    setCreatingSession(false);
                    setNewSessionName("");
                  }
                }}
                placeholder="session name"
                className="w-24 rounded border border-border bg-surface px-2 py-0.5 text-[11px] text-text placeholder:text-text-muted focus:outline-none focus:border-accent"
                autoFocus
              />
              <button
                onClick={handleCreateSession}
                disabled={!newSessionName.trim()}
                className="rounded px-1.5 py-0.5 text-[11px] text-accent hover:bg-surface-hover disabled:opacity-30"
              >
                ok
              </button>
            </div>
          ) : (
            <button
              onClick={() => setCreatingSession(true)}
              className="shrink-0 rounded p-1 text-text-muted transition-colors hover:text-accent hover:bg-surface-hover/50"
              title="New session"
            >
              <Plus className="h-3.5 w-3.5" />
            </button>
          )
        )}
      </div>

      {/* Messages */}
      <div
        ref={scrollRef}
        className="flex-1 overflow-y-auto border-x border-border bg-surface p-4 text-xs"
      >
        {loading ? (
          <p className="text-text-muted animate-pulse">Loading messages...</p>
        ) : allMessages.length === 0 && !sending ? (
          <p className="text-text-muted">
            No messages yet. Type below to start a conversation.
          </p>
        ) : (
          allMessages.map((msg, i) => (
            <MessageRow
              key={`${msg.timestamp}-${i}`}
              msg={msg}
              expanded={expandedIdx === i}
              onToggle={() => setExpandedIdx(expandedIdx === i ? null : i)}
            />
          ))
        )}

        {sending && (
          <div className="py-1 text-text-muted animate-pulse">
            <span className="text-purple font-medium">assistant:</span>{" "}
            thinking...
          </div>
        )}
      </div>

      {chatError && (
        <div className="border-x border-border bg-red/5 px-4 py-2 text-[10px] text-red">
          {chatError}
        </div>
      )}

      {/* Chat input */}
      {isReadOnly ? (
        <div className="flex items-center justify-center rounded-b border border-border bg-surface-hover/50 px-4 py-2.5 text-[10px] text-text-muted italic">
          archived session (read-only)
        </div>
      ) : (
        <div className="flex items-center gap-2 rounded-b border border-border bg-surface-hover p-3">
          <span className="text-accent text-xs">&gt;</span>
          <input
            ref={inputRef}
            type="text"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            placeholder="Type a message..."
            disabled={sending}
            className="flex-1 bg-transparent text-xs text-text placeholder:text-text-muted focus:outline-none disabled:opacity-50"
          />
          <button
            onClick={handleSend}
            disabled={sending || !input.trim()}
            className="rounded border border-border px-3 py-1 text-[10px] font-medium text-text-secondary transition-colors hover:bg-surface hover:text-text disabled:opacity-30"
          >
            send
          </button>
        </div>
      )}
    </div>
  );
}

function MessageRow({
  msg,
  expanded,
  onToggle,
}: {
  msg: ChatMessage;
  expanded: boolean;
  onToggle: () => void;
}) {
  const isLong = msg.content.length > 200;
  const displayText = isLong && !expanded
    ? msg.content.slice(0, 200) + "..."
    : msg.content;

  return (
    <div
      className={`py-1 ${isLong ? "cursor-pointer hover:bg-surface-hover/50 rounded px-1 -mx-1" : ""}`}
      onClick={isLong ? onToggle : undefined}
    >
      <span className="text-text-muted">
        {new Date(msg.timestamp).toLocaleTimeString()}
      </span>{" "}
      <span
        className={`font-medium ${msg.role === "user" ? "text-green" : "text-purple"}`}
      >
        {msg.role}:
      </span>{" "}
      <span className={`text-text ${expanded ? "whitespace-pre-wrap" : ""}`}>
        {displayText}
      </span>
      {isLong && !expanded && (
        <span className="ml-1 text-text-muted">▸</span>
      )}
      {isLong && expanded && (
        <span className="ml-1 text-text-muted">▾</span>
      )}
    </div>
  );
}

function InfoCard({
  label,
  value,
  highlight,
}: {
  label: string;
  value: string;
  highlight?: boolean;
}) {
  return (
    <div className="rounded border border-border bg-surface p-4">
      <p className="text-[10px] font-medium uppercase tracking-wider text-text-muted">
        {label}
      </p>
      <p
        className={`mt-1.5 text-lg font-semibold ${highlight ? "text-accent" : "text-text"}`}
      >
        {value}
      </p>
    </div>
  );
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
