import { useState, useId, useCallback } from "react";
import type { ToolCall } from "../../lib/chat-events";
import type { ChatPart } from "../../lib/types";
import { RichOutput } from "./RichOutput";
import { openHtmlInNewTab } from "../../lib/dom";

// ── Helpers ──────────────────────────────────────────────────────────────

type OutputStatus = "ok" | "error" | "blocked";

function classifyOutput(output: string | undefined): OutputStatus {
  if (!output) return "ok";
  const lower = output.trimStart().toLowerCase();

  // Explicit errors
  if (lower.startsWith("error:") || lower.startsWith("error -") ||
      lower.startsWith("fatal:") || lower.startsWith("fail")) {
    return "error";
  }

  // JSON responses with "error" key (e.g., API error responses)
  if (lower.startsWith("{") && lower.includes('"error"')) {
    return "error";
  }

  // HTTP error status codes in output
  if (/\b(4\d{2}|5\d{2})\b/.test(lower) && (lower.includes("not found") || lower.includes("internal server") || lower.includes("bad request"))) {
    return "error";
  }

  // Permission / approval / security blocks
  if (lower.includes("requires approval") || lower.includes("command not allowed") ||
      lower.includes("not allowed by security") || lower.includes("permission denied") ||
      lower.includes("not permitted") || lower.includes("access denied") ||
      lower.includes("forbidden") || lower.includes("unauthorized") ||
      lower.includes("haven't granted") || lower.includes("requested permissions")) {
    return "blocked";
  }

  // Connection / timeout failures
  if (lower.startsWith("connect econnrefused") || lower.startsWith("etimedout") ||
      lower.startsWith("enotfound") || lower.startsWith("econnreset")) {
    return "error";
  }

  // Tool use errors (cancelled, errored, etc.)
  if (lower.includes("tool_use_error") || (lower.includes("cancelled") && lower.includes("errored"))) {
    return "error";
  }

  return "ok";
}

/** Check if args represent an HTML canvas tool call */
function isHtmlCanvas(args: Record<string, any> | undefined): args is Record<string, any> & { content: string; content_type: "html" } {
  return args?.content_type === "html" && typeof args?.content === "string";
}

/** For HTML canvas tools: compact metadata + copy/source/output toggles on one line */
function HtmlCanvasArgs({ args, output }: { args: Record<string, any>; output?: string }) {
  const [copied, setCopied] = useState(false);
  const [showSource, setShowSource] = useState(false);
  const [showOutput, setShowOutput] = useState(false);

  // Show all fields except the massive content blob
  const { content, ...meta } = args;
  const hasMeta = Object.keys(meta).length > 0;

  const copyHtml = useCallback(() => {
    navigator.clipboard.writeText(content).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }).catch((err) => {
      console.warn("clipboard write failed:", err);
    });
  }, [content]);

  return (
    <div className="space-y-1">
      {hasMeta && (
        <div>
          <span className="text-text-muted">args: </span>
          <pre className="mt-0.5 overflow-x-auto whitespace-pre-wrap text-[10px] text-text-secondary">
            {formatArgs(meta)}
          </pre>
        </div>
      )}
      <div className="flex items-center gap-2">
        <button
          onClick={copyHtml}
          className="text-[9px] text-text-muted hover:text-text transition-colors"
        >
          {copied ? "copied!" : "copy html"}
        </button>
        <button
          onClick={() => setShowSource(!showSource)}
          className="text-[9px] text-text-muted hover:text-text transition-colors"
        >
          {showSource ? "\u25BE hide source" : "\u25B8 show source"}
        </button>
        {output != null && (
          <button
            onClick={() => setShowOutput(!showOutput)}
            className="text-[9px] text-text-muted hover:text-text transition-colors"
          >
            {showOutput ? "\u25BE hide output" : "\u25B8 show output"}
          </button>
        )}
      </div>
      {showSource && (
        <pre className="mt-0.5 max-h-24 overflow-y-auto overflow-x-auto whitespace-pre-wrap text-[10px] text-text-muted">
          {content}
        </pre>
      )}
      {showOutput && output != null && (
        <div className="mt-0.5">
          <RichOutput text={output} htmlContent={content} />
        </div>
      )}
    </div>
  );
}

