import {
  useEffect,
  useState,
  useCallback,
  useRef,
  useMemo,
  type ReactNode,
  type KeyboardEvent,
} from "react";
import { useSearchParams } from "react-router-dom";
import type { ChatMessage, ChatEvent, Session } from "../lib/types";
import {
  fetchSessions,
  fetchChatMessages,
  streamMessage,
  createSession,
  resetSession,
  deleteSession,
  destroySession,
  hideSession,
} from "../lib/api";

import { type ToolCall, matchToolResult } from "../lib/chat-events";
import { recordLatency, recordUsage } from "../lib/useAgentMetrics";
export type { ToolCall };

import {
  ChatError,
  MessageRow,
  SessionBar,
  type SessionGroup,
} from "./chat";
import { StreamingIndicator } from "./chat/StreamingIndicator";
import type { StreamingPart } from "./chat/StreamingIndicator";
import { useAutoScroll } from "../lib/useAutoScroll";

// ── Types ───────────────────────────────────────────────────────────────

type FlatItem =
  | { kind: "spacer"; label: string; archiveKey?: string; currentKey?: string }
  | { kind: "message"; msg: ChatMessage; isCurrent: boolean; flatIdx: number };

// ── Session helpers ─────────────────────────────────────────────────────

function sessionDisplayName(key: string): string {
  if (!key) return "main";
  const parts = key.split(":");
  return parts[parts.length - 1] || key;
}

function sessionBaseName(s: Session): string {
  if (s.readonly) {
    const paren = s.title.indexOf(" (");
    return paren > 0 ? s.title.slice(0, paren) : s.title;
  }
  if (s.key.includes(":")) {
    return sessionDisplayName(s.key);
  }
  return s.title || s.key;
}

function groupLastActivity(group: SessionGroup): number {
  let latest = 0;
  if (group.current?.last_message) {
    latest = Math.max(latest, new Date(group.current.last_message).getTime());
  }
  for (const a of group.archived) {
    if (a.last_message) {
      latest = Math.max(latest, new Date(a.last_message).getTime());
    }
  }
  return latest;
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
  return Array.from(map.values()).sort(
    (a, b) => groupLastActivity(b) - groupLastActivity(a),
  );
}

// ── ChatPanel ───────────────────────────────────────────────────────────

export interface ChatPanelProps {
  alive: boolean;
  framework: string;
  agentName: string;
  /** Extra content rendered inside the input row (e.g. mention popup) */
  inputAddon?: ReactNode;
  /** Extra keydown handler for the input (e.g. mention keyboard nav).
   *  Return true to suppress default Enter-to-send. */
  onInputKeyDown?: (e: KeyboardEvent<HTMLInputElement>) => boolean | void;
  /** Override placeholder text */
  placeholder?: string;
  /** Extra disabled condition beyond !alive */
  disabled?: boolean;
  /** Height offset for container (default 240) */
  heightOffset?: number;
}

