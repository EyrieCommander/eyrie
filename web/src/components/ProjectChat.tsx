// ProjectChat.tsx — Real-time project group chat with SSE streaming.
//
// WHY no mountedRef pattern:
//   DO NOT add a `mountedRef` (useRef tracking mount state) to guard SSE
//   callbacks. If a parent re-render causes even a brief unmount/remount,
//   the SSE callback retains a reference to the OLD mountedRef (set to false),
//   silently dropping ALL subsequent events. This was the root cause of
//   "streaming events not rendering" — events arrived through the proxy but
//   the callback discarded them. Use AbortController for cleanup instead.
//   See: ProjectDetail.tsx always-mounts this component to avoid the issue.
//
// WHY merge-based sync (not replace) on `done` event:
//   When the SSE stream completes, we fetch server messages and MERGE them
//   with local state rather than replacing. The agent's response is written
//   to chat.jsonl by a detached goroutine (see orchestrate.go) that may not
//   have flushed to disk by the time the SSE `done` event fires. Replacing
//   would lose messages received via SSE that haven't been persisted yet.
//
// WHY optimistic user messages:
//   The user message appears immediately in the UI (with an "optimistic-"
//   prefixed ID) before the server acknowledges it. This provides instant
//   feedback while the SSE round-trip completes. The optimistic message is
//   replaced with the server version when it arrives.
//
// WHY 60s response timeout:
//   If no SSE activity arrives for 60 seconds, we assume the agent is stuck
//   and abort. This is generous enough for slow LLM responses (which stream
//   deltas within seconds of starting) but catches genuine failures like
//   agent crashes or network partitions.
//
// WHY 4s polling interval:
//   When idle (not streaming), we poll for new messages every 4 seconds. This
//   catches messages from other sources (agents talking to each other, system
//   events) without hammering the server. During streaming, SSE provides
//   real-time updates so polling is disabled.

import { useState, useEffect, useCallback, useRef, useMemo } from "react";
import { Send } from "lucide-react";
import type { ProjectChatMessage } from "../lib/types";
import { ToolRunCard, groupPartsIntoRuns } from "./ChatPanel";
import { ChatError } from "./chat/ChatError";
import { StreamingIndicator } from "./chat/StreamingIndicator";
import type { StreamingPart } from "./chat/StreamingIndicator";
import { roleLabel, roleColor } from "./chat/MessageHeader";
import { fetchProjectChat, streamProjectChat, stopProjectChat, projectChatStatus } from "../lib/api";
import { useAutoScroll } from "../lib/useAutoScroll";
import { useData } from "../lib/DataContext";
import { recordLatency, recordUsage } from "../lib/useAgentMetrics";

// StreamingPart type imported from chat/StreamingIndicator

function ProjectMessageHeader({ role, sender, displayName, time, toolCount }: {
  role: string; sender?: string; displayName?: string; time: string; toolCount?: number;
}) {
  return (
    <div className="flex items-baseline gap-2">
      <span className={`font-bold ${roleColor(role)}`}>
        {roleLabel(role, displayName, sender)}
      </span>
      <span className="text-[10px] text-text-muted">{time}</span>
      {(toolCount ?? 0) > 0 && (
        <span className="text-[10px] text-accent/60">[{toolCount} tool{toolCount! > 1 ? "s" : ""}]</span>
      )}
    </div>
  );
}

export interface ProjectChatProps {
  projectId: string;
  participants: { name: string; role: string }[];
}

