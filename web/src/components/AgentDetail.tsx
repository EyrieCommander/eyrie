import { useEffect, useState, useCallback, useRef } from "react";
import { useParams, Link } from "react-router-dom";
import {
  Play,
  Square,
  RotateCcw,
  Plus,
} from "lucide-react";
import type {
  AgentInfo,
  LogEntry,
  ChatMessage,
  ChatPart,
  ChatEvent,
  Session,
} from "../lib/types";
import {
  agentAction,
  fetchAgentConfig,
  type AgentConfig,
  streamLogs,
  fetchSessions,
  fetchChatMessages,
  streamMessage,
  resetSession,
  purgeSession,
  hideSession,
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

const validTabs = ["overview", "chat", "logs", "config"] as const;
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
      {tab === "chat" && (
        <ChatTab alive={agent.alive} framework={agent.framework} agentName={agent.name} />
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

interface ToolCall {
  tool: string;
  toolId?: string;
  args?: Record<string, unknown>;
  output?: string;
  success?: boolean;
  done: boolean;
}

function sessionBaseName(s: Session): string {
  if (s.readonly) {
    const paren = s.title.indexOf(" (");
    return paren > 0 ? s.title.slice(0, paren) : s.title;
  }
  return sessionDisplayName(s.key);
}

interface SessionGroup {
  name: string;
  current?: Session;
  archived: Session[];
}

function groupSessions(sessions: Session[]): SessionGroup[] {
  const map = new Map<string, SessionGroup>();
  for (const s of sessions) {
    const name = sessionBaseName(s);
    let group = map.get(name);
    if (!group) {
      group = { name, archived: [] };
      map.set(name, group);
    }
    if (s.readonly) group.archived.push(s);
    else group.current = s;
  }
  return Array.from(map.values());
}

type FlatItem =
  | { kind: "spacer"; label: string; archiveKey?: string; currentKey?: string }
  | { kind: "message"; msg: ChatMessage; isCurrent: boolean; flatIdx: number };

function ChatTab({
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

  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeGroupName, setActiveGroupName] = useState("");
  const [sessionMsgs, setSessionMsgs] = useState<Map<string, ChatMessage[]>>(new Map());
  const [loading, setLoading] = useState(true);
  const [toggledSet, setToggledSet] = useState<Set<number>>(new Set());

  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [chatError, setChatError] = useState<string | null>(null);
  const [pendingMsgs, setPendingMsgs] = useState<ChatMessage[]>([]);

  const [streamingContent, setStreamingContent] = useState("");
  const [toolCalls, setToolCalls] = useState<ToolCall[]>([]);

  const [creatingSession, setCreatingSession] = useState(false);
  const [newSessionName, setNewSessionName] = useState("");

  const abortRef = useRef<AbortController | null>(null);

  const defaultSessionKey = framework === "zeroclaw" ? "main" : "agent:main:main";
  const groups = groupSessions(sessions);
  const activeGroup = groups.find((g) => g.name === activeGroupName) ?? groups[0];
  const currentSessionKey = activeGroup?.current?.key ?? defaultSessionKey;

  useEffect(() => {
    if (!alive) return;
    fetchSessions(agentName)
      .then((resp) => {
        const all = resp.sessions ?? [];
        setSessions(all);
        const defaultName = sessionDisplayName(defaultSessionKey);
        const gs = groupSessions(all);
        const match = gs.find((g) => g.name === defaultName);
        setActiveGroupName(match?.name ?? gs[0]?.name ?? defaultName);
      })
      .catch(() => {
        setActiveGroupName(sessionDisplayName(defaultSessionKey));
      });
  }, [agentName, alive, defaultSessionKey]);

  const loadGroup = useCallback(
    (group: SessionGroup | undefined) => {
      if (!group) return;
      setLoading(true);
      setSessionMsgs(new Map());
      setToggledSet(new Set());
      setPendingMsgs([]);
      setStreamingContent("");
      setToolCalls([]);

      const keys = [
        ...group.archived.map((s) => s.key),
        ...(group.current ? [group.current.key] : []),
      ];
      if (keys.length === 0) {
        setSessionMsgs(new Map());
        setLoading(false);
        return;
      }

      Promise.all(
        keys.map((k) =>
          fetchChatMessages(agentName, k, 100)
            .then((msgs) => [k, msgs] as const)
            .catch(() => [k, [] as ChatMessage[]] as const)
        )
      ).then((results) => {
        const m = new Map<string, ChatMessage[]>();
        for (const [k, msgs] of results) m.set(k, msgs);
        setSessionMsgs(m);
        setLoading(false);
      });
    },
    [agentName],
  );

  const refreshCurrentSession = useCallback(
    (key: string) => {
      if (!key) return;
      fetchChatMessages(agentName, key, 100)
        .then((msgs) => {
          setSessionMsgs((prev) => {
            const next = new Map(prev);
            next.set(key, msgs);
            return next;
          });
          setPendingMsgs([]);
        })
        .catch(() => {});
    },
    [agentName],
  );

  useEffect(() => {
    const group = groups.find((g) => g.name === activeGroupName);
    if (group && alive) loadGroup(group);
  }, [activeGroupName, alive, loadGroup, sessions]); // eslint-disable-line react-hooks/exhaustive-deps

  const isNoReply = (content: string) =>
    /^(\[\[no_reply\]\]|NO_REPLY)$/i.test(content.trim());

  const flatItems: FlatItem[] = [];
  if (activeGroup) {
    let flatIdx = 0;
    const sortedArchived = [...activeGroup.archived].sort((a, b) => {
      const ta = a.last_message ? new Date(a.last_message).getTime() : 0;
      const tb = b.last_message ? new Date(b.last_message).getTime() : 0;
      return ta - tb;
    });

    for (const arch of sortedArchived) {
      flatItems.push({ kind: "spacer", label: arch.title, archiveKey: arch.key });
      const msgs = sessionMsgs.get(arch.key) ?? [];
      for (const msg of msgs) {
        if (msg.role === "assistant" && isNoReply(msg.content)) continue;
        flatItems.push({ kind: "message", msg, isCurrent: false, flatIdx });
        flatIdx++;
      }
    }

    if (activeGroup.current) {
      if (sortedArchived.length > 0 || framework !== "zeroclaw") {
        flatItems.push({
          kind: "spacer",
          label: "current session",
          currentKey: framework !== "zeroclaw" ? activeGroup.current.key : undefined,
        });
      }
      const msgs = sessionMsgs.get(activeGroup.current.key) ?? [];
      for (const msg of msgs) {
        if (msg.role === "assistant" && isNoReply(msg.content)) continue;
        flatItems.push({ kind: "message", msg, isCurrent: true, flatIdx });
        flatIdx++;
      }
    }

    for (const msg of pendingMsgs) {
      flatItems.push({ kind: "message", msg, isCurrent: true, flatIdx });
      flatIdx++;
    }
  }

  const totalMsgCount = flatItems.filter((it) => it.kind === "message").length;

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [totalMsgCount, sending, streamingContent, toolCalls]);

  const handleSend = useCallback(() => {
    const text = input.trim();
    if (!text || sending) return;

    setInput("");
    setChatError(null);
    setSending(true);
    setStreamingContent("");
    setToolCalls([]);

    const userMsg: ChatMessage = {
      role: "user",
      content: text,
      timestamp: new Date().toISOString(),
    };
    setPendingMsgs((prev) => [...prev, userMsg]);

    const controller = streamMessage(agentName, text, currentSessionKey, (ev: ChatEvent) => {
      switch (ev.type) {
        case "delta":
          setStreamingContent((prev) => prev + (ev.content ?? ""));
          break;
        case "tool_start":
          setToolCalls((prev) => [
            ...prev,
            {
              tool: ev.tool ?? "unknown",
              toolId: ev.tool_id,
              args: ev.args,
              done: false,
            },
          ]);
          break;
        case "tool_result":
          setToolCalls((prev) => {
            const updated = [...prev];
            let idx = -1;
            for (let i = updated.length - 1; i >= 0; i--) {
              if (updated[i].tool === ev.tool && !updated[i].done) {
                idx = i;
                break;
              }
            }
            if (idx >= 0) {
              updated[idx] = { ...updated[idx], output: ev.output, success: ev.success, done: true };
            }
            return updated;
          });
          break;
        case "done": {
          const raw = ev.content ?? "";
          const skip = /^(\[\[no_reply\]\]|NO_REPLY)$/i.test(raw.trim());
          if (!skip) {
            const reply: ChatMessage = {
              role: "assistant",
              content: raw,
              timestamp: new Date().toISOString(),
            };
      setPendingMsgs((prev) => [...prev, reply]);
          }
          setStreamingContent("");
          setToolCalls([]);
      setSending(false);
      inputRef.current?.focus();
          setTimeout(() => refreshCurrentSession(currentSessionKey), 500);
          break;
        }
        case "error":
          setChatError(ev.error ?? "Unknown error");
          setStreamingContent("");
          setToolCalls([]);
          setSending(false);
          inputRef.current?.focus();
          break;
      }
    });
    abortRef.current = controller;
  }, [input, sending, agentName, currentSessionKey, refreshCurrentSession]);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
    };
  }, []);

  const refreshSessions = useCallback(() => {
    fetchSessions(agentName)
      .then((resp) => setSessions(resp.sessions ?? []))
      .catch(() => {});
  }, [agentName]);

  const handleResetSession = useCallback(
    async (key: string) => {
      const name = sessionDisplayName(key);
      if (!window.confirm(`Reset session "${name}"? The transcript will be archived.`)) return;
      try {
        await resetSession(agentName, key);
        refreshSessions();
      } catch (e) {
        console.error(e);
      }
    },
    [agentName, refreshSessions],
  );

  const handlePurgeSession = useCallback(
    async (archiveKey: string) => {
      if (!window.confirm("Permanently delete this archived session? This cannot be undone.")) return;
      try {
        await purgeSession(agentName, archiveKey);
        refreshSessions();
      } catch (e) {
        console.error(e);
      }
    },
    [agentName, refreshSessions],
  );

  const handleHideSession = useCallback(
    async (archiveKey: string) => {
      try {
        await hideSession(agentName, archiveKey);
        refreshSessions();
      } catch (e) {
        console.error(e);
      }
    },
    [agentName, refreshSessions],
  );

  const handleCreateSession = () => {
    const name = newSessionName.trim().toLowerCase().replace(/\s+/g, "-");
    if (!name) return;
    const key = framework === "zeroclaw" ? name : `agent:main:${name}`;
    setSessions((prev) => [...prev, { key, title: name, channel: "" }]);
    setActiveGroupName(name);
    setCreatingSession(false);
    setNewSessionName("");
  };

  if (!alive) {
    return (
      <p className="text-xs text-text-muted">
        Agent is not running. Start it to chat.
      </p>
    );
  }

  const longMsgItems = flatItems.filter(
    (it): it is Extract<FlatItem, { kind: "message" }> =>
      it.kind === "message" && it.msg.content.length > 200,
  );

  return (
    <div className="flex flex-col" style={{ height: "calc(100vh - 320px)" }}>
      {/* Session group bar */}
      {(groups.length > 1 || framework !== "zeroclaw") && (
      <div className="flex items-center gap-1 overflow-x-auto rounded-t border border-b-0 border-border bg-bg-sidebar px-3 py-2">
          {groups.map((g) => (
            <button
              key={g.name}
              onClick={() => setActiveGroupName(g.name)}
              className={`shrink-0 rounded px-3 py-1 text-[11px] font-medium transition-colors ${
                activeGroupName === g.name
                  ? "bg-surface-hover text-accent"
                  : "text-text-secondary hover:text-text hover:bg-surface-hover/50"
              }`}
            >
              {g.name}
              {g.archived.length > 0 && (
                <span className="ml-1 text-[9px] text-text-muted">
                  +{g.archived.length}
                </span>
              )}
            </button>
          ))}

        {framework !== "zeroclaw" && (
          creatingSession ? (
            <div className="flex items-center gap-1 ml-1">
              <input
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
      )}

      {/* Messages */}
      <div
        ref={scrollRef}
        className={`flex-1 overflow-y-auto border-x border-border bg-surface text-xs ${groups.length <= 1 && framework === "zeroclaw" ? "rounded-t border-t" : ""}`}
      >
        {longMsgItems.length > 0 && (
          <div className="sticky top-0 z-10 float-right flex gap-0.5 pr-2 pt-2">
            <button
              onClick={() => {
                setToggledSet(() => {
                  const next = new Set<number>();
                  for (const it of longMsgItems) {
                    if (!it.isCurrent) next.add(it.flatIdx);
                  }
                  return next;
                });
              }}
              className="text-green font-bold text-sm leading-none px-1 rounded hover:bg-surface-hover transition-colors"
              title="Expand all"
            >
              +
            </button>
            <button
              onClick={() => {
                setToggledSet(() => {
                  const next = new Set<number>();
                  for (const it of longMsgItems) {
                    if (it.isCurrent) next.add(it.flatIdx);
                  }
                  return next;
                });
              }}
              className="text-purple font-bold text-sm leading-none px-1 rounded hover:bg-surface-hover transition-colors"
              title="Compact all"
            >
              −
            </button>
          </div>
        )}

        <div className="px-4 pb-4 pt-2">
        {loading ? (
          <p className="text-text-muted animate-pulse">Loading messages...</p>
        ) : flatItems.length === 0 && !sending ? (
          <p className="text-text-muted">
            No messages yet. Type below to start a conversation.
          </p>
        ) : (
          flatItems.map((item, i) => {
            if (item.kind === "spacer") {
              return (
                <div
                  key={`spacer-${i}`}
                  className="group/spacer my-3 flex items-center gap-3"
                >
                  <div className="flex-1 border-t border-green/40" />
                  <span className="text-[10px] font-medium text-green">
                    {item.label}
                  </span>
                  {item.archiveKey && (
                    <span className="hidden group-hover/spacer:inline-flex items-center gap-1">
                      <button
                        onClick={() => handlePurgeSession(item.archiveKey!)}
                        className="rounded px-1 py-0.5 text-[9px] text-text-muted hover:text-red hover:bg-red/10 transition-colors"
                        title="Delete permanently"
                      >
                        delete
                      </button>
                      <button
                        onClick={() => handleHideSession(item.archiveKey!)}
                        className="rounded px-1 py-0.5 text-[9px] text-text-muted hover:text-purple hover:bg-purple/10 transition-colors"
                        title="Hide from view"
                      >
                        hide
                      </button>
                    </span>
                  )}
                  {item.currentKey && (
                    <span className="hidden group-hover/spacer:inline-flex items-center gap-1">
                      <button
                        onClick={() => handleResetSession(item.currentKey!)}
                        className="rounded px-1 py-0.5 text-[9px] text-text-muted hover:text-red hover:bg-red/10 transition-colors"
                        title="Reset session (archive transcript)"
                      >
                        reset
                      </button>
                    </span>
                  )}
                  <div className="flex-1 border-t border-green/40" />
                </div>
              );
            }
            const { msg, isCurrent, flatIdx } = item;
            const expanded = isCurrent
              ? !toggledSet.has(flatIdx)
              : toggledSet.has(flatIdx);
            return (
            <MessageRow
                key={`${msg.timestamp}-${flatIdx}`}
              msg={msg}
                expanded={expanded}
                onToggle={() => {
                  setToggledSet((prev) => {
                    const next = new Set(prev);
                    if (next.has(flatIdx)) next.delete(flatIdx);
                    else next.add(flatIdx);
                    return next;
                  });
                }}
              />
            );
          })
        )}

        {sending && (
          <div className="py-1">
            {toolCalls.map((tc, i) => (
              <ToolCallCard key={`tc-${i}`} tc={tc} />
            ))}
            {streamingContent ? (
              <div className="py-1">
                <span className="text-purple font-medium">assistant:</span>{" "}
                <span className="text-text whitespace-pre-wrap">{streamingContent}</span>
                <span className="inline-block w-1.5 h-3 bg-accent/60 animate-pulse ml-0.5 align-text-bottom" />
              </div>
            ) : toolCalls.length === 0 ? (
          <div className="py-1 text-text-muted animate-pulse">
            <span className="text-purple font-medium">assistant:</span>{" "}
            thinking...
              </div>
            ) : null}
          </div>
        )}
        </div>
      </div>

      {chatError && (
        <div className="border-x border-border bg-red/5 px-4 py-2 text-[10px] text-red">
          {chatError}
        </div>
      )}

      {/* Chat input */}
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
  onToggle?: () => void;
}) {
  const parts = msg.parts ?? [];
  const toolCount = parts.filter((p) => p.type === "tool_call").length;
  const hasParts = parts.length > 0;
  const isLong = msg.content.length > 200 || toolCount > 0;
  const canToggle = isLong && onToggle;
  const displayText = isLong && !expanded
    ? (msg.content.length > 200 ? msg.content.slice(0, 200) + "..." : msg.content)
    : msg.content;
  const toolSummary = !expanded && toolCount > 0
    ? ` [${toolCount} tool${toolCount > 1 ? "s" : ""}]`
    : "";

  return (
    <div
      className={`py-1 ${canToggle ? "cursor-pointer hover:bg-surface-hover/50 rounded px-1 -mx-1" : ""}`}
      onClick={canToggle ? () => {
        if (!window.getSelection()?.toString()) onToggle!();
      } : undefined}
    >
      <span className="text-text-muted">
        {new Date(msg.timestamp).toLocaleTimeString()}
      </span>{" "}
      <span
        className={`font-medium ${msg.role === "user" ? "text-green" : "text-purple"}`}
      >
        {msg.role}:
      </span>{" "}
      {!expanded && (
        <>
          <span className="text-text">{displayText}</span>
          {toolSummary && (
            <span className="ml-1 text-accent/60 text-[10px]">{toolSummary}</span>
          )}
        </>
      )}
      {canToggle && !expanded && (
        <span className="ml-1 text-green">▸</span>
      )}
      {canToggle && expanded && (
        <span className="ml-1 text-green">▾</span>
      )}
      {expanded && hasParts && (
        <div className="mt-0.5" onClick={(e) => e.stopPropagation()}>
          {groupPartsIntoRuns(parts).map((run, ri) =>
            run.type === "text" ? (
              <div key={`text-${ri}`} className="text-text whitespace-pre-wrap py-0.5">
                {run.text}
              </div>
            ) : (
              <ToolRunCard key={`run-${ri}`} tools={run.tools} />
            ),
          )}
        </div>
      )}
      {expanded && !hasParts && msg.content && (
        <span className="text-text whitespace-pre-wrap">{msg.content}</span>
      )}
    </div>
  );
}

type PartRun =
  | { type: "text"; text: string }
  | { type: "tools"; tools: ChatPart[] };

function groupPartsIntoRuns(parts: ChatPart[]): PartRun[] {
  const runs: PartRun[] = [];
  for (const p of parts) {
    if (p.type === "text") {
      runs.push({ type: "text", text: p.text ?? "" });
    } else {
      const last = runs[runs.length - 1];
      if (last && last.type === "tools") {
        last.tools.push(p);
      } else {
        runs.push({ type: "tools", tools: [p] });
      }
    }
  }
  return runs;
}

function ToolRunCard({ tools }: { tools: ChatPart[] }) {
  const [expanded, setExpanded] = useState(false);
  const failCount = tools.filter((t) => t.error).length;
  const names = tools.map((t) => t.name).filter(Boolean);
  const uniqueNames = [...new Set(names)];
  const summary =
    tools.length === 1
      ? tools[0].name ?? "tool"
      : `${tools.length} tools` +
        (uniqueNames.length <= 3 ? `: ${uniqueNames.join(", ")}` : "");

  return (
    <div className="my-1.5 ml-4 rounded border border-border bg-surface-hover/30 text-[11px]">
      <button
        onClick={(e) => {
          e.stopPropagation();
          setExpanded(!expanded);
        }}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left"
      >
        <span className="font-mono text-text">{summary}</span>
        <span className="ml-auto flex items-center gap-1.5">
          {failCount > 0 ? (
            <span className="text-red text-[10px]">{failCount} FAIL</span>
          ) : (
            <span className="text-green text-[10px]">OK</span>
          )}
          <span className="text-text-muted text-[10px]">
            {expanded ? "▾" : "▸"}
      </span>
        </span>
      </button>
      {expanded && (
        <div className="border-t border-border">
          {tools.map((part, i) => (
            <PartToolCallCard key={part.id || `tc-${i}`} part={part} defaultExpanded />
          ))}
        </div>
      )}
    </div>
  );
}

function PartToolCallCard({ part, defaultExpanded = false }: { part: ChatPart; defaultExpanded?: boolean }) {
  const [expanded, setExpanded] = useState(defaultExpanded);

  return (
    <div className="border-b border-border/30 last:border-b-0 text-[11px]">
      <button
        onClick={(e) => {
          e.stopPropagation();
          setExpanded(!expanded);
        }}
        className="flex w-full items-center gap-2 px-3 py-1 text-left hover:bg-surface-hover/30"
      >
        <span className="font-mono text-text-secondary">{part.name}</span>
        <span className="ml-auto flex items-center gap-1.5">
          {part.error ? (
            <span className="text-red text-[10px]">FAIL</span>
          ) : part.output != null ? (
            <span className="text-green text-[10px]">OK</span>
          ) : null}
          <span className="text-text-muted text-[10px]">
            {expanded ? "▾" : "▸"}
          </span>
        </span>
      </button>
      {expanded && (
        <div className="border-t border-border/30 px-3 py-2 space-y-1.5 bg-surface/50">
          {part.args && Object.keys(part.args).length > 0 && (
            <div>
              <span className="text-text-muted">args: </span>
              <pre className="mt-0.5 overflow-x-auto whitespace-pre-wrap text-[10px] text-text-secondary">
                {JSON.stringify(part.args, null, 2)}
              </pre>
            </div>
          )}
          {part.output != null && (
            <div>
              <span className="text-text-muted">output: </span>
              <pre className="mt-0.5 max-h-32 overflow-y-auto overflow-x-auto whitespace-pre-wrap text-[10px] text-text-secondary">
                {part.output}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ToolCallCard({ tc }: { tc: ToolCall }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div
      className="my-1.5 ml-4 rounded border border-border bg-surface-hover/30 text-[11px]"
    >
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left"
      >
        <span className="font-mono text-text">{tc.tool}</span>
        <span className="ml-auto flex items-center gap-1.5">
          {!tc.done && (
            <span className="h-1.5 w-1.5 rounded-full bg-accent animate-pulse" />
          )}
          {tc.done && tc.success !== false && (
            <span className="text-green text-[10px]">OK</span>
          )}
          {tc.done && tc.success === false && (
            <span className="text-red text-[10px]">FAIL</span>
          )}
          <span className="text-text-muted text-[10px]">
            {expanded ? "▾" : "▸"}
          </span>
        </span>
      </button>
      {expanded && (
        <div className="border-t border-border/50 px-3 py-2 space-y-1.5">
          {tc.args && Object.keys(tc.args).length > 0 && (
            <div>
              <span className="text-text-muted">args: </span>
              <pre className="mt-0.5 overflow-x-auto whitespace-pre-wrap text-[10px] text-text-secondary">
                {JSON.stringify(tc.args, null, 2)}
              </pre>
            </div>
          )}
          {tc.output != null && (
            <div>
              <span className="text-text-muted">output: </span>
              <pre className="mt-0.5 max-h-32 overflow-y-auto overflow-x-auto whitespace-pre-wrap text-[10px] text-text-secondary">
                {tc.output}
              </pre>
            </div>
          )}
        </div>
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
