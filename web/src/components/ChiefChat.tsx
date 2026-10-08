// ChiefChat — the "Chief" target of the top-level chat panel.
//
// Messages here do NOT go through the commander's LLM loop. They are saved
// by the backend (durable store), sent to the chief's wake endpoint, and the
// chief's replies arrive over the bridge. Replies are display only: nothing
// here parses commands from them or approves anything.
//
// The thread is always re-read from GET /api/chief/messages. The SSE stream
// at /api/chief/events only says "something changed".

import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2, RotateCcw, Send } from "lucide-react";
import {
  fetchChiefMessages,
  fetchChiefStatus,
  retryChiefMessage,
  sendChiefMessage,
  subscribeChiefEvents,
  type ChiefMessage,
} from "../lib/api";
import { useAutoScroll } from "../lib/useAutoScroll";
import { SafeMarkdown } from "../lib/safeMarkdown";

const STILL_WAITING_MS = 10 * 60 * 1000;

function elapsed(fromIso: string, now: number): string {
  const s = Math.max(0, Math.floor((now - new Date(fromIso).getTime()) / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

export default function ChiefChat() {
  const [messages, setMessages] = useState<ChiefMessage[]>([]);
  const [status, setStatus] = useState<{ enabled: boolean; wake_configured: boolean } | null>(null);
  const [input, setInput] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [now, setNow] = useState(() => Date.now());
  const { ref: scrollRef } = useAutoScroll([messages]);
  const inputRef = useRef<HTMLTextAreaElement>(null);

  const reload = useCallback(async () => {
    try {
      setMessages(await fetchChiefMessages());
    } catch {
      /* status card explains a disabled bridge */
    }
  }, []);

  useEffect(() => {
    fetchChiefStatus().then(setStatus).catch(() => setStatus({ enabled: false, wake_configured: false }));
    reload();
    const unsub = subscribeChiefEvents(() => reload());
    const tick = setInterval(() => setNow(Date.now()), 1000);
    return () => {
      unsub();
      clearInterval(tick);
    };
  }, [reload]);

  const send = useCallback(async () => {
    const text = input.trim();
    if (!text || sending) return;
    setSending(true);
    setError(null);
    try {
      await sendChiefMessage(text);
      setInput("");
      await reload();
    } catch (e) {
      setError(e instanceof Error ? e.message : "send failed");
    } finally {
      setSending(false);
    }
  }, [input, sending, reload]);

  const retry = useCallback(
    async (id: string) => {
      setError(null);
      try {
        await retryChiefMessage(id);
        await reload();
      } catch (e) {
        setError(e instanceof Error ? e.message : "retry failed");
      }
    },
    [reload],
  );

  const disabled = status !== null && (!status.enabled || !status.wake_configured);

  return (
    <>
      <div ref={scrollRef} className="flex-1 overflow-y-auto px-3 py-3 space-y-3">
        {disabled && (
          <div className="rounded border border-border bg-bg p-2 text-[11px] text-text-muted">
            {status && !status.enabled
              ? "The chief bridge is off. See docs/bridge.md to turn it on."
              : "The chief wake URL and key are not set in ~/.eyrie/bridge.toml."}
          </div>
        )}
        {!disabled && messages.length === 0 && (
          <div className="text-xs text-text-muted">
            Messages here go straight to the chief, the same one you reach from Workbench.
          </div>
        )}
        {messages.map((m) => {
          const finals = (m.replies ?? []).filter((r) => r.final);
          const interim = (m.replies ?? []).filter((r) => !r.final);
          const waiting = m.state === "pending" || m.state === "waiting";
          const stale = waiting && now - new Date(m.ts).getTime() > STILL_WAITING_MS;
          return (
            <div key={m.message_id} className="space-y-1.5">
              <div className="text-[10px] font-medium text-accent">you</div>
              <div className="text-xs text-text whitespace-pre-wrap">{m.text}</div>
              {interim.map((r) => (
                <div key={r.reply_id} className="text-[11px] italic text-text-muted">chief: {r.text}</div>
              ))}
              {m.state === "pending" && (
                <div className="flex items-center gap-1.5 text-[10px] text-text-muted">
                  <Loader2 className="h-3 w-3 animate-spin" /> sending{m.attempts > 0 ? ` (attempt ${m.attempts + 1})` : ""}
                </div>
              )}
              {m.state === "waiting" && !stale && (
                <div className="flex items-center gap-1.5 text-[10px] text-text-muted">
                  <Loader2 className="h-3 w-3 animate-spin" /> Waiting on chief · {elapsed(m.ts, now)}
                </div>
              )}
              {stale && (
                <div className="flex items-center gap-2 text-[10px] text-yellow">
                  Still waiting on chief · {elapsed(m.ts, now)}
                  <button onClick={() => retry(m.message_id)} className="flex items-center gap-1 underline">
                    <RotateCcw className="h-3 w-3" /> Retry
                  </button>
                </div>
              )}
              {m.state === "failed" && (
                <div className="flex items-center gap-2 text-[10px] text-red">
                  Couldn't reach the chief{m.last_error ? ` (${m.last_error})` : ""}.
                  <button onClick={() => retry(m.message_id)} className="flex items-center gap-1 underline">
                    <RotateCcw className="h-3 w-3" /> Retry
                  </button>
                </div>
              )}
              {finals.map((r) => (
                <div key={r.reply_id}>
                  <div className="text-[10px] font-medium text-purple">chief</div>
                  <SafeMarkdown text={r.text} />
                </div>
              ))}
            </div>
          );
        })}
      </div>
      {error && <div className="border-t border-border px-3 py-1 text-[10px] text-red">{error}</div>}
      <div className="border-t border-border p-2">
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
            placeholder={disabled ? "chief bridge not configured" : "message the chief…"}
            disabled={disabled || sending}
            rows={2}
            className="flex-1 resize-none rounded border border-border bg-bg px-2 py-1.5 text-xs text-text placeholder:text-text-muted focus:outline-none focus:border-accent disabled:opacity-50"
          />
          <button
            onClick={send}
            disabled={disabled || sending || !input.trim()}
            className="rounded bg-accent p-2 text-white disabled:opacity-40"
            title="send to chief"
          >
            {sending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />}
          </button>
        </div>
      </div>
    </>
  );
}
