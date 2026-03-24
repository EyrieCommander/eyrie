import {
  useEffect,
  useState,
  useCallback,
  useRef,
  type ReactNode,
  type KeyboardEvent,
} from "react";
import { useSearchParams } from "react-router-dom";
import { Plus, X } from "lucide-react";
import type { ChatMessage, ChatPart, ChatEvent, Session } from "../lib/types";
import {
  fetchSessions,
  fetchChatMessages,
  streamMessage,
  streamCommanderBriefing,
  createSession,
  resetSession,
  deleteSession,
  destroySession,
  hideSession,
} from "../lib/api";

// ── Types ───────────────────────────────────────────────────────────────

export interface ToolCall {
  tool: string;
  toolId?: string;
  args?: Record<string, unknown>;
  output?: string;
  success?: boolean;
  done: boolean;
}

interface SessionGroup {
  name: string;
  current?: Session;
  archived: Session[];
}

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
  const scrollRef = useRef<HTMLDivElement>(null);
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

  const [searchParams, setSearchParams] = useSearchParams();
  const requestedSession = searchParams.get("session");
  const briefMode = searchParams.get("brief");

  const defaultSessionKey =
    framework === "openclaw" ? "agent:main:main" : "main";
  const groups = groupSessions(sessions);
  const activeGroup =
    groups.find((g) => g.name === activeGroupName) ?? groups[0];
  const currentSessionKey = activeGroup?.current?.key ?? defaultSessionKey;

  // ── Load sessions ───────────────────────────────────────────────────

  useEffect(() => {
    if (briefMode === "commander") return;
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
  }, [agentName, alive, defaultSessionKey, requestedSession, briefMode]);

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
  }, [activeGroupName, alive, loadGroup, sessions, groups.length]); // eslint-disable-line react-hooks/exhaustive-deps

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
      let firstMsgHandled = false;
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

    for (const msg of pendingMsgs) {
      flatItems.push({ kind: "message", msg, isCurrent: true, flatIdx });
      flatIdx++;
    }
  }

  const totalMsgCount = flatItems.filter(
    (it) => it.kind === "message",
  ).length;

  // ── Auto-scroll ─────────────────────────────────────────────────────

  const briefTriggered = useRef(false);
  const briefKey = `brief-${agentName}`;

  useEffect(() => {
    briefTriggered.current = !!(window as any)[briefKey];
  }, [agentName, briefKey]);

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [totalMsgCount, sending, streamingContent, toolCalls]);

  // ── Commander briefing ──────────────────────────────────────────────

  useEffect(() => {
    if (briefMode !== "commander" || briefTriggered.current || !alive) return;
    briefTriggered.current = true;
    (window as any)[briefKey] = true;
    setSearchParams({}, { replace: true });

    let mounted = true;
    setSending(true);
    setStreamingContent("");
    setToolCalls([]);

    const { controller } = streamCommanderBriefing((ev) => {
      if (!mounted) return;
      const evType = ev.type as string;
      switch (evType) {
        case "session": {
          const switchToBriefing = () => {
            fetchSessions(agentName)
              .then((resp) => {
                if (!mounted) return;
                const all = resp.sessions ?? [];
                setSessions(all);
                const gs = groupSessions(all);
                const match = gs.find(
                  (g) => g.name === "eyrie-commander-briefing",
                );
                if (match) {
                  setActiveGroupName(match.name);
                } else {
                  setTimeout(() => {
                    fetchSessions(agentName)
                      .then((resp2) => {
                        if (!mounted) return;
                        const all2 = resp2.sessions ?? [];
                        setSessions(all2);
                        setActiveGroupName("eyrie-commander-briefing");
                      })
                      .catch(() => {});
                  }, 1000);
                }
              })
              .catch(() => {});
          };
          switchToBriefing();
          break;
        }
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
              if (
                ((ev.tool_id && updated[i].toolId === ev.tool_id) ||
                  (!ev.tool_id && updated[i].tool === ev.tool)) &&
                !updated[i].done
              ) {
                idx = i;
                break;
              }
            }
            if (idx >= 0) {
              updated[idx] = {
                ...updated[idx],
                output: ev.output,
                success: ev.success,
                done: true,
              };
            }
            return updated;
          });
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
          setStreamingContent("");
          setToolCalls([]);
          setSending(false);
          fetchSessions(agentName)
            .then((resp) => {
              if (!mounted) return;
              const all = resp.sessions ?? [];
              setSessions(all);
              const gs = groupSessions(all);
              const match = gs.find(
                (g) => g.name === "eyrie-commander-briefing",
              );
              if (match) setActiveGroupName(match.name);
            })
            .catch(() => {});
          break;
        }
        case "error":
          setChatError(ev.error || "Briefing failed");
          setSending(false);
          briefTriggered.current = false;
          delete (window as any)[briefKey];
          break;
      }
    });

    return () => {
      mounted = true; // keep same behavior
      controller.abort();
      delete (window as any)[briefKey];
    };
  }, [briefMode, alive, agentName, briefKey, setSearchParams]); // eslint-disable-line react-hooks/exhaustive-deps

  // ── Send message ────────────────────────────────────────────────────

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

    const controller = streamMessage(
      agentName,
      text,
      currentSessionKey,
      (ev: ChatEvent) => {
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
                if (
                  ((ev.tool_id && updated[i].toolId === ev.tool_id) ||
                    (!ev.tool_id && updated[i].tool === ev.tool)) &&
                  !updated[i].done
                ) {
                  idx = i;
                  break;
                }
              }
              if (idx >= 0) {
                updated[idx] = {
                  ...updated[idx],
                  output: ev.output,
                  success: ev.success,
                  done: true,
                };
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
      },
    );
    abortRef.current = controller;
  }, [input, sending, agentName, currentSessionKey, refreshCurrentSession]);

  useEffect(() => {
    return () => {
      abortRef.current?.abort();
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
          await resetSession(agentName, key);
        } catch {
          /* ignore */
        }
        try {
          await deleteSession(agentName, key);
        } catch {
          /* ignore */
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

  const handleCreateSession = async () => {
    const name = newSessionName
      .trim()
      .toLowerCase()
      .replace(/\s+/g, "-");
    if (!name) return;
    setCreatingSession(false);
    setNewSessionName("");
    try {
      const sess = await createSession(agentName, name);
      setSessions((prev) => [...prev, { key: sess.key, title: sess.title }]);
      setActiveGroupName(name);
    } catch {
      const key =
        framework === "openclaw" ? `agent:main:${name}` : name;
      setSessions((prev) => [...prev, { key, title: name }]);
      setActiveGroupName(name);
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
      {groups.length > 0 && (
        <div className="flex items-center gap-1 overflow-x-auto rounded-t border border-b-0 border-border bg-bg-sidebar px-3 py-2">
          {groups.map((g) => (
            <div key={g.name} className="group/tab relative shrink-0">
              <button
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
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  handleDestroySession(g);
                }}
                className="absolute -top-1.5 -right-1.5 hidden group-hover/tab:flex group-focus-within/tab:flex h-4 w-4 items-center justify-center rounded-full bg-surface border border-border text-text-muted transition-colors hover:text-red hover:border-red/50"
                title={`Delete session "${g.name}"`}
              >
                <X className="h-2 w-2" />
              </button>
            </div>
          ))}

          {creatingSession ? (
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
          )}
        </div>
      )}

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
              −
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

          {sending && (
            <div className="py-1">
              {toolCalls.map((tc, i) => (
                <ToolCallCard key={`tc-${i}`} tc={tc} />
              ))}
              {streamingContent ? (
                <div className="py-1">
                  <span className="text-purple font-medium">assistant:</span>{" "}
                  <span className="text-text whitespace-pre-wrap">
                    {streamingContent}
                  </span>
                  <StreamingCursor />
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

// ── Streaming cursor ────────────────────────────────────────────────────

export function StreamingCursor() {
  return (
    <span className="inline-block w-1.5 h-3 bg-accent/60 animate-pulse ml-0.5 align-text-bottom" />
  );
}

// ── Tool call components ────────────────────────────────────────────────

function toolCallSummary(
  _tool: string,
  args: Record<string, any>,
): string {
  const cmd =
    args.command ||
    args.cmd ||
    args.query ||
    args.path ||
    args.url ||
    args.description;
  if (typeof cmd === "string") {
    return cmd.length > 60 ? cmd.slice(0, 57) + "..." : cmd;
  }
  return "";
}

type PartRun =
  | { type: "text"; text: string }
  | { type: "tools"; tools: ChatPart[] };

export function groupPartsIntoRuns(parts: ChatPart[]): PartRun[] {
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

export function ToolRunCard({ tools }: { tools: ChatPart[] }) {
  const [expanded, setExpanded] = useState(false);
  const failCount = tools.filter((t) => t.error).length;
  const names = tools.map((t) => t.name).filter(Boolean);
  const uniqueNames = [...new Set(names)];
  const summary =
    tools.length === 1
      ? tools[0].name ?? "tool"
      : `${tools.length} tools` +
        (uniqueNames.length <= 3
          ? `: ${uniqueNames.join(", ")}`
          : "");

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
            <PartToolCallCard
              key={part.id || `tc-${i}`}
              part={part}
              defaultExpanded
            />
          ))}
        </div>
      )}
    </div>
  );
}

export function PartToolCallCard({
  part,
  defaultExpanded = false,
}: {
  part: ChatPart;
  defaultExpanded?: boolean;
}) {
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
        {part.args && (
          <span className="font-mono text-text-muted truncate max-w-[300px]">
            {toolCallSummary(part.name || "", part.args)}
          </span>
        )}
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

export function ToolCallCard({ tc }: { tc: ToolCall }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="my-1.5 ml-4 rounded border border-border bg-surface-hover/30 text-[11px]">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left"
      >
        <span className="font-mono text-text">{tc.tool}</span>
        {tc.args && (
          <span className="font-mono text-text-muted truncate max-w-[300px]">
            {toolCallSummary(tc.tool, tc.args)}
          </span>
        )}
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

// ── MessageRow ───────────────────────────────────────────────────────────

export function MessageRow({
  msg,
  expanded,
  onToggle,
}: {
  msg: {
    timestamp: string;
    role: string;
    content: string;
    parts?: ChatPart[];
  };
  expanded: boolean;
  onToggle?: () => void;
}) {
  const parts = msg.parts ?? [];
  const toolCount = parts.filter((p) => p.type === "tool_call").length;
  const hasParts = parts.length > 0;
  const isLong = msg.content.length > 200 || toolCount > 0;
  const canToggle = isLong && onToggle;
  const displayText =
    isLong && !expanded
      ? msg.content.length > 200
        ? msg.content.slice(0, 200) + "..."
        : msg.content
      : msg.content;
  const toolSummary =
    !expanded && toolCount > 0
      ? ` [${toolCount} tool${toolCount > 1 ? "s" : ""}]`
      : "";

  return (
    <div
      className={`py-1 ${canToggle ? "cursor-pointer hover:bg-surface-hover/50 rounded px-1 -mx-1" : ""}`}
      onClick={
        canToggle
          ? () => {
              if (!window.getSelection()?.toString()) onToggle!();
            }
          : undefined
      }
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
            <span className="ml-1 text-accent/60 text-[10px]">
              {toolSummary}
            </span>
          )}
        </>
      )}
      {canToggle && !expanded && <span className="ml-1 text-green">▸</span>}
      {canToggle && expanded && <span className="ml-1 text-green">▾</span>}
      {expanded && hasParts && (
        <div className="mt-0.5" onClick={(e) => e.stopPropagation()}>
          {groupPartsIntoRuns(parts).map((run, ri) =>
            run.type === "text" ? (
              <div
                key={`text-${ri}`}
                className="text-text whitespace-pre-wrap py-0.5"
              >
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