export function ChatPanel({
  alive,
  framework,
  agentName,
  inputAddon,
  onInputKeyDown,
  placeholder,
  disabled = false,
  heightOffset = 240,
}: ChatPanelProps) {
  const inputRef = useRef<HTMLInputElement>(null);

  const [sessions, setSessions] = useState<Session[]>([]);
  const [activeGroupName, setActiveGroupName] = useState("");
  const [sessionMsgs, setSessionMsgs] = useState<Map<string, ChatMessage[]>>(
    new Map(),
  );
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
  // Track time-to-first-token for latency metrics
  const sendTimeRef = useRef<number | null>(null);
  const latencyRecordedRef = useRef(false);
  const refreshTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [searchParams] = useSearchParams();
  const requestedSession = searchParams.get("session");

  const defaultSessionKey =
    framework === "openclaw" ? "agent:main:main" : "main";
  const groups = useMemo(() => groupSessions(sessions), [sessions]);
  const activeGroup =
    groups.find((g) => g.name === activeGroupName) ?? groups[0];
  const currentSessionKey = activeGroup?.current?.key ?? defaultSessionKey;

  // ── Load sessions ───────────────────────────────────────────────────

  useEffect(() => {
    fetchSessions(agentName)
      .then((resp) => {
        const all = resp.sessions ?? [];
        setSessions(all);
        const gs = groupSessions(all);
        if (requestedSession) {
          const match = gs.find((g) => g.name === requestedSession);
          if (match) {
            setActiveGroupName(match.name);
            return;
          }
        }
        setActiveGroupName(
          gs[0]?.name ?? sessionDisplayName(defaultSessionKey),
        );
      })
      .catch(() => {
        setActiveGroupName(
          requestedSession || sessionDisplayName(defaultSessionKey),
        );
      });
  }, [agentName, alive, defaultSessionKey, requestedSession]);

  // ── Load group messages ─────────────────────────────────────────────

  const prevGroupRef = useRef<string>("");
  const loadGroup = useCallback(
    (group: SessionGroup | undefined) => {
      if (!group) return;
      const isSwitch =
        prevGroupRef.current !== "" && prevGroupRef.current !== group.name;
      prevGroupRef.current = group.name;
      setLoading(true);
      setSessionMsgs(new Map());
      setToggledSet(new Set());
      if (isSwitch) {
        setPendingMsgs([]);
        setStreamingContent("");
        setToolCalls([]);
      }

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
            .catch(() => [k, [] as ChatMessage[]] as const),
        ),
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
          if (msgs.length > 0) {
            setPendingMsgs([]);
          }
        })
        .catch(() => {});
    },
    [agentName],
  );

  useEffect(() => {
    const group = groups.find((g) => g.name === activeGroupName);
    if (group) {
      loadGroup(group);
    } else if (groups.length === 0) {
      setLoading(false);
    }
  }, [activeGroupName, alive, loadGroup, groups]);

  // ── Poll for new messages ───────────────────────────────────────────

  useEffect(() => {
    if (!currentSessionKey || !alive || sending) return;
    const interval = setInterval(() => {
      refreshCurrentSession(currentSessionKey);
    }, 5000);
    return () => clearInterval(interval);
  }, [currentSessionKey, alive, sending, refreshCurrentSession]);

  // ── Build flat items ────────────────────────────────────────────────

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
      flatItems.push({
        kind: "spacer",
        label: arch.title,
        archiveKey: arch.key,
      });
      const msgs = sessionMsgs.get(arch.key) ?? [];
      for (const msg of msgs) {
        if (msg.role === "assistant" && isNoReply(msg.content)) continue;
        flatItems.push({ kind: "message", msg, isCurrent: false, flatIdx });
        flatIdx++;
      }
    }

    let firstMsgHandled = false;
    if (activeGroup.current) {
      if (sortedArchived.length > 0) {
        flatItems.push({
          kind: "spacer",
          label: "current session",
          currentKey: activeGroup.current.key,
        });
      }
      const msgs = sessionMsgs.get(activeGroup.current.key) ?? [];
      let prevTime: number | null = null;
      for (const msg of msgs) {
        if (msg.role === "assistant" && isNoReply(msg.content)) continue;
        const msgTime = msg.timestamp
          ? new Date(msg.timestamp).getTime()
          : 0;

        if (!firstMsgHandled && msgTime > 0) {
          const d = new Date(msgTime);
          const label = d.toLocaleDateString(undefined, {
            month: "short",
            day: "numeric",
            year: "numeric",
          });
          if (sortedArchived.length === 0) {
            flatItems.push({ kind: "spacer", label });
          }
          firstMsgHandled = true;
        }

        if (
          prevTime !== null &&
          msgTime > 0 &&
          msgTime - prevTime > 4 * 60 * 60 * 1000
        ) {
          const d = new Date(msgTime);
          const label = d.toLocaleDateString(undefined, {
            month: "short",
            day: "numeric",
            hour: "numeric",
            minute: "2-digit",
          });
          flatItems.push({ kind: "spacer", label });
        }
        if (msgTime > 0) prevTime = msgTime;
        flatItems.push({ kind: "message", msg, isCurrent: true, flatIdx });
        flatIdx++;
      }
    }

    if (pendingMsgs.length > 0 && !firstMsgHandled) {
      const label = new Date().toLocaleDateString(undefined, {
        month: "short",
        day: "numeric",
        year: "numeric",
      });
      flatItems.push({ kind: "spacer", label });
    }
    for (const msg of pendingMsgs) {
      flatItems.push({ kind: "message", msg, isCurrent: true, flatIdx });
      flatIdx++;
    }
  }

  const totalMsgCount = flatItems.filter(
    (it) => it.kind === "message",
  ).length;

  // WHY localStorage for pending reply: React state (sending) is lost on
  // reload, but the agent may still be processing. We persist a pending flag
  // in localStorage with a TTL so the "thinking..." indicator survives reloads
  // and clears itself on done, error, abort, or expiry (5 min).
  const pendingKey = `pendingReply:${agentName}:${currentSessionKey}`;
  const PENDING_TTL_MS = 5 * 60 * 1000; // 5 minutes

  const setPendingReply = useCallback((active: boolean) => {
    if (active) {
      localStorage.setItem(pendingKey, String(Date.now()));
    } else {
      localStorage.removeItem(pendingKey);
    }
  }, [pendingKey]);

  // Derive the last message in the current session for reply detection.
  const lastCurrentMsg = useMemo(() => {
    for (let i = flatItems.length - 1; i >= 0; i--) {
      const it = flatItems[i];
      if (it.kind === "message" && it.isCurrent) return it.msg;
    }
    return null;
  }, [flatItems]);

  // WHY separate useMemo + useEffect: useMemo must be pure (no side effects).
  // The memo reads localStorage and computes a boolean. The effect handles
  // cleanup (removing stale/expired keys) based on the same conditions.
  const waitingForReply = useMemo(() => {
    if (sending || loading) return false;
    const stored = localStorage.getItem(pendingKey);
    if (!stored) return false;
    const elapsed = Date.now() - Number(stored);
    if (elapsed > PENDING_TTL_MS) return false;
    if (lastCurrentMsg?.role === "assistant") return false;
    return true;
  }, [sending, loading, pendingKey, lastCurrentMsg, totalMsgCount]);

  // Clean up stale pending flags outside of useMemo
  useEffect(() => {
    const stored = localStorage.getItem(pendingKey);
    if (!stored) return;
    const elapsed = Date.now() - Number(stored);
    if (elapsed > PENDING_TTL_MS || lastCurrentMsg?.role === "assistant") {
      localStorage.removeItem(pendingKey);
    }
  }, [pendingKey, lastCurrentMsg, totalMsgCount]);

  // ── Auto-scroll ─────────────────────────────────────────────────────
  const scrollRef = useAutoScroll([totalMsgCount, sending, waitingForReply, streamingContent, toolCalls]);

  // WHY: Track briefing by a composite key (agent + timestamp) rather than
  // a simple boolean. A boolean ref gets reset by React Strict Mode's
  // unmount/remount cycle, causing the briefing to fire twice. The key
  // approach: we store a unique string when the briefing fires, and only
  // fire if the current key doesn't match. A new ?brief=commander navigation
  // generates a fresh key via the URL change itself.

  // ── Unified chat event handler ────────────────────────────────────────

  const handleChatEvent = useCallback(
    (ev: ChatEvent, onDone: () => void) => {
      // Record time-to-first-token latency
      if (!latencyRecordedRef.current && sendTimeRef.current && (ev.type === "delta" || ev.type === "tool_start")) {
        const latencyMs = performance.now() - sendTimeRef.current;
        recordLatency(agentName, latencyMs);
        latencyRecordedRef.current = true;
        sendTimeRef.current = null;
      }

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
          setToolCalls((prev) => matchToolResult(prev, ev));
          break;
        case "done": {
          const raw = ev.content ?? "";
          if (!/^(\[\[no_reply\]\]|NO_REPLY)$/i.test(raw.trim())) {
            setPendingMsgs((prev) => [
              ...prev,
              {
                role: "assistant",
                content: raw,
                timestamp: new Date().toISOString(),
              },
            ]);
          }
          // Record token usage and cost if available
          if (ev.input_tokens || ev.output_tokens || ev.cost_usd) {
            recordUsage(agentName, ev.input_tokens ?? 0, ev.output_tokens ?? 0, ev.cost_usd ?? 0);
          }
          setStreamingContent("");
          setToolCalls([]);
          setSending(false);
          setPendingReply(false);
          onDone();
          break;
        }
        case "error":
          setChatError(ev.error ?? "Unknown error");
          setStreamingContent("");
          setToolCalls([]);
          setSending(false);
          setPendingReply(false);
          break;
      }
    },
    [agentName, setPendingReply],
  );


  const handleStop = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setSending(false);
    setPendingReply(false);
  }, [setPendingReply]);

  // ── Send message ────────────────────────────────────────────────────

  const handleSend = useCallback(() => {
    const text = input.trim();
    if (!text || sending) return;

    setInput("");
    setChatError(null);
    setSending(true);
    setPendingReply(true);
    setStreamingContent("");
    setToolCalls([]);
    sendTimeRef.current = performance.now();
    latencyRecordedRef.current = false;

    const userMsg: ChatMessage = {
      role: "user",
      content: text,
      timestamp: new Date().toISOString(),
    };
    setPendingMsgs((prev) => [...prev, userMsg]);

    const controller = streamMessage(
      agentName,
      text,
      currentSessionKey,
      (ev: ChatEvent) => {
        handleChatEvent(ev, () => {
          // On done: refresh current session and focus input
          inputRef.current?.focus();
          if (refreshTimeoutRef.current) clearTimeout(refreshTimeoutRef.current);
          refreshTimeoutRef.current = setTimeout(() => refreshCurrentSession(currentSessionKey), 500);
        });
      },
    );
    abortRef.current = controller;
  }, [input, sending, agentName, currentSessionKey, refreshCurrentSession, handleChatEvent, setPendingReply]);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
      if (refreshTimeoutRef.current) clearTimeout(refreshTimeoutRef.current);
    };
  }, []);

  // ── Session management ──────────────────────────────────────────────

  const refreshSessions = useCallback(() => {
    fetchSessions(agentName)
      .then((resp) => setSessions(resp.sessions ?? []))
      .catch(() => {});
  }, [agentName]);

  const handleResetSession = useCallback(
    async (key: string) => {
      const name = sessionDisplayName(key);
      if (
        !window.confirm(
          `Reset session "${name}"? The transcript will be archived.`,
        )
      )
        return;
      try {
        await resetSession(agentName, key);
        refreshSessions();
      } catch (e) {
        console.error(e);
      }
    },
    [agentName, refreshSessions],
  );

  const handleDeleteSession = useCallback(
    async (archiveKey: string) => {
      if (
        !window.confirm(
          "Permanently delete this archived session? This cannot be undone.",
        )
      )
        return;
      try {
        await deleteSession(agentName, archiveKey);
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

  const safeDestroySession = useCallback(
    async (key: string) => {
      try {
        await destroySession(agentName, key);
      } catch {
        try {
          await deleteSession(agentName, key);
        } catch {
          // Agent may be stopped — hide the session locally instead.
          // The session still exists in the framework but won't show in Eyrie.
          try {
            await hideSession(agentName, key);
          } catch {
            /* all attempts failed */
          }
        }
      }
    },
    [agentName],
  );

  const handleDestroySession = useCallback(
    async (group: SessionGroup) => {
      if (
        !window.confirm(
          `Destroy session "${group.name}" and all its history?`,
        )
      )
        return;
      try {
        for (const s of group.archived) {
          await safeDestroySession(s.key);
        }
        if (group.current) {
          await safeDestroySession(group.current.key);
        }
        const resp = await fetchSessions(agentName);
        const all = resp.sessions ?? [];
        setSessions(all);
        const gs = groupSessions(all);
        setActiveGroupName(gs[0]?.name ?? "");
      } catch (e) {
        console.error(e);
        refreshSessions();
      }
    },
    [agentName, refreshSessions, safeDestroySession],
  );

  const handleCreateSession = async (name: string) => {
    const cleanName = name
      .trim()
      .toLowerCase()
      .replace(/\s+/g, "-");
    if (!cleanName) return;
    setCreatingSession(false);
    setNewSessionName("");
    try {
      const sess = await createSession(agentName, cleanName);
      setSessions((prev) => [...prev, { key: sess.key, title: sess.title }]);
      setActiveGroupName(cleanName);
    } catch (err) {
      console.error("Failed to create session:", err);
    }
  };

  // ── Expand/collapse helpers ─────────────────────────────────────────

  const longMsgItems = flatItems.filter(
    (it): it is Extract<FlatItem, { kind: "message" }> =>
      it.kind === "message" && it.msg.content.length > 200,
  );

  // ── Render ──────────────────────────────────────────────────────────

  return (
    <div
      className="flex flex-col resize-y overflow-hidden"
      style={{
        height: `calc(100vh - ${heightOffset}px)`,
        minHeight: "300px",
        maxHeight: "calc(100vh - 120px)",
      }}
    >
      {/* Session group bar */}
      <SessionBar
        groups={groups}
        activeGroupName={activeGroupName}
        onSelectGroup={setActiveGroupName}
        onCreateSession={handleCreateSession}
        onDestroySession={handleDestroySession}
        creatingSession={creatingSession}
        onSetCreating={setCreatingSession}
        newSessionName={newSessionName}
        onNewSessionNameChange={setNewSessionName}
      />

      {/* Messages */}
      <div
        ref={scrollRef}
        className={`flex-1 overflow-y-auto border-x border-border bg-surface text-xs ${groups.length === 0 ? "rounded-t border-t" : ""}`}
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
              {"\u2212"}
            </button>
          </div>
        )}

        <div className="px-4 pb-4 pt-2">
          {loading ? (
            <p className="text-text-muted animate-pulse">
              Loading messages...
            </p>
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
                          onClick={() =>
                            handleDeleteSession(item.archiveKey!)
                          }
                          className="rounded px-1 py-0.5 text-[9px] text-text-muted hover:text-red hover:bg-red/10 transition-colors"
                          title="Delete permanently"
                        >
                          delete
                        </button>
                        <button
                          onClick={() =>
                            handleHideSession(item.archiveKey!)
                          }
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
                          onClick={() =>
                            handleResetSession(item.currentKey!)
                          }
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

          {(sending || waitingForReply) && (() => {
            // Build StreamingPart[] from ChatPanel's separate state
            const parts: StreamingPart[] = [
              ...toolCalls.map((tc): StreamingPart => ({
                kind: "tool",
                name: tc.tool,
                done: tc.done,
                args: tc.args,
                output: tc.output,
              })),
              ...(streamingContent ? [{ kind: "text" as const, content: streamingContent }] : []),
            ];
            return (
              <StreamingIndicator
                parts={parts}
                onStop={sending ? handleStop : undefined}
                header={
                  <span className="text-purple font-medium">assistant:</span>
                }
              />
            );
          })()}
        </div>
      </div>

      {chatError && <ChatError message={chatError} />}

      {/* Input */}
      <div className="relative flex items-center gap-2 rounded-b border border-border bg-surface-hover p-3">
        {inputAddon}
        <span className="text-accent text-xs">&gt;</span>
        <input
          ref={inputRef}
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (onInputKeyDown) {
              const handled = onInputKeyDown(e);
              if (handled || e.defaultPrevented) return;
            }
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              handleSend();
            }
          }}
          placeholder={
            placeholder ??
            (alive ? "Type a message..." : "Agent is not running")
          }
          disabled={sending || disabled || !alive}
          className="flex-1 bg-transparent text-xs text-text placeholder:text-text-muted focus:outline-none disabled:opacity-50"
        />
        <button
          onClick={handleSend}
          disabled={sending || disabled || !alive || !input.trim()}
          className="rounded border border-border px-3 py-1 text-[10px] font-medium text-text-secondary transition-colors hover:bg-surface hover:text-text disabled:opacity-30"
        >
          send
        </button>
      </div>
    </div>
  );
}

// Re-exports for backward compatibility
export { PartToolCallCard, StreamingCursor, ToolCallCard, ToolRunCard, MessageRow, groupPartsIntoRuns } from './chat';
