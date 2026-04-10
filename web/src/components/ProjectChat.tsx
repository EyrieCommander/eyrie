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
import { MessageRow } from "./ChatPanel";
import { ChatError } from "./chat/ChatError";
import { StreamingIndicator } from "./chat/StreamingIndicator";
import type { StreamingPart } from "./chat/StreamingIndicator";
import { roleLabel, roleColor } from "./chat/MessageHeader";
import { fetchProjectChat, streamProjectChat, stopProjectChat, projectChatStatus } from "../lib/api";
import { useAutoScroll } from "../lib/useAutoScroll";
import { useData } from "../lib/DataContext";
import { recordLatency, recordUsage } from "../lib/useAgentMetrics";

// StreamingPart type imported from chat/StreamingIndicator

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
  const [pendingAgent, setPendingAgent] = useState(""); // set from routing debug event
  const [streamingParts, setStreamingParts] = useState<StreamingPart[]>([]);
  const [toggledSet, setToggledSet] = useState<Set<string>>(new Set());
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

  // Helper: mark agent as streaming. If a different agent was streaming,
  // flush its parts into messages first so they don't disappear.
  const markStreaming = (sender: string, role: string) => {
    setStreamingAgent((prev) => {
      if (prev && prev !== sender) {
        // Previous agent's parts → convert to a stored message
        setStreamingParts((parts) => {
          if (parts.length > 0) {
            const text = parts.filter((p) => p.kind === "text").map((p) => p.content || "").join("");
            if (text) {
              setMessages((msgs) => [...msgs, {
                id: `stream-${prev}-${Date.now()}`,
                sender: prev,
                role: streamingRole || "agent",
                content: text,
                timestamp: streamingTime || new Date().toISOString(),
              }]);
            }
          }
          return []; // Clear for the new agent
        });
      }
      if (!prev) setStreamingTime(new Date().toLocaleTimeString());
      return sender;
    });
    setStreamingRole(role);
  };

  // Helper: clear all streaming state
  const clearStreaming = () => {
    setStreamingAgent("");
    setStreamingParts([]);
    setPendingAgent("");
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
            const m = event.message;
            // Skip agent response messages — they're already visible
            // via streamingParts from the agent_event deltas. Adding
            // them to messages too causes duplication. The idle poll
            // will pick them up from the server after streaming ends.
            if (m.role !== "user" && m.role !== "system") break;

            setMessages((prev) => {
              // Replace optimistic user message with server version
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
              // Don't clearStreaming — streaming parts stay visible to
              // preserve tool call expand/collapse state. Duplicate stored
              // message is filtered out in the render. Cleared on next send().
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
          // Messages were already added via SSE "message" events during
          // streaming. No need to re-fetch — that would re-render
          // everything and lose tool call expand/collapse state.
          break;

        case "debug":
          console.log(`[eyrie] ${event.msg}`, event.detail || "");
          // "routing to magnus (commander)" → extract "magnus"
          if (event.msg?.startsWith("routing to ")) {
            const name = event.msg.slice(11).replace(/ \(.*\)$/, "");
            setPendingAgent(displayNames.get(name) || name);
          }
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
    // Don't clearStreaming() — keep the partial content visible so the
    // user can see what the agent was doing when they stopped it.
    // Streaming state is cleared on the next send().
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
  const sortedMessages = useMemo(() => [...messages].sort((a, b) => {
    const ta = new Date(a.timestamp).getTime();
    const tb = new Date(b.timestamp).getTime();
    if (Math.abs(ta - tb) < 1000) {
      if (a.role === "system" && b.role === "user") return -1;
      if (a.role === "user" && b.role === "system") return 1;
    }
    return ta - tb;
  }), [messages]);

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div ref={scrollRef} className="flex-1 overflow-y-auto space-y-3 p-4 text-xs">
        {/* Expand/collapse controls — sticky top-right, same as 1:1 chat */}
        {sortedMessages.length > 1 && (
          <div className="sticky top-0 z-10 float-right flex gap-0.5 pr-2 pt-2">
            <button
              onClick={() => setToggledSet(new Set())}
              className="text-green font-bold text-sm leading-none px-1 rounded hover:bg-surface-hover transition-colors"
              title="Expand all"
            >
              +
            </button>
            <button
              onClick={() => setToggledSet(new Set(sortedMessages.map((m) => m.id)))}
              className="text-purple font-bold text-sm leading-none px-1 rounded hover:bg-surface-hover transition-colors"
              title="Compact all"
            >
              {"\u2212"}
            </button>
          </div>
        )}

        {/* Error display */}
        {chatError && (
          <ChatError message={chatError} />
        )}

        {/* Messages — hide pre-chat system messages until chat has started.
            When streamingParts is non-empty, the last message from that agent
            is already rendered by StreamingIndicator — skip it here to avoid
            duplication. */}
        {(() => {
          const hasNonSystem = sortedMessages.some((x) => x.role !== "system");
          let visible = sortedMessages.filter((m) => hasNonSystem || m.role !== "system");
          if (streamingParts.length > 0 && streamingAgent) {
            // Find the last message from the streaming agent and exclude it
            const lastIdx = visible.map((m) => m.sender).lastIndexOf(streamingAgent);
            if (lastIdx >= 0) {
              visible = visible.filter((_, i) => i !== lastIdx);
            }
          }
          return visible;
        })().map((msg) => {
          // Default: expanded. Toggle collapses.
          const expanded = !toggledSet.has(msg.id);
          return (
            <MessageRow
              key={msg.id}
              msg={{
                ...msg,
                timestamp: typeof msg.timestamp === "string" ? msg.timestamp : new Date(msg.timestamp).toISOString(),
                display_name: displayNames.get(msg.sender),
              }}
              expanded={expanded}
              onToggle={() => {
                setToggledSet((prev) => {
                  const next = new Set(prev);
                  if (next.has(msg.id)) next.delete(msg.id);
                  else next.add(msg.id);
                  return next;
                });
              }}
            />
          );
        })}

        {/* Streaming indicator */}
        {/* Show streaming indicator while actively streaming, or after stop
            if there's partial content to preserve */}
        {((sending && streamingAgent) || (!sending && streamingParts.length > 0)) && (
          <StreamingIndicator
            parts={streamingParts}
            onStop={sending ? handleStop : undefined}
            header={
              <>
                <span className="text-text-muted">{streamingTime}</span>{" "}
                <span className={`font-medium ${roleColor(streamingRole || "agent")}`}>
                  {roleLabel(streamingRole || "agent", displayNames.get(streamingAgent), streamingAgent)}:
                </span>
              </>
            }
          />
        )}

        {/* Waiting indicator — before the agent starts streaming */}
        {sending && !streamingAgent && messages.length > 0 && (
          <div className="text-xs py-1">
            <div className="flex items-center gap-2 text-text-muted animate-pulse">
              <span className="h-1 w-1 rounded-full bg-accent" />
              {pendingAgent ? `waiting for ${pendingAgent}...` : "waiting for agent response..."}
            </div>
            <button
              onClick={handleStop}
              className="mt-1.5 rounded border border-border px-2 py-0.5 text-[10px] text-text-muted hover:border-red/50 hover:text-red transition-colors"
            >
              stop
            </button>
          </div>
        )}

        {/* Background streaming indicator — agent is responding but we reconnected */}
        {backgroundStreaming && !sending && (
          <div className="text-xs py-1">
            <div className="flex items-center gap-2 text-text-muted animate-pulse">
              <span className="h-1 w-1 rounded-full bg-accent" />
              {pendingAgent ? `waiting for ${pendingAgent}...` : "waiting for agent response..."}
            </div>
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
