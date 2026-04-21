// Commander chat panel — persistent right-side panel for talking to Eyrie.
//
// Visible on all routes. Expand/collapse state persists via localStorage.
// Streams from POST /api/commander/chat (SSE), rehydrates from
// GET /api/commander/history on mount. Tool-call events render inline
// as collapsible cards. Confirm-tier tool calls pause the turn and show
// an approve/deny widget. Context-window usage bar updates from each
// `done` event. Memory drawer lists entries from GET /api/commander/memory.

import { useCallback, useEffect, useRef, useState } from "react";
import {
  MessageSquare,
  PanelRightClose,
  PanelRightOpen,
  Send,
  Loader2,
  Brain,
  ChevronDown,
  ChevronRight,
  Trash2,
  Check,
  X,
} from "lucide-react";
import {
  streamCommanderChat,
  confirmCommanderAction,
  fetchCommanderHistory,
  clearCommanderHistory,
  fetchCommanderMemory,
} from "../lib/api";
import type {
  CommanderEvent,
  CommanderHistoryMessage,
  MemoryEntry,
} from "../lib/types";
import { useAutoScroll } from "../lib/useAutoScroll";
import { KEYS_CHANGED_EVENT, COMMANDER_PREFILL_EVENT } from "../lib/events";

// ── Types ────────────────────────────────────────────────────────────

interface ToolCallEntry {
  id: string;
  name: string;
  args: Record<string, unknown>;
  output?: string;
  error?: boolean;
  done: boolean;
}

interface ConfirmEntry {
  id: string;
  tool: string;
  args: Record<string, unknown>;
  summary: string;
  status: "pending" | "approved" | "denied";
}

/** A rendered chat item — either a full message or a streaming delta. */
interface ChatItem {
  role: "user" | "assistant";
  content: string;
  toolCalls?: ToolCallEntry[];
  confirmations?: ConfirmEntry[];
}

interface ContextUsage {
  tokens: number;
  window: number;
}

// ── Component ────────────────────────────────────────────────────────

const STORAGE_KEY = "eyrie-commander-chat-expanded";

interface Props {
  /** Current onboarding phase — drives context-aware greeting + chips. */
  phase?: string;
}