export function ProjectChat({ projectId, participants }: ProjectChatProps) {
  const { agents, instances } = useData();
  const displayNames = useMemo(() => {
    const map = new Map<string, string>();
    for (const a of agents) { if (a.display_name) map.set(a.name, a.display_name); }
    for (const i of instances) { if (i.display_name) map.set(i.name, i.display_name); }
    return map;
  }, [agents, instances]);

  const [messages, setMessages] = useState<ProjectChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [chatError, setChatError] = useState("");
  const [streamingAgent, setStreamingAgent] = useState("");
  const [streamingRole, setStreamingRole] = useState("");
  const [streamingTime, setStreamingTime] = useState("");
  const [streamingParts, setStreamingParts] = useState<StreamingPart[]>([]);
  const [showMentions, setShowMentions] = useState(false);
  const [mentionFilter, setMentionFilter] = useState("");
  const [mentionIdx, setMentionIdx] = useState(0);
  const scrollRef = useAutoScroll([messages, streamingParts]);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const timeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Latency tracking: measures time from handoff to first token per agent.
  // For the first agent, handoff = user send time.
  // For subsequent agents, handoff = previous agent's "done" timestamp.
  const latencyStartRef = useRef<number | null>(null);
  const latencyRecordedForRef = useRef<Set<string>>(new Set());

  const RESPONSE_TIMEOUT = 60_000; // 60 seconds with no SSE activity

  // WHY no abort on timeout: The agent may still be processing (tool
  // calls, long LLM response). Aborting would kill the SSE stream and
  // cause message loss. Instead, show a warning and let the stream
  // continue. If the agent truly isn't responding, the user can click stop.
  const resetTimeout = useCallback(() => {
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    timeoutRef.current = setTimeout(() => {
      setChatError("agent may be unresponsive — click stop if needed");
    }, RESPONSE_TIMEOUT);
  }, []);
  const abortRef = useRef<AbortController | null>(null);

  // WHY backgroundStreaming: When the user navigates away during streaming
  // and comes back, the agent may still be responding (detached context).
  // We detect this via the status endpoint and poll rapidly (every 1s)
  // to show the incrementally-persisted content as it arrives.
  const [backgroundStreaming, setBackgroundStreaming] = useState(false);

  // Load messages on mount + check if a response is in-flight
  const [chatLoaded, setChatLoaded] = useState(false);
  useEffect(() => {
    setChatLoaded(false);
    Promise.all([
      fetchProjectChat(projectId),
      projectChatStatus(projectId),
    ]).then(([msgs, status]) => {
      setMessages(msgs);
      setBackgroundStreaming(status.streaming);
      setChatLoaded(true);
    }).catch((err) => { console.error(err); setChatLoaded(true); });
  }, [projectId]);

  // Poll for new messages — fast (1s) when agent is responding in
  // background, slow (4s) when idle. Checks status each cycle to
  // detect when the response completes.
  useEffect(() => {
    if (sending) return; // SSE handles updates while we're the sender
    const interval = backgroundStreaming ? 1000 : 4000;
    const id = setInterval(() => {
      const statusCheck = backgroundStreaming
        ? projectChatStatus(projectId).then((s) => {
            if (!s.streaming) setBackgroundStreaming(false);
          })
        : Promise.resolve();

      Promise.all([
        fetchProjectChat(projectId),
        statusCheck,
      ]).then(([msgs]) => {
        setMessages((prev) => {
          if (msgs.length === prev.length && !backgroundStreaming) return prev;
          const ids = new Set(msgs.map((m: ProjectChatMessage) => m.id));
          const extras = prev.filter((m) => !ids.has(m.id) && !m.id.startsWith("optimistic-"));
          return [...msgs, ...extras];
        });
      }).catch(() => {});
    }, interval);
    return () => clearInterval(id);
  }, [projectId, sending, backgroundStreaming]);

  // Auto-scroll handled by useAutoScroll above

  useEffect(() => () => {
    abortRef.current?.abort();
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
  }, []);

  // Helper: mark agent as streaming (sets agent + time on first call)
  const markStreaming = (sender: string, role: string) => {
    setStreamingAgent((prev) => {
      if (!prev) setStreamingTime(new Date().toLocaleTimeString());
      return sender;
    });
    setStreamingRole(role);
  };

  // Helper: clear all streaming state
  const clearStreaming = () => {
    setStreamingAgent("");
    setStreamingParts([]);
    setStreamingTime("");
  };

  // Helper: filter participants for @mention autocomplete
  const filteredParticipants = (filter: string) =>
    participants.filter((p) => !filter || p.role.toLowerCase().includes(filter) || p.name.toLowerCase().includes(filter));

  const send = useCallback((text: string) => {
    if (!text || sending) return;
    setSending(true);
    setChatError("");
    clearStreaming();
    latencyStartRef.current = performance.now();
    latencyRecordedForRef.current = new Set();

    // Optimistic: show user message immediately
    setMessages((prev) => [...prev, {
      id: `optimistic-${Date.now()}`,
      sender: "user",
      role: "user",
      content: text,
      timestamp: new Date().toISOString(),
    }]);

    resetTimeout(); // Start response timeout
    const ctrl = streamProjectChat(projectId, text, (event) => {
      resetTimeout(); // Reset on any SSE activity
      switch (event.type) {
        case "message":
          if (event.message) {
            setMessages((prev) => {
              // Replace optimistic user message with server version
              const m = event.message!;
              if (m.role === "user" && prev.some((p) => p.id.startsWith("optimistic-") && p.content === m.content)) {
                return prev.map((p) => p.id.startsWith("optimistic-") && p.content === m.content ? m : p);
              }
              return [...prev, m];
            });
          }
          break;

        case "agent_event":
          if (event.event) {
            const ev = event.event;
            // Record per-agent latency: time from handoff to first token.
            // First agent's handoff = user send time. Subsequent = previous agent's done.
            if (latencyStartRef.current && event.sender && !latencyRecordedForRef.current.has(event.sender) && (ev.type === "delta" || ev.type === "tool_start")) {
              recordLatency(event.sender, performance.now() - latencyStartRef.current);
              latencyRecordedForRef.current.add(event.sender);
            }
            // When an agent finishes, reset the clock for the next agent
            if (ev.type === "done" && event.sender) {
              latencyStartRef.current = performance.now();
            }
            if (ev.type === "delta") {
              markStreaming(event.sender || "", event.role || "");
              setStreamingParts((prev) => {
                const last = prev[prev.length - 1];
                if (last && last.kind === "text") {
                  return [...prev.slice(0, -1), { kind: "text", content: last.content + (ev.content || "") }];
                }
                return [...prev, { kind: "text", content: ev.content || "" }];
              });
            } else if (ev.type === "tool_start") {
              markStreaming(event.sender || "", event.role || "");
              setStreamingParts((prev) => [...prev, { kind: "tool", name: ev.tool || "tool", done: false, args: ev.args }]);
            } else if (ev.type === "tool_result") {
              setStreamingParts((prev) => {
                const updated = [...prev];
                for (let i = updated.length - 1; i >= 0; i--) {
                  const p = updated[i];
                  if (p.kind === "tool" && !p.done) { updated[i] = { ...p, done: true, output: ev.output }; break; }
                }
                return updated;
              });
            } else if (ev.type === "done") {
              // Record token usage attributed to this agent
              if (event.sender && (ev.input_tokens !== undefined || ev.output_tokens !== undefined || ev.cost_usd !== undefined)) {
                recordUsage(event.sender, ev.input_tokens ?? 0, ev.output_tokens ?? 0, ev.cost_usd ?? 0);
              }
              clearStreaming();
            } else if (ev.type === "error") {
              setMessages((prev) => [...prev, {
                id: `err-${Date.now()}`,
                sender: event.sender || "agent",
                role: event.role || "system",
                content: `error: ${ev.content || ev.error || "unknown"}`,
                timestamp: new Date().toISOString(),
              }]);
            }
          }
          break;

        case "done":
          if (timeoutRef.current) clearTimeout(timeoutRef.current);
          setSending(false);
          clearStreaming();
          // Merge server messages with local state rather than replacing,
          // so messages received via SSE aren't lost if they haven't been
          // written to disk yet (race with detached context storage).
          fetchProjectChat(projectId).then((serverMsgs) => {
            setMessages((prev) => {
              const ids = new Set(serverMsgs.map((m) => m.id));
              // Keep any local messages not yet on server (SSE-received)
              const extras = prev.filter((m) => !ids.has(m.id) && !m.id.startsWith("optimistic-"));
              return [...serverMsgs, ...extras];
            });
          }).catch(() => {});
          break;

        case "debug":
          console.log(`[eyrie] ${event.msg}`, event.detail || "");
          break;

        case "error":
          if (timeoutRef.current) clearTimeout(timeoutRef.current);
          setSending(false);
          setChatError(event.error || "failed");
          break;
      }
    });
    abortRef.current = ctrl;
  }, [sending, projectId]);

  const handleSend = useCallback(() => {
    const msg = input.trim();
    if (!msg) return;
    setInput("");
    if (inputRef.current) inputRef.current.style.height = "auto";
    send(msg);
  }, [input, send]);

  const handleStop = useCallback(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    if (timeoutRef.current) clearTimeout(timeoutRef.current);
    setSending(false);
    // Cancel the backend's detached orchestration so the agent stops too
    stopProjectChat(projectId).catch(() => {});
  }, [projectId]);

  // Auto-start project chat when loaded with no messages.
  // WHY autoStartedRef: Prevents double-fire in StrictMode. The ref is set
  // to true before the send call, so the re-mount cycle sees it as already
  // started. The ref resets on remount (e.g. chatKey increment after reset),
  // which is exactly when we want auto-start to fire again.
  const autoStartedRef = useRef(false);
  // Reset the auto-start flag when projectId changes so a new project
  // can trigger auto-start without requiring a parent remount.
  useEffect(() => {
    autoStartedRef.current = false;
  }, [projectId]);
  useEffect(() => {
    if (chatLoaded && !autoStartedRef.current && !sending && messages.length === 0) {
      autoStartedRef.current = true;
      send("Let's get started on this project.");
    }
  }, [chatLoaded, sending, messages.length, send]);

  // Sort messages: system before user when timestamps are within 1 second
  const sortedMessages = [...messages].sort((a, b) => {
    const ta = new Date(a.timestamp).getTime();
    const tb = new Date(b.timestamp).getTime();
    if (Math.abs(ta - tb) < 1000) {
      if (a.role === "system" && b.role === "user") return -1;
      if (a.role === "user" && b.role === "system") return 1;
    }
    return ta - tb;
  });

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div ref={scrollRef} className="flex-1 overflow-y-auto space-y-3 p-4">
        {/* Error display */}
        {chatError && (
          <ChatError message={chatError} />
        )}

        {/* Messages — hide pre-chat system messages until chat has started */}
        {sortedMessages
          .filter((m) => messages.some((x) => x.role !== "system") || m.role !== "system")
          .map((msg) => {
          const parts = msg.parts ?? [];
          const hasParts = parts.length > 0;
          const toolCount = parts.filter((p) => p.type === "tool_call").length;
          return (
            <div key={msg.id} className="text-xs">
              <ProjectMessageHeader
                role={msg.role}
                sender={msg.sender}
                displayName={displayNames.get(msg.sender)}
                time={new Date(msg.timestamp).toLocaleTimeString()}
                toolCount={toolCount}
              />
              {hasParts ? (
                <div className="mt-1 space-y-1">
                  {groupPartsIntoRuns(parts).map((run, ri) =>
                    run.type === "text" ? (
                      <div key={`${msg.id}-r-${ri}`} className="text-text whitespace-pre-wrap">{run.text}</div>
                    ) : (
                      <ToolRunCard key={`${msg.id}-r-${ri}`} tools={run.tools} />
                    )
                  )}
                </div>
              ) : (
                <div className="mt-0.5 text-text whitespace-pre-wrap">{msg.content}</div>
              )}
            </div>
          );
        })}

        {/* Streaming indicator */}
        {sending && streamingAgent && (
          <StreamingIndicator
            parts={streamingParts}
            onStop={handleStop}
            header={
              <ProjectMessageHeader
                role={streamingRole || "agent"}
                sender={streamingAgent}
                displayName={displayNames.get(streamingAgent)}
                time={streamingTime}
              />
            }
          />
        )}

        {/* Waiting indicator */}
        {sending && !streamingAgent && messages.length > 0 && (
          <StreamingIndicator
            parts={[]}
            onStop={handleStop}
            header={
              <div className="flex items-center gap-2 text-text-muted">
                <span className="h-1 w-1 rounded-full bg-accent animate-pulse" />
                waiting for agent response...
              </div>
            }
          />
        )}

        {/* Background streaming indicator — agent is responding but we reconnected */}
        {backgroundStreaming && !sending && (
          <div className="text-xs py-1 flex items-center gap-2 text-text-muted">
            <span className="h-1 w-1 rounded-full bg-accent animate-pulse" />
            agent is still responding...
          </div>
        )}
      </div>

      {/* Input */}
      <div className="relative border-t border-border p-3 flex gap-2">
        {showMentions && (() => {
          const filtered = filteredParticipants(mentionFilter);
          if (filtered.length === 0) return null;
          return (
            <div className="absolute bottom-full left-3 mb-1 rounded border border-border bg-bg shadow-lg py-1 min-w-[160px]">
              {filtered.map((p, i) => (
                <button
                  key={p.name}
                  onClick={() => {
                    const atIdx = input.lastIndexOf("@");
                    setInput((atIdx >= 0 ? input.slice(0, atIdx) : input) + "@" + p.role + " ");
                    setShowMentions(false);
                    inputRef.current?.focus();
                  }}
                  className={`flex w-full items-center gap-2 px-3 py-1.5 text-xs text-left ${i === mentionIdx ? "bg-surface-hover" : "hover:bg-surface-hover"}`}
                >
                  <span className={`font-bold ${roleColor(p.role)}`}>{p.role}</span>
                  <span className="text-text-muted">{p.name}</span>
                </button>
              ))}
            </div>
          );
        })()}
        <textarea
          ref={inputRef}
          rows={1}
          value={input}
          onChange={(e) => {
            setInput(e.target.value);
            e.target.style.height = "auto";
            e.target.style.height = Math.min(e.target.scrollHeight, 150) + "px";
            const atIdx = e.target.value.lastIndexOf("@");
            if (atIdx >= 0 && (atIdx === 0 || e.target.value[atIdx - 1] === " ")) {
              setMentionFilter(e.target.value.slice(atIdx + 1).toLowerCase());
              setShowMentions(true);
              setMentionIdx(0);
            } else {
              setShowMentions(false);
            }
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") { setShowMentions(false); return; }
            if (showMentions) {
              const filtered = filteredParticipants(mentionFilter);
              if (e.key === "ArrowDown") { e.preventDefault(); setMentionIdx((i) => Math.min(i + 1, filtered.length - 1)); return; }
              if (e.key === "ArrowUp") { e.preventDefault(); setMentionIdx((i) => Math.max(i - 1, 0)); return; }
              if ((e.key === "Enter" || e.key === "Tab") && filtered.length > 0) {
                e.preventDefault();
                const p = filtered[Math.min(mentionIdx, filtered.length - 1)];
                const atIdx = input.lastIndexOf("@");
                setInput((atIdx >= 0 ? input.slice(0, atIdx) : input) + "@" + p.role + " ");
                setShowMentions(false);
                return;
              }
            }
            if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); handleSend(); }
          }}
          className="flex-1 resize-none rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
          placeholder="type a message... (@ to mention)"
          disabled={sending}
        />
        <button
          onClick={handleSend}
          disabled={sending || !input.trim()}
          className="rounded bg-accent px-3 py-2 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
        >
          <Send className="h-3.5 w-3.5" />
        </button>
      </div>
    </div>
  );
}
