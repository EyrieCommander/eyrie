import { useState, useEffect, useCallback, useRef } from "react";
import { Send } from "lucide-react";
import type { ProjectChatMessage, ChatPart } from "../lib/types";
import { PartToolCallCard, StreamingCursor } from "./ChatPanel";
import { fetchProjectChat, streamProjectChat, streamCaptainBriefing } from "../lib/api";

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
    fetchProjectChat(projectId).then(setMessages).catch(() => {});
    const interval = setInterval(() => {
      if (sending) return;
      fetchProjectChat(projectId).then((msgs) => {
        setMessages((prev) => {
          if (msgs.length !== prev.length) return msgs;
          return prev;
        });
      }).catch(() => {});
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

  const handleSend = useCallback(() => {
    const msg = input.trim();
    if (!msg || sending) return;
    setInput("");
    setSending(true);
    setChatError("");
    setStreamingContent("");
    setStreamingSender(null);
    setStreamingToolCalls([]);

    const controller = streamProjectChat(projectId, msg, (event) => {
      if (event.type === "message" && event.message) {
        setMessages((prev) => [...prev, event.message!]);
        setStreamingContent("");
        setStreamingSender(null);
        setStreamingRole(null);
        setStreamingToolCalls([]);
      } else if (event.type === "agent_event" && event.event) {
        if (event.event.type === "delta") {
          setStreamingSender(event.sender || null);
          setStreamingRole(event.role || null);
          setStreamingContent((prev) => prev + (event.event!.content || ""));
        } else if (event.event.type === "tool_start") {
          setStreamingSender(event.sender || null);
          setStreamingRole(event.role || null);
          setStreamingToolCalls((prev) => [...prev, {
            type: "tool_call",
            id: event.event!.tool_id,
            name: event.event!.tool,
          }]);
        } else if (event.event.type === "tool_result") {
          setStreamingToolCalls((prev) => {
            const updated = [...prev];
            for (let i = updated.length - 1; i >= 0; i--) {
              if ((event.event!.tool_id && updated[i].id === event.event!.tool_id) ||
                  (!event.event!.tool_id && updated[i].name === event.event!.tool && !updated[i].output)) {
                updated[i] = { ...updated[i], output: event.event!.output };
                break;
              }
            }
            return updated;
          });
        } else if (event.event.type === "error") {
          const agentName = event.sender || "agent";
          const errContent = event.event.content || "unknown error";
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
  }, [input, sending, projectId]);

  const startProjectChat = useCallback(() => {
    setSending(true);
    setChatError("");
    // Brief the captain first (fetches API ref, saves TOOLS.md), then start the chat
    const { sessionReady } = streamCaptainBriefing(projectId, () => {});
    sessionReady.then(() => {
      if (!mountedRef.current) return;
      const controller = streamProjectChat(projectId, "Let's get started on this project.", (event) => {
        if (!mountedRef.current) return;
        if (event.type === "message" && event.message) {
          setMessages((prev) => [...prev, event.message!]);
        } else if (event.type === "done") {
          setSending(false);
        } else if (event.type === "error") {
          setSending(false);
          setChatError(event.error || "failed to start chat");
        }
      });
      streamControllerRef.current = controller;
    }).catch((err) => {
      console.error("Captain briefing failed:", err);
      if (!mountedRef.current) return;
      setSending(false);
      setChatError("failed to brief captain");
    });
  }, [projectId]);

  return (
    <div className="flex flex-col resize-y overflow-hidden" style={{ height: "calc(100vh - 300px)", minHeight: "300px", maxHeight: "calc(100vh - 120px)" }}>
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
              onClick={startProjectChat}
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
            <span className={`font-bold ${ROLE_COLORS["commander"]}`}>agent</span>{" "}
            thinking...
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

      {/* Input */}
      <div className="relative border-t border-border p-3 flex gap-2">
        {showMentions && (() => {
          const filtered = participants.filter((p) => !mentionFilter || p.role.includes(mentionFilter) || p.name.includes(mentionFilter));
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
              const filtered = participants.filter((p) => !mentionFilter || p.role.includes(mentionFilter) || p.name.includes(mentionFilter));
              if (e.key === "ArrowDown") { e.preventDefault(); setMentionIdx((prev) => Math.min(prev + 1, filtered.length - 1)); return; }
              if (e.key === "ArrowUp") { e.preventDefault(); setMentionIdx((prev) => Math.max(prev - 1, 0)); return; }
              if ((e.key === "Enter" || e.key === "Tab") && filtered.length > 0) {
                e.preventDefault();
                const p = filtered[mentionIdx];
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