export default function CommanderChat({ phase }: Props) {
  const [expanded, setExpanded] = useState(() => {
    const saved = localStorage.getItem(STORAGE_KEY);
    return saved === null ? true : saved === "true";
  });
  const [items, setItems] = useState<ChatItem[]>([]);
  const [input, setInput] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [contextUsage, setContextUsage] = useState<ContextUsage | null>(null);
  const [memories, setMemories] = useState<MemoryEntry[]>([]);
  const [memoryOpen, setMemoryOpen] = useState(false);
  const [unavailable, setUnavailable] = useState(false);

  const scrollRef = useAutoScroll([items]);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const controllerRef = useRef<AbortController | null>(null);

  useEffect(() => {
    localStorage.setItem(STORAGE_KEY, String(expanded));
  }, [expanded]);

  // ── Shared rehydration logic ──────────────────────────────────────

  /** Fetch history + memory and update state. Shared across mount,
   *  keys-changed, and health-poll paths. */
  const rehydrate = useCallback(async () => {
    const history = await fetchCommanderHistory();
    setUnavailable(false);
    if (history.length > 0) {
      setItems(history.map((m: CommanderHistoryMessage) => ({
        role: m.role as "user" | "assistant",
        content: m.content,
      })));
    }
    fetchCommanderMemory().then(setMemories).catch(() => {});
  }, []);

  useEffect(() => {
    let cancelled = false;
    rehydrate().catch(() => { if (!cancelled) setUnavailable(true); });
    return () => { cancelled = true; };
  }, [rehydrate]);

  // Immediately re-check when keys change (add or delete from any page)
  useEffect(() => {
    const handler = () => {
      rehydrate().catch(() => setUnavailable(true));
    };
    window.addEventListener(KEYS_CHANGED_EVENT, handler);
    return () => window.removeEventListener(KEYS_CHANGED_EVENT, handler);
  }, [rehydrate]);

  // Continuous health poll — fast (5s) while unavailable, slow (15s) when up.
  // Detects both key addition (becomes available) and key deletion (goes offline).
  useEffect(() => {
    const interval = unavailable ? 5_000 : 15_000;
    const id = setInterval(async () => {
      try {
        if (unavailable) {
          await rehydrate();
        } else {
          await fetchCommanderHistory();
        }
      } catch {
        if (!unavailable) setUnavailable(true);
      }
    }, interval);
    return () => clearInterval(id);
  }, [unavailable, rehydrate]);

  // ── SSE event handler ──────────────────────────────────────────────

  const handleEvent = useCallback((event: CommanderEvent) => {
    switch (event.type) {
      case "delta":
        setItems((prev) => {
          const copy = [...prev];
          const last = copy[copy.length - 1];
          if (last?.role === "assistant") {
            copy[copy.length - 1] = { ...last, content: last.content + event.text };
          } else {
            copy.push({ role: "assistant", content: event.text });
          }
          return copy;
        });
        break;

      case "tool_call":
        setItems((prev) => {
          const copy = [...prev];
          const last = copy[copy.length - 1];
          if (last?.role === "assistant") {
            const calls = [...(last.toolCalls || [])];
            calls.push({ id: event.id, name: event.name, args: event.args, done: false });
            copy[copy.length - 1] = { ...last, toolCalls: calls };
          }
          return copy;
        });
        break;

      case "tool_result":
        setItems((prev) => {
          const copy = [...prev];
          const last = copy[copy.length - 1];
          if (last?.role === "assistant" && last.toolCalls) {
            const calls = last.toolCalls.map((tc) =>
              tc.id === event.id
                ? { ...tc, output: event.output, error: event.error, done: true }
                : tc,
            );
            copy[copy.length - 1] = { ...last, toolCalls: calls };
          }
          return copy;
        });
        break;

      case "confirm_required":
        setItems((prev) => {
          const copy = [...prev];
          const last = copy[copy.length - 1];
          if (last?.role === "assistant") {
            const confs = [...(last.confirmations || [])];
            confs.push({
              id: event.id,
              tool: event.tool,
              args: event.args,
              summary: event.summary,
              status: "pending",
            });
            copy[copy.length - 1] = { ...last, confirmations: confs };
          }
          return copy;
        });
        break;

      case "message":
        // Finalize — replace accumulated deltas with complete content
        setItems((prev) => {
          const copy = [...prev];
          const last = copy[copy.length - 1];
          if (last?.role === "assistant") {
            copy[copy.length - 1] = { ...last, content: event.content };
          }
          return copy;
        });
        break;

      case "done":
        setStreaming(false);
        if (event.context_tokens && event.context_window) {
          setContextUsage({ tokens: event.context_tokens, window: event.context_window });
        }
        // Refresh memory (the commander may have used remember/forget)
        fetchCommanderMemory().then(setMemories).catch(() => {});
        break;

      case "error":
        setStreaming(false);
        setItems((prev) => {
          const copy = [...prev];
          const last = copy[copy.length - 1];
          if (last?.role === "assistant") {
            copy[copy.length - 1] = {
              ...last,
              content: last.content + (last.content ? "\n\n" : "") + `**Error:** ${event.error}`,
            };
          } else {
            copy.push({ role: "assistant", content: `**Error:** ${event.error}` });
          }
          return copy;
        });
        break;
    }
  }, []);

  // ── Actions ────────────────────────────────────────────────────────

  const send = useCallback(() => {
    const msg = input.trim();
    if (!msg || streaming) return;
    setInput("");
    setItems((prev) => [...prev, { role: "user", content: msg }]);
    setStreaming(true);
    controllerRef.current = streamCommanderChat(msg, handleEvent);
  }, [input, streaming, handleEvent]);

  const handleConfirm = useCallback((id: string, approved: boolean) => {
    // Update the confirmation entry status
    setItems((prev) =>
      prev.map((item) => {
        if (item.confirmations) {
          const confs = item.confirmations.map((c) =>
            c.id === id ? { ...c, status: approved ? "approved" as const : "denied" as const } : c,
          );
          return { ...item, confirmations: confs };
        }
        return item;
      }),
    );
    // Stream the continuation turn
    setStreaming(true);
    controllerRef.current = confirmCommanderAction(id, approved, handleEvent);
  }, [handleEvent]);

  const handleClear = useCallback(async () => {
    try {
      await clearCommanderHistory();
      setItems([]);
      setContextUsage(null);
    } catch { /* silent */ }
  }, []);

  const stop = useCallback(() => {
    controllerRef.current?.abort();
    setStreaming(false);
  }, []);

  /** Pre-fill the chat input (used by "ask commander" CTAs). */
  const prefill = useCallback((text: string) => {
    setInput(text);
    if (!expanded) setExpanded(true);
    requestAnimationFrame(() => inputRef.current?.focus());
  }, [expanded]);

  // Listen for prefill events dispatched by other components (avoids
  // prop-drilling through every layer).
  useEffect(() => {
    const handler = (e: Event) => {
      const detail = (e as CustomEvent<string>).detail;
      if (detail) prefill(detail);
    };
    window.addEventListener(COMMANDER_PREFILL_EVENT, handler);
    return () => window.removeEventListener(COMMANDER_PREFILL_EVENT, handler);
  }, [prefill]);

  // ── Collapsed strip ────────────────────────────────────────────────

  if (!expanded) {
    return (
      <div
        onClick={() => setExpanded(true)}
        className="flex w-11 shrink-0 cursor-pointer flex-col items-center gap-2 border-l border-border bg-surface py-4 hover:bg-surface-hover transition-colors"
        title="expand commander chat"
      >
        <PanelRightOpen className="h-4 w-4 text-text-muted" />
        <span className="text-[9px] text-text-muted [writing-mode:vertical-rl] rotate-180">
          commander
        </span>
      </div>
    );
  }

  // ── Context-aware chips ────────────────────────────────────────────

  const chips = getChips(phase);

  // ── Expanded panel ─────────────────────────────────────────────────

  const usagePct = contextUsage
    ? Math.round((contextUsage.tokens / contextUsage.window) * 100)
    : null;

  return (
    <div className="flex w-[380px] shrink-0 flex-col border-l border-border bg-surface">
      {/* Header */}
      <div className="flex items-center gap-2 border-b border-border px-3 py-2">
        <MessageSquare className="h-3.5 w-3.5 text-purple" />
        <span className="text-xs font-semibold text-text flex-1">commander</span>

        {/* Memory badge */}
        {memories.length > 0 && (
          <button
            onClick={() => setMemoryOpen(!memoryOpen)}
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] text-text-muted hover:bg-surface-hover transition-colors"
            title="commander memory"
          >
            <Brain className="h-3 w-3" />
            {memories.length}
          </button>
        )}

        {/* Context usage bar */}
        {usagePct !== null && (
          <div
            className="h-1.5 w-12 rounded-full bg-border overflow-hidden"
            title={`${contextUsage!.tokens.toLocaleString()} / ${contextUsage!.window.toLocaleString()} tokens (${usagePct}%)`}
          >
            <div
              className={`h-full rounded-full transition-all ${
                usagePct > 80 ? "bg-red" : usagePct > 50 ? "bg-yellow" : "bg-green"
              }`}
              style={{ width: `${Math.min(usagePct, 100)}%` }}
            />
          </div>
        )}

        {/* Clear history */}
        <button
          onClick={handleClear}
          className="text-text-muted hover:text-red transition-colors"
          title="clear chat history"
        >
          <Trash2 className="h-3 w-3" />
        </button>

        {/* Collapse */}
        <button
          onClick={() => setExpanded(false)}
          className="text-text-muted hover:text-text transition-colors"
          title="collapse panel"
        >
          <PanelRightClose className="h-3.5 w-3.5" />
        </button>
      </div>

      {/* Memory drawer */}
      {memoryOpen && memories.length > 0 && (
        <div className="border-b border-border bg-bg px-3 py-2 max-h-48 overflow-y-auto">
          <div className="text-[10px] font-medium uppercase tracking-wider text-text-muted mb-1">
            memory ({memories.length})
          </div>
          <div className="space-y-1">
            {memories.map((m) => (
              <div key={m.key} className="text-[10px]">
                <span className="font-medium text-purple">{m.key}</span>
                <span className="text-text-muted">: </span>
                <span className="text-text-secondary">{m.value}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Unavailable — nudge toward Phase 0 setup */}
      {unavailable && (
        <div className="border-b border-border bg-bg px-3 py-2 text-[10px] text-text-muted">
          Add an API key in the <span className="font-medium text-text">commander</span> phase to get started.
        </div>
      )}

      {/* Messages */}
      <div ref={scrollRef} className="flex-1 overflow-y-auto px-3 py-3 space-y-3">
        {/* Greeting when empty */}
        {items.length === 0 && !unavailable && (
          <div className="text-xs text-text-muted space-y-2">
            <p>
              Welcome to Eyrie! I'll be your commander — I can help you
              pick a framework, walk through installs, or explain any of the steps.
            </p>
            <p>
              If you're not sure where to start, ZeroClaw is a solid default:
              fast, sandboxed, and works with OpenRouter.
            </p>
          </div>
        )}

        {items.map((item, i) => (
          <div key={i}>
            {/* Role label */}
            <div className={`text-[10px] font-medium mb-0.5 ${
              item.role === "user" ? "text-accent" : "text-purple"
            }`}>
              {item.role === "user" ? "you" : "commander"}
            </div>

            {/* Content */}
            <div className="text-xs text-text whitespace-pre-wrap">{item.content}</div>

            {/* Tool calls */}
            {item.toolCalls?.map((tc) => (
              <ToolCallCard key={tc.id} tc={tc} />
            ))}

            {/* Confirmations */}
            {item.confirmations?.map((c) => (
              <ConfirmCard key={c.id} entry={c} onConfirm={handleConfirm} />
            ))}
          </div>
        ))}

        {/* Streaming indicator */}
        {streaming && (
          <div className="flex items-center gap-2 text-[10px] text-text-muted">
            <Loader2 className="h-3 w-3 animate-spin" />
            <span>thinking...</span>
            <button onClick={stop} className="text-text-muted hover:text-red transition-colors">
              stop
            </button>
          </div>
        )}
      </div>

      {/* Suggestion chips (hidden when unavailable) */}
      {!unavailable && items.length === 0 && chips.length > 0 && (
        <div className="flex flex-wrap gap-1.5 px-3 pb-2">
          {chips.map((chip) => (
            <button
              key={chip}
              onClick={() => { setInput(chip); inputRef.current?.focus(); }}
              className="rounded-full border border-purple/30 bg-purple/5 px-2.5 py-1 text-[10px] text-purple hover:bg-purple/10 transition-colors"
            >
              {chip}
            </button>
          ))}
        </div>
      )}

      {/* Input */}
      <div className="border-t border-border px-3 py-2">
        <div className="flex items-end gap-2">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                send();
              }
            }}
            disabled={unavailable}
            placeholder={unavailable ? "set up an API key first" : "ask the commander..."}
            rows={1}
            className="flex-1 resize-none rounded border border-border bg-bg px-2 py-1.5 text-xs text-text placeholder:text-text-muted focus:border-accent focus:outline-none disabled:opacity-40 disabled:cursor-not-allowed"
          />
          <button
            onClick={send}
            disabled={unavailable || !input.trim() || streaming}
            className="rounded bg-purple px-2 py-1.5 text-white transition-colors hover:bg-purple/80 disabled:opacity-30"
          >
            <Send className="h-3 w-3" />
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Sub-components ───────────────────────────────────────────────────

function ToolCallCard({ tc }: { tc: ToolCallEntry }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="mt-1 rounded border border-border bg-bg text-[10px]">
      <button
        onClick={() => setOpen(!open)}
        className="flex w-full items-center gap-1.5 px-2 py-1 text-left"
      >
        {tc.done ? (
          tc.error ? (
            <X className="h-3 w-3 text-red shrink-0" />
          ) : (
            <Check className="h-3 w-3 text-green shrink-0" />
          )
        ) : (
          <Loader2 className="h-3 w-3 animate-spin text-blue shrink-0" />
        )}
        <span className="font-medium text-text flex-1 truncate">{tc.name}</span>
        {open ? <ChevronDown className="h-3 w-3 text-text-muted" /> : <ChevronRight className="h-3 w-3 text-text-muted" />}
      </button>
      {open && (
        <div className="border-t border-border px-2 py-1 space-y-1">
          <div className="text-text-muted">
            args: <code className="text-text-secondary">{JSON.stringify(tc.args)}</code>
          </div>
          {tc.output && (
            <div className="text-text-muted">
              result: <code className="text-text-secondary whitespace-pre-wrap break-all">{tc.output.length > 500 ? tc.output.slice(0, 500) + "..." : tc.output}</code>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ConfirmCard({
  entry,
  onConfirm,
}: {
  entry: ConfirmEntry;
  onConfirm: (id: string, approved: boolean) => void;
}) {
  const resolved = entry.status !== "pending";
  return (
    <div className={`mt-1 rounded border px-3 py-2 text-xs ${
      resolved
        ? entry.status === "approved"
          ? "border-green/30 bg-green/5"
          : "border-red/30 bg-red/5"
        : "border-yellow/30 bg-yellow/5"
    }`}>
      <div className="font-medium text-text">{entry.summary}</div>
      <div className="mt-0.5 text-[10px] text-text-muted">
        tool: {entry.tool}
      </div>
      {resolved ? (
        <div className={`mt-1 text-[10px] font-medium ${
          entry.status === "approved" ? "text-green" : "text-red"
        }`}>
          {entry.status === "approved" ? "approved" : "denied"}
        </div>
      ) : (
        <div className="mt-2 flex items-center gap-2">
          <button
            onClick={() => onConfirm(entry.id, true)}
            className="rounded bg-green/10 px-2.5 py-1 text-[10px] font-medium text-green hover:bg-green/20 transition-colors"
          >
            approve
          </button>
          <button
            onClick={() => onConfirm(entry.id, false)}
            className="rounded border border-red/30 px-2.5 py-1 text-[10px] font-medium text-red hover:bg-red/5 transition-colors"
          >
            deny
          </button>
        </div>
      )}
    </div>
  );
}

// ── Context-aware chips ──────────────────────────────────────────────

function getChips(phase?: string): string[] {
  switch (phase) {
    case "commander":
      return ["what can you do?", "what frameworks are available?"];
    case "frameworks":
      return ["what framework should I pick?", "what's an API key?", "help me install"];
    case "projects":
      return ["help me scope this project", "suggest talons for this goal", "what should the captain do?"];
    default:
      return ["what can you do?", "what's an API key?"];
  }
}
