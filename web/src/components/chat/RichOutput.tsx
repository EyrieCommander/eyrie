// RichOutput.tsx — Smart rendering for tool call outputs.
//
// WHY a single component instead of per-feature detection:
//   Tool outputs are plain strings. We need to detect what kind of content
//   they represent and render accordingly. A single component that runs
//   detection in priority order (JSON > diff > enhanced text) avoids
//   multiple parsing passes and ensures only one renderer wins.
//
// Detection priority:
//   1. JSON (valid object/array) → syntax-highlighted, collapsible
//   2. Unified diff (@@, +/- lines) → color-coded additions/deletions
//   3. Plain text → URLs linkified, images inlined, canvas frames linked,
//      file paths highlighted

import type React from "react";
import { useState } from "react";
import { openHtmlInNewTab } from "../../lib/dom";

// ── Detection ────────────────────────────────────────────────────────────

function tryParseJson(text: string): unknown | null {
  const trimmed = text.trim();
  if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) return null;
  try {
    const parsed = JSON.parse(trimmed);
    if (typeof parsed === "object" && parsed !== null) return parsed;
    return null;
  } catch {
    return null;
  }
}

/** Heuristic: looks like a unified diff if it has hunk headers and +/- lines. */
function isDiff(text: string): boolean {
  const lines = text.split("\n");
  const hasHunk = lines.some((l) => l.startsWith("@@ "));
  const hasPlusMinus =
    lines.some((l) => l.startsWith("+") && !l.startsWith("+++")) &&
    lines.some((l) => l.startsWith("-") && !l.startsWith("---"));
  return hasHunk && hasPlusMinus;
}

// ── JSON Renderer ────────────────────────────────────────────────────────

// WHY dangerouslySetInnerHTML: JSON syntax highlighting requires wrapping
// individual tokens in <span> tags. The input is our own JSON.stringify
// output (not user HTML), so there's no injection risk.
function JsonView({ data }: { data: unknown }) {
  const [collapsed, setCollapsed] = useState(false);
  const text = JSON.stringify(data, null, 2);
  const lineCount = text.split("\n").length;

  // Simple syntax highlighting via regex replacement on our own stringify output.
  // Order matters: keys first (before their values match the string pattern).
  const highlighted = text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(
      /("(?:[^"\\]|\\.)*")\s*:/g,
      '<span class="text-purple-400">$1</span>:',
    )
    .replace(
      /:\s*("(?:[^"\\]|\\.)*")/g,
      ': <span class="text-accent">$1</span>',
    )
    .replace(
      /:\s*(true|false|null)\b/g,
      ': <span class="text-yellow-400">$1</span>',
    )
    .replace(
      /:\s*(-?\d+\.?\d*(?:[eE][+-]?\d+)?)\b/g,
      ': <span class="text-blue-400">$1</span>',
    );

  return (
    <div>
      {lineCount > 10 && (
        <button
          onClick={() => setCollapsed(!collapsed)}
          className="text-[9px] text-text-muted hover:text-text mb-0.5"
        >
          {collapsed ? "\u25B8 expand" : "\u25BE collapse"} ({lineCount} lines)
        </button>
      )}
      <pre
        className={`overflow-x-auto whitespace-pre-wrap text-[10px] text-text-secondary ${
          collapsed ? "max-h-6 overflow-hidden" : "max-h-48 overflow-y-auto"
        }`}
        dangerouslySetInnerHTML={{ __html: highlighted }}
      />
    </div>
  );
}

// ── Diff Renderer ────────────────────────────────────────────────────────

function DiffView({ text }: { text: string }) {
  const lines = text.split("\n");

  return (
    <pre className="overflow-x-auto whitespace-pre text-[10px] max-h-48 overflow-y-auto">
      {lines.map((line, i) => {
        let className = "text-text-secondary";
        if (line.startsWith("+++") || line.startsWith("---")) {
          className = "text-text-muted font-bold";
        } else if (line.startsWith("@@")) {
          className = "text-purple-400";
        } else if (line.startsWith("+")) {
          className = "text-green bg-green/5";
        } else if (line.startsWith("-")) {
          className = "text-red bg-red/5";
        }
        return (
          <div key={i} className={className}>
            {line || " "}
          </div>
        );
      })}
    </pre>
  );
}

// ── Enhanced Text Renderer ───────────────────────────────────────────────
//
// Detects multiple inline patterns in a single pass:
//   - URLs (https://...)
//   - Image URLs (.png, .jpg, etc.) → rendered as inline <img>
//   - Base64 data URIs (data:image/...) → rendered as inline <img>
//   - Canvas frame references → "view frame" link
//   - File paths (/absolute/path/to/file.ext) → highlighted

