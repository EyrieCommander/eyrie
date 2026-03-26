import { useState, useEffect, useCallback, useRef } from "react";
import { Send } from "lucide-react";
import type { ProjectChatMessage, ChatPart } from "../lib/types";
import { PartToolCallCard, StreamingCursor } from "./ChatPanel";
import { fetchProjectChat, streamProjectChat } from "../lib/api";

const ROLE_COLORS: Record<string, string> = {
  user: "text-green",
  commander: "text-purple",
  captain: "text-yellow-400",
  talon: "text-blue-400",
  system: "text-text-muted",
};

export interface ProjectChatProps {
  projectId: string;
  participants: { name: string; role: string }[];
}

export function ProjectChat({ projectId, participants }: ProjectChatProps) {
  const [messages, setMessages] = useState<ProjectChatMessage[]>([]);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [chatError, setChatError] = useState("");
  const [fetchError, setFetchError] = useState<string | null>(null);
  const [streamingSender, setStreamingSender] = useState<string | null>(null);
  const [streamingRole, setStreamingRole] = useState<string | null>(null);
  const [streamingContent, setStreamingContent] = useState("");
  const [streamingToolCalls, setStreamingToolCalls] = useState<ChatPart[]>([]);
  const [showMentions, setShowMentions] = useState(false);
  const [mentionFilter, setMentionFilter] = useState("");
  const [mentionIdx, setMentionIdx] = useState(0);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const streamControllerRef = useRef<AbortController | null>(null);
  const mountedRef = useRef(true);

  // Load chat history and poll for new messages
  useEffect(() => {
    fetchProjectChat(projectId).then(setMessages).catch((err) => {
      console.error("fetchProjectChat error:", err);
      setFetchError(err instanceof Error ? err.message : "Failed to load chat");
    });
    const interval = setInterval(() => {
      if (sending) return;
      fetchProjectChat(projectId).then((msgs) => {
        setMessages((prev) => {
          if (msgs.length !== prev.length) return msgs;
          // Check if any message identity changed (edits/replacements)
          for (let i = 0; i < msgs.length; i++) {
            if (msgs[i].id !== prev[i].id || msgs[i].timestamp !== prev[i].timestamp) {
              return msgs;
            }
          }
          return prev;
        });
      }).catch((err) => {
        console.error("fetchProjectChat poll error:", err);
      });
    }, 4000);
    return () => clearInterval(interval);
  }, [projectId, sending]);

  // Auto-scroll
  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [messages, streamingContent]);

  useEffect(() => {
    return () => {
      mountedRef.current = false;
      streamControllerRef.current?.abort();
    };
  }, []);

  const sendMsg = useCallback((msg: string) => {
    if (!msg || sending) return;
    setSending(true);
    setChatError("");
    setStreamingContent("");
    setStreamingSender(null);
    setStreamingToolCalls([]);

    // Optimistic: show user message immediately
    setMessages((prev) => [...prev, {
      id: `optimistic-${Date.now()}`,
      sender: "user",
      role: "user",
      content: msg,
      timestamp: new Date().toISOString(),
    }]);

    const controller = streamProjectChat(projectId, msg, (event) => {
      if (!mountedRef.current) return;
      if (event.type === "message" && event.message) {
        const msg = event.message;
        setMessages((prev) => {
          // Skip if this is the user message we already added optimistically
          if (msg.role === "user" && prev.some((m) => m.id.startsWith("optimistic-") && m.content === msg.content)) {
            return prev.map((m) => m.id.startsWith("optimistic-") && m.content === msg.content ? msg : m);
          }
          return [...prev, msg];
        });
        setStreamingContent("");
        setStreamingSender(null);
        setStreamingRole(null);
        setStreamingToolCalls([]);
      } else if (event.type === "agent_event" && event.event) {
        const ev = event.event;
        if (ev.type === "delta") {
          setStreamingSender(event.sender || null);
          setStreamingRole(event.role || null);
          setStreamingContent((prev) => prev + (ev.content || ""));
        } else if (ev.type === "tool_start") {
          setStreamingSender(event.sender || null);
          setStreamingRole(event.role || null);
          setStreamingToolCalls((prev) => [...prev, {
            type: "tool_call",
            id: ev.tool_id,
            name: ev.tool,
            pending: true,
          }]);
        } else if (ev.type === "tool_result") {
          setStreamingToolCalls((prev) => {
            const updated = [...prev];
            for (let i = updated.length - 1; i >= 0; i--) {
              if ((ev.tool_id && updated[i].id === ev.tool_id) ||
                  (!ev.tool_id && updated[i].name === ev.tool && updated[i].pending)) {
                updated[i] = { ...updated[i], output: ev.output, pending: false };
                break;
              }
            }
            return updated;
          });
        } else if (ev.type === "error") {
          const agentName = event.sender || "agent";
          const errContent = ev.content || "unknown error";
          setMessages((prev) => [...prev, {
            id: `err-${Date.now()}`,
            sender: agentName,
            role: event.role || "system",
            content: `error: ${errContent}`,
            timestamp: new Date().toISOString(),
          }]);
          setStreamingContent("");
          setStreamingSender(null);
          setStreamingRole(null);
        }
      } else if (event.type === "done") {
        setSending(false);
        setStreamingContent("");
        setStreamingSender(null);
        setStreamingRole(null);
        setStreamingToolCalls([]);
      } else if (event.type === "error") {
        setSending(false);
        setChatError(event.error || "failed to send message");
      }
    });
    streamControllerRef.current = controller;
  }, [sending, projectId]);

  const handleSend = useCallback(() => {
    const msg = input.trim();
    if (!msg) return;
    setInput("");
    sendMsg(msg);
  }, [input, sendMsg]);

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      {/* Messages */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto space-y-3 p-4">
        {messages.length === 0 && !sending && (
          <div className="text-center py-10 space-y-4">
            <p className="text-xs text-text-muted">
              start the project chat to bring the team together
            </p>
            {chatError && (
              <div className="rounded border border-red/30 bg-red/5 px-4 py-2 text-xs text-red max-w-sm mx-auto">
                {chatError}
              </div>
            )}
            <button
              onClick={() => sendMsg("Let's get started on this project.")}
              disabled={sending}
              className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
            >
              start project chat
            </button>
          </div>
        )}
        {messages.map((msg) => {
          const parts = msg.parts ?? [];
          const toolParts = parts.filter((p) => p.type === "tool_call");
          const hasParts = toolParts.length > 0;
          return (
            <div key={msg.id} className="text-xs">
              <div className="flex items-baseline gap-2">
                <span className={`font-bold ${ROLE_COLORS[msg.role] || "text-text"}`}>
                  {msg.role === "user" ? "you" : msg.role}
                </span>
                {msg.role !== "user" && msg.sender !== msg.role && (
                  <span className="text-[10px] text-text-muted">({msg.sender})</span>
                )}
                <span className="text-[10px] text-text-muted">
                  {new Date(msg.timestamp).toLocaleTimeString()}
                </span>
                {hasParts && (
                  <span className="text-[10px] text-accent/60">
                    [{toolParts.length} tool{toolParts.length > 1 ? "s" : ""}]
                  </span>
                )}
              </div>
              {hasParts && (
                <div className="mt-1 space-y-1">
                  {toolParts.map((tc, i) => (
                    <PartToolCallCard key={`${msg.id}-tc-${i}`} part={tc} />
                  ))}
                </div>
              )}
              <div className="mt-0.5 text-text whitespace-pre-wrap">{msg.content}</div>
            </div>
          );
        })}
        {sending && !streamingContent && !streamingSender && streamingToolCalls.length === 0 && (
          <div className="text-xs py-1 text-text-muted animate-pulse">
            agent thinking...
          </div>
        )}
        {streamingSender && (
          <div className="text-xs">
            <div className="flex items-baseline gap-2">
              <span className={`font-bold ${ROLE_COLORS[streamingRole || "captain"] || "text-text"}`}>
                {streamingRole || "agent"}
              </span>
              {streamingSender !== streamingRole && (
                <span className="text-[10px] text-text-muted">({streamingSender})</span>
              )}
            </div>
            {streamingToolCalls.length > 0 && (
              <div className="mt-1 space-y-1">
                {streamingToolCalls.map((tc, i) => (
                  <div key={`stc-${i}`} className="rounded border border-border bg-surface px-2 py-1">
                    <div className="flex items-center gap-2">
                      <span className="text-accent text-[10px] font-mono">{tc.name}</span>
                      {!tc.output && <span className="text-[10px] text-text-muted animate-pulse">running...</span>}
                    </div>
                  </div>
                ))}
              </div>
            )}
            {streamingContent && (
              <div className="mt-0.5 text-text whitespace-pre-wrap">
                {streamingContent}
                <StreamingCursor />
              </div>
            )}
          </div>
        )}
      </div>

      {fetchError && !sending && messages.length === 0 && (
        <div className="text-center py-4">
          <p className="text-xs text-red">{fetchError}</p>
        </div>
      )}

      {/* Input */}
      <div className="relative border-t border-border p-3 flex gap-2">
        {showMentions && (() => {
          const filtered = participants.filter((p) => !mentionFilter || p.role.toLowerCase().includes(mentionFilter) || p.name.toLowerCase().includes(mentionFilter));
          if (filtered.length === 0) return null;
          return (
            <div className="absolute bottom-full left-3 mb-1 rounded border border-border bg-bg shadow-lg py-1 min-w-[160px]">
              {filtered.map((p, i) => (
                <button
                  key={p.name}
                  onClick={() => {
                    const atIdx = input.lastIndexOf("@");
                    const before = atIdx >= 0 ? input.slice(0, atIdx) : input;
                    setInput(before + "@" + p.role + " ");
                    setShowMentions(false);
                    inputRef.current?.focus();
                  }}
                  className={`flex w-full items-center gap-2 px-3 py-1.5 text-xs text-left ${i === mentionIdx ? "bg-surface-hover" : "hover:bg-surface-hover"}`}
                >
                  <span className={`font-bold ${ROLE_COLORS[p.role] || "text-text"}`}>{p.role}</span>
                  <span className="text-text-muted">{p.name}</span>
                </button>
              ))}
            </div>
          );
        })()}
        <input
          ref={inputRef}
          type="text"
          value={input}
          onChange={(e) => {
            const val = e.target.value;
            setInput(val);
            const atIdx = val.lastIndexOf("@");
            if (atIdx >= 0 && (atIdx === 0 || val[atIdx - 1] === " ")) {
              const partial = val.slice(atIdx + 1).toLowerCase();
              setMentionFilter(partial);
              setShowMentions(true);
              setMentionIdx(0);
            } else {
              setShowMentions(false);
            }
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") { setShowMentions(false); return; }
            if (showMentions) {
              const filtered = participants.filter((p) => !mentionFilter || p.role.toLowerCase().includes(mentionFilter) || p.name.toLowerCase().includes(mentionFilter));
              if (e.key === "ArrowDown") { e.preventDefault(); setMentionIdx((prev) => Math.min(prev + 1, filtered.length - 1)); return; }
              if (e.key === "ArrowUp") { e.preventDefault(); setMentionIdx((prev) => Math.max(prev - 1, 0)); return; }
              if ((e.key === "Enter" || e.key === "Tab") && filtered.length > 0) {
                e.preventDefault();
                const idx = Math.min(mentionIdx, filtered.length - 1);
                const p = filtered[idx];
                const atIdx = input.lastIndexOf("@");
                const before = atIdx >= 0 ? input.slice(0, atIdx) : input;
                setInput(before + "@" + p.role + " ");
                setShowMentions(false);
                return;
              }
            }
            if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); handleSend(); }
          }}
          className="flex-1 rounded border border-border bg-surface px-3 py-2 text-xs text-text focus:border-accent focus:outline-none"
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