/** Full HTML preview with iframe + "open in new tab" button */
function HtmlPreview({ html }: { html: string }) {
  return (
    <div>
      <div className="flex items-center gap-2 mb-1">
        <span className="text-text-muted text-[10px]">preview</span>
        <button
          onClick={() => openHtmlInNewTab(html)}
          className="text-[9px] text-accent hover:underline"
        >
          open in new tab
        </button>
      </div>
      <div className="rounded border border-border overflow-hidden bg-white">
        <iframe
          srcDoc={html}
          sandbox=""
          className="w-full border-0"
          style={{ height: "300px" }}
          title="html preview"
        />
      </div>
    </div>
  );
}

/** Format args JSON with literal newlines rendered inside string values. */
function formatArgs(args: Record<string, any>): string {
  return JSON.stringify(args, null, 2)
    .replace(/\\n/g, "\n")
    .replace(/\\t/g, "\t");
}

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
  const generatedId = useId();
  const panelId = `toolcall-${part.id || generatedId}`;

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
          {(() => {
            if (part.error) return <span className="text-red text-[10px]">FAIL</span>;
            if (part.output == null) return null;
            const status = classifyOutput(part.output);
            if (status === "error") return <span className="text-red text-[10px]">FAIL</span>;
            if (status === "blocked") return <span className="text-yellow text-[10px]">BLOCKED</span>;
            return <span className="text-green text-[10px]">OK</span>;
          })()}
          <span className="text-text-muted text-[10px]">
            {expanded ? "\u25BE" : "\u25B8"}
          </span>
        </span>
      </button>
      {expanded && (
        <div id={panelId} className={`${isOuter ? "border-t border-border/50" : "border-t border-border/30"} px-3 py-2 space-y-1.5 bg-surface/50`}>
          {isHtmlCanvas(part.args) ? (
            <>
              <HtmlCanvasArgs args={part.args} output={part.output ?? undefined} />
              <HtmlPreview html={part.args.content} />
            </>
          ) : (
            <>
              {part.args && Object.keys(part.args).length > 0 && (
                <div>
                  <span className="text-text-muted">args: </span>
                  <pre className="mt-0.5 overflow-x-auto whitespace-pre-wrap text-[10px] text-text-secondary">
                    {formatArgs(part.args)}
                  </pre>
                </div>
              )}
              {part.output != null && <RichOutput text={part.output} />}
            </>
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
  const generatedId = useId();
  const panelId = `toolcall-${tc.toolId || generatedId}`;

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
          {isHtmlCanvas(tc.args) ? (
            <>
              <HtmlCanvasArgs args={tc.args} output={tc.output ?? undefined} />
              <HtmlPreview html={tc.args.content} />
            </>
          ) : (
            <>
              {tc.args && Object.keys(tc.args).length > 0 && (
                <div>
                  <span className="text-text-muted">args: </span>
                  <pre className="mt-0.5 overflow-x-auto whitespace-pre-wrap text-[10px] text-text-secondary">
                    {formatArgs(tc.args)}
                  </pre>
                </div>
              )}
              {tc.output != null && <RichOutput text={tc.output} />}
            </>
          )}
        </div>
      )}
    </div>
  );
}

// ── ToolRunCard ──────────────────────────────────────────────────────────

export function ToolRunCard({ tools }: { tools: ChatPart[] }) {
  const [expanded, setExpanded] = useState(true);
  const generatedId = useId();
  const panelId = `toolrun-${generatedId}`;
  const errorCount = tools.filter((t) => t.error || classifyOutput(t.output) === "error").length;
  const blockedCount = tools.filter((t) => !t.error && classifyOutput(t.output) === "blocked").length;

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
        aria-expanded={expanded}
        aria-controls={panelId}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left"
      >
        <span className="font-mono text-text">{summary}</span>
        <span className="ml-auto flex items-center gap-1.5">
          {errorCount > 0 ? (
            <span className="text-red text-[10px]">{errorCount} FAIL</span>
          ) : blockedCount > 0 ? (
            <span className="text-yellow text-[10px]">{blockedCount} BLOCKED</span>
          ) : (
            <span className="text-green text-[10px]">OK</span>
          )}
          <span className="text-text-muted text-[10px]">
            {expanded ? "\u25BE" : "\u25B8"}
          </span>
        </span>
      </button>
      {expanded && (
        <div id={panelId} className="border-t border-border">
          {tools.map((part, i) => (
            <PartToolCallCard
              key={part.id || `tc-${i}`}
              part={part}
            />
          ))}
        </div>
      )}
    </div>
  );
}