// Combined regex: matches all interesting patterns. Order matters for
// priority — earlier alternatives match first at the same position.
const COMBINED_RE = new RegExp(
  [
    // Canvas frame output: "Rendered html content to canvas 'NAME' (frame: UUID)"
    String.raw`(Rendered html content to canvas '([^']+)' \(frame: ([a-f0-9-]+)\))`,
    // Base64 image data URI
    String.raw`(data:image\/[^;]+;base64,[A-Za-z0-9+/=]+)`,
    // Image URL (must come before generic URL to get special rendering)
    String.raw`(https?:\/\/[^\s)]+\.(?:png|jpe?g|gif|svg|webp)(?:\?[^\s)]*)?)`,
    // Generic URL
    String.raw`(https?:\/\/[^\s)]+)`,
    // Absolute file path with extension (at least 2 segments)
    String.raw`((?:\/[a-zA-Z0-9._@-]+){2,}\.[a-zA-Z0-9]+)`,
  ].join("|"),
  "gi",
);

/** Sentinel for canvas frame — groups 1,2,3 */
const CANVAS_GROUP = 1;
/** Base64 image — group 4 */
const BASE64_GROUP = 4;
/** Image URL — group 5 */
const IMG_URL_GROUP = 5;
/** Generic URL — group 6 */
const URL_GROUP = 6;
/** File path — group 7 */
const PATH_GROUP = 7;

function EnhancedText({ text, htmlContent }: { text: string; htmlContent?: string }) {
  const nodes: React.ReactNode[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  // Reset lastIndex for global regex reuse
  COMBINED_RE.lastIndex = 0;

  while ((match = COMBINED_RE.exec(text)) !== null) {
    // Push plain text before match
    if (match.index > lastIndex) {
      nodes.push(text.slice(lastIndex, match.index));
    }

    const key = match.index;

    if (match[CANVAS_GROUP]) {
      // Canvas frame reference — clickable if we have the HTML content
      const canvasName = match[CANVAS_GROUP + 1];
      const frameId = match[CANVAS_GROUP + 2];
      nodes.push(
        <span key={key} className="text-text-secondary">
          Rendered html content to canvas &apos;{canvasName}&apos; (frame:{" "}
          {htmlContent ? (
            <button
              onClick={() => openHtmlInNewTab(htmlContent)}
              className="text-purple-400 font-mono hover:underline cursor-pointer"
              title="open rendered frame in new tab"
            >
              {frameId}
            </button>
          ) : (
            <span className="text-purple-400 font-mono">{frameId}</span>
          )}
          )
        </span>,
      );
    } else if (match[BASE64_GROUP]) {
      // Base64 image
      const dataUri = match[BASE64_GROUP];
      nodes.push(
        <span key={key} className="block my-1">
          <img
            src={dataUri}
            alt="tool output"
            className="max-w-full max-h-48 rounded border border-border"
          />
        </span>,
      );
    } else if (match[IMG_URL_GROUP]) {
      // Image URL — render both the link and an inline preview
      const url = match[IMG_URL_GROUP];
      nodes.push(
        <span key={key}>
          <a
            href={url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-accent hover:underline"
          >
            {url}
          </a>
          <span className="block my-1">
            <img
              src={url}
              alt="tool output"
              className="max-w-full max-h-48 rounded border border-border"
              onError={(e) => {
                // Hide broken image — may be unreachable
                (e.target as HTMLImageElement).style.display = "none";
              }}
            />
          </span>
        </span>,
      );
    } else if (match[URL_GROUP]) {
      // Generic URL
      const url = match[URL_GROUP];
      nodes.push(
        <a
          key={key}
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          className="text-accent hover:underline"
        >
          {url}
        </a>,
      );
    } else if (match[PATH_GROUP]) {
      // File path
      const path = match[PATH_GROUP];
      nodes.push(
        <span key={key} className="text-blue-400 font-mono">
          {path}
        </span>,
      );
    }

    lastIndex = match.index + match[0].length;
  }

  // Remaining text
  if (lastIndex < text.length) {
    nodes.push(text.slice(lastIndex));
  }

  return (
    <pre className="mt-0.5 max-h-32 overflow-y-auto overflow-x-auto whitespace-pre-wrap text-[10px] text-text-secondary">
      {nodes}
    </pre>
  );
}

// ── Main Component ───────────────────────────────────────────────────────

export function RichOutput({ text, htmlContent }: { text: string; htmlContent?: string }) {
  // 1. JSON?
  const json = tryParseJson(text);
  if (json) {
    return (
      <div>
        <span className="text-text-muted">output: </span>
        <JsonView data={json} />
      </div>
    );
  }

  // 2. Diff?
  if (isDiff(text)) {
    return (
      <div>
        <span className="text-text-muted">output: </span>
        <DiffView text={text} />
      </div>
    );
  }

  // 3. Enhanced text (URLs, images, file paths, canvas frames)
  return (
    <div>
      <span className="text-text-muted">output: </span>
      <EnhancedText text={text} htmlContent={htmlContent} />
    </div>
  );
}
