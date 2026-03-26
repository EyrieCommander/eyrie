import { useState } from "react";
import type { ToolCall } from "../../lib/chat-events";
import type { ChatPart } from "../../lib/types";

// ── Helpers ──────────────────────────────────────────────────────────────

export function toolCallSummary(
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

// ── PartToolCallCard ─────────────────────────────────────────────────────

export interface PartToolCallCardProps {
  part: ChatPart;
  defaultExpanded?: boolean;
  /** "outer" uses the bolder top-level card styling (for single-tool runs) */
  headerStyle?: "inner" | "outer";
}

export function PartToolCallCard({
  part,
  defaultExpanded = false,
  headerStyle = "inner",
}: PartToolCallCardProps) {
  const [expanded, setExpanded] = useState(defaultExpanded);

  const isOuter = headerStyle === "outer";
  const panelId = `toolcall-${part.id || "panel"}`;

  return (
    <div className={isOuter ? "text-[11px]" : "border-b border-border/30 last:border-b-0 text-[11px]"}>
      <button
        onClick={(e) => {
          e.stopPropagation();
          setExpanded(!expanded);
        }}
        aria-expanded={expanded}
        aria-controls={panelId}
        className={isOuter
          ? "flex w-full items-center gap-2 px-3 py-1.5 text-left"
          : "flex w-full items-center gap-2 px-3 py-1 text-left hover:bg-surface-hover/30"
        }
      >
        <span className={isOuter ? "font-mono text-text" : "font-mono text-text-secondary"}>{part.name}</span>
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
            {expanded ? "\u25BE" : "\u25B8"}
          </span>
        </span>
      </button>
      {expanded && (
        <div id={panelId} className={`${isOuter ? "border-t border-border/50" : "border-t border-border/30"} px-3 py-2 space-y-1.5 bg-surface/50`}>
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

// ── ToolCallCard (streaming) ─────────────────────────────────────────────

export interface ToolCallCardProps {
  tc: ToolCall;
}

export function ToolCallCard({ tc }: ToolCallCardProps) {
  const [expanded, setExpanded] = useState(false);
  const panelId = `toolcall-${tc.toolId || "panel"}`;

  return (
    <div className="my-1.5 ml-4 rounded border border-border bg-surface-hover/30 text-[11px] overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
        aria-controls={panelId}
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
            {expanded ? "\u25BE" : "\u25B8"}
          </span>
        </span>
      </button>
      {expanded && (
        <div id={panelId} className="border-t border-border/50 px-3 py-2 space-y-1.5">
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

// ── ToolRunCard ──────────────────────────────────────────────────────────

export function ToolRunCard({ tools }: { tools: ChatPart[] }) {
  const [expanded, setExpanded] = useState(true);
  const failCount = tools.filter((t) => t.error).length;

  // Single tool: render one PartToolCallCard directly (no outer header)
  // to avoid showing the tool name twice.
  if (tools.length === 1) {
    return (
      <div className="my-1.5 ml-4 rounded border border-border bg-surface-hover/30 text-[11px] overflow-hidden">
        <PartToolCallCard
          part={tools[0]}
          defaultExpanded
          headerStyle="outer"
        />
      </div>
    );
  }

  const names = tools.map((t) => t.name).filter(Boolean);
  const uniqueNames = [...new Set(names)];
  const summary =
    `${tools.length} tools` +
    (uniqueNames.length <= 3
      ? `: ${uniqueNames.join(", ")}`
      : "");

  return (
    <div className="my-1.5 ml-4 rounded border border-border bg-surface-hover/30 text-[11px] overflow-hidden">
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
            {expanded ? "\u25BE" : "\u25B8"}
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
