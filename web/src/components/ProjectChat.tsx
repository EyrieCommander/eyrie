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
  const [streamingText, setStreamingText] = useState("");
  const [streamingAgent, setStreamingAgent] = useState("");
  const [streamingRole, setStreamingRole] = useState("");
  const [streamingTools, setStreamingTools] = useState<{ name: string; done: boolean }[]>([]);
  const [showMentions, setShowMentions] = useState(false);
  const [mentionFilter, setMentionFilter] = useState("");
  const [mentionIdx, setMentionIdx] = useState(0);
  const scrollRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  // Load messages on mount
  useEffect(() => {
    fetchProjectChat(projectId).then(setMessages).catch(console.error);
  }, [projectId]);

  // Poll for new messages when idle (not sending)
  useEffect(() => {
    if (sending) return;
    const id = setInterval(() => {
      fetchProjectChat(projectId).then((msgs) => {
        setMessages((prev) => msgs.length !== prev.length ? msgs : prev);
      }).catch(() => {});
    }, 4000);
    return () => clearInterval(id);
  }, [projectId, sending]);

  // Auto-scroll
  useEffect(() => {
    scrollRef.current?.scrollTo(0, scrollRef.current.scrollHeight);
  }, [messages, streamingText]);

  useEffect(() => () => { abortRef.current?.abort(); }, []);

  // Send a message
  const send = useCallback((text: string) => {
    if (!text || sending) return;
    setSending(true);
    setChatError("");
    setStreamingText("");
    setStreamingAgent("");

    const ctrl = streamProjectChat(projectId, text, (event) => {

      switch (event.type) {
        case "message":
          if (event.message) {
            setMessages((prev) => [...prev, event.message!]);
          }
          break;

        case "agent_event":
          if (event.event) {
            const ev = event.event;
            if (ev.type === "delta") {
              setStreamingAgent(event.sender || "");
              setStreamingRole(event.role || "");
              setStreamingText((p) => p + (ev.content || ""));
            } else if (ev.type === "tool_start") {
              setStreamingAgent(event.sender || "");
              setStreamingRole(event.role || "");
              setStreamingTools((p) => [...p, { name: ev.tool || "tool", done: false }]);
            } else if (ev.type === "tool_result") {
              setStreamingTools((p) => {
                const updated = [...p];
                for (let i = updated.length - 1; i >= 0; i--) {
                  if (!updated[i].done) { updated[i] = { ...updated[i], done: true }; break; }
                }
                return updated;
              });
            } else if (ev.type === "done" && ev.content) {
              setStreamingText("");
              setStreamingAgent("");
              setStreamingTools([]);
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
          setSending(false);
          setStreamingText("");
          setStreamingAgent("");
          setStreamingTools([]);
          // Final sync
          fetchProjectChat(projectId).then(setMessages).catch(() => {});
          break;

        case "error":
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

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div ref={scrollRef} className="flex-1 overflow-y-auto space-y-3 p-4">
        {/* Empty state */}
        {messages.length === 0 && !sending && (
          <div className="text-center py-10 space-y-4">
            <p className="text-xs text-text-muted">start the project chat to bring the team together</p>
            {chatError && (
              <div className="rounded border border-red/30 bg-red/5 px-4 py-2 text-xs text-red max-w-sm mx-auto">{chatError}</div>
            )}
            <button
              onClick={() => send("Let's get started on this project.")}
              disabled={sending}
              className="rounded bg-accent px-4 py-2 text-xs font-medium text-white hover:bg-accent/80 disabled:opacity-50"
            >
              start project chat
            </button>
          </div>
        )}

        {/* Messages — sort system messages before user messages at the same timestamp */}
        {[...messages].sort((a, b) => {
          // System messages go before user messages when timestamps are within 1 second
          const ta = new Date(a.timestamp).getTime();
          const tb = new Date(b.timestamp).getTime();
          if (Math.abs(ta - tb) < 1000) {
            if (a.role === "system" && b.role === "user") return -1;
            if (a.role === "user" && b.role === "system") return 1;
          }
          return ta - tb;
        }).map((msg) => {
          const parts = msg.parts ?? [];
          const toolParts = parts.filter((p) => p.type === "tool_call");
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
                {toolParts.length > 0 && (
                  <span className="text-[10px] text-accent/60">[{toolParts.length} tool{toolParts.length > 1 ? "s" : ""}]</span>
                )}
              </div>
              {toolParts.length > 0 && (
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

        {/* Streaming indicator */}
        {sending && streamingAgent && (
          <div className="text-xs">
            <div className="flex items-baseline gap-2">
              <span className={`font-bold ${ROLE_COLORS[streamingRole] || "text-text"}`}>
                {streamingRole || "agent"}
              </span>
              <span className="text-[10px] text-text-muted">({streamingAgent})</span>
              {!streamingText && streamingTools.length === 0 && (
                <span className="text-[10px] text-text-muted animate-pulse">thinking...</span>
              )}
            </div>
            {streamingTools.length > 0 && (
              <div className="mt-1 space-y-1">
                {streamingTools.map((tc, i) => (
                  <div key={i} className="flex items-center gap-2 rounded border border-border bg-surface px-2 py-1">
                    <span className="text-accent text-[10px] font-mono">{tc.name}</span>
                    {tc.done
                      ? <span className="text-[10px] text-green">OK</span>
                      : <span className="text-[10px] text-text-muted animate-pulse">running...</span>
                    }
                  </div>
                ))}
              </div>
            )}
            {streamingText && (
              <div className="mt-0.5 text-text whitespace-pre-wrap">
                {streamingText}<StreamingCursor />
              </div>
            )}
          </div>
        )}

        {/* Waiting indicator (before any agent starts) */}
        {sending && !streamingAgent && messages.length > 0 && (
          <div className="text-xs py-1 flex items-center gap-2 text-text-muted">
            <span className="h-1 w-1 rounded-full bg-accent animate-pulse" />
            waiting for agent response...
          </div>
        )}
      </div>

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
                    setInput((atIdx >= 0 ? input.slice(0, atIdx) : input) + "@" + p.role + " ");
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
              const filtered = participants.filter((p) => !mentionFilter || p.role.toLowerCase().includes(mentionFilter) || p.name.toLowerCase().includes(mentionFilter));
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
