// safeMarkdown.tsx — minimal markdown → React elements for chief replies.
//
// WHY no library and no innerHTML: chief replies arrive over the public
// bridge, so they are untrusted. This renderer only ever creates React
// elements from text, so any HTML in a reply shows as literal text. Links
// are kept only for http(s) and open in a new tab with noopener.
//
// Supported: headings (#..###), paragraphs, fenced code, - / * / 1. lists,
// > quotes, **bold**, *italic* / _italic_, `code`, [text](https://...).

import type React from "react";
import { memo } from "react";

function safeHref(href: string): string | null {
  try {
    const u = new URL(href);
    return u.protocol === "https:" || u.protocol === "http:" ? u.toString() : null;
  } catch {
    return null;
  }
}

// Inline parsing is a single left-to-right scan, not a regex: chief replies
// are untrusted, and backtracking regexes over unmatched brackets are
// quadratic. Each opener looks for its closer within MAX_SPAN characters
// (an unclosed opener costs at most MAX_SPAN), and nested emphasis is
// parsed at most MAX_DEPTH deep, so the total work is O(n * MAX_SPAN).
const MAX_SPAN = 512;
const MAX_DEPTH = 4;

/** Index of `closer` in text[from, from+MAX_SPAN), or -1. Searches only
 *  that window (never the rest of the text), so an unclosed opener costs
 *  O(MAX_SPAN), not O(n). */
function findClose(text: string, from: number, closer: string): number {
  const window = text.slice(from, from + MAX_SPAN);
  const at = window.indexOf(closer);
  return at < 0 ? -1 : from + at;
}

export function renderInline(text: string, keyBase = "i", depth = 0): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  let plain = "";
  let k = 0;
  const flush = () => {
    if (plain) out.push(plain);
    plain = "";
  };
  let i = 0;
  while (i < text.length) {
    const c = text[i];
    const key = `${keyBase}-${k}`;
    if (c === "`") {
      const end = findClose(text, i + 1, "`");
      if (end > i + 1) {
        flush();
        out.push(<code key={key} className="rounded bg-bg px-1 font-mono text-[11px]">{text.slice(i + 1, end)}</code>);
        k++;
        i = end + 1;
        continue;
      }
    } else if (c === "*" && text[i + 1] === "*" && depth < MAX_DEPTH) {
      const end = findClose(text, i + 2, "**");
      if (end > i + 2) {
        flush();
        out.push(<strong key={key}>{renderInline(text.slice(i + 2, end), key, depth + 1)}</strong>);
        k++;
        i = end + 2;
        continue;
      }
    } else if ((c === "*" || c === "_") && depth < MAX_DEPTH && text[i + 1] && !/\s/.test(text[i + 1]) && text[i + 1] !== c) {
      const end = findClose(text, i + 1, c);
      if (end > i + 1) {
        flush();
        out.push(<em key={key}>{renderInline(text.slice(i + 1, end), key, depth + 1)}</em>);
        k++;
        i = end + 1;
        continue;
      }
    } else if (c === "[") {
      const mid = findClose(text, i + 1, "](");
      if (mid > i + 1 && !text.slice(i + 1, mid).includes("[")) {
        const end = findClose(text, mid + 2, ")");
        const href = end > mid + 2 ? text.slice(mid + 2, end) : "";
        if (href && !/\s/.test(href)) {
          flush();
          const label = text.slice(i + 1, mid);
          const safe = safeHref(href);
          out.push(
            safe ? (
              <a key={key} href={safe} target="_blank" rel="noopener noreferrer" className="text-accent underline">
                {label}
              </a>
            ) : (
              <span key={key}>{label}</span>
            ),
          );
          k++;
          i = end + 1;
          continue;
        }
      }
    }
    plain += c;
    i++;
  }
  flush();
  return out;
}

/** True when `line` starts a non-paragraph block. Shared by the block
 *  dispatcher and the paragraph loop so the two can never disagree (if the
 *  paragraph loop refused a line the dispatcher also refused, nothing would
 *  consume it and rendering would spin forever). */
function startsBlock(line: string): boolean {
  return (
    line.startsWith("```") ||
    /^#{1,3} \S/.test(line) ||
    line.startsWith(">") ||
    /^ *([-*]|\d+\.) +\S/.test(line)
  );
}

/** Memoised: ChiefChat re-renders every second for its elapsed timers, and
 *  stored replies never change, so each reply is parsed once. */
export const SafeMarkdown = memo(function SafeMarkdown({ text }: { text: string }) {
  // Normalise every line terminator, including U+2028/U+2029 and other
  // vertical whitespace that JS regexes treat as line breaks.
  const lines = text.replace(/\r\n?|[\u2028\u2029\u0085\v\f]/g, "\n").split("\n");
  const blocks: React.ReactNode[] = [];
  let i = 0;
  let b = 0;
  while (i < lines.length) {
    const line = lines[i];
    const key = `b${b++}`;
    if (line.startsWith("```")) {
      const body: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith("```")) body.push(lines[i++]);
      i++; // closing fence
      blocks.push(
        <pre key={key} className="overflow-x-auto rounded bg-bg p-2 font-mono text-[11px] whitespace-pre">
          {body.join("\n")}
        </pre>,
      );
      continue;
    }
    const h = /^(#{1,3}) (\S.*)$/.exec(line);
    if (h) {
      const cls = h[1].length === 1 ? "text-sm font-semibold" : "text-xs font-semibold";
      blocks.push(<div key={key} className={cls}>{renderInline(h[2], key)}</div>);
      i++;
      continue;
    }
    if (/^ *([-*]|\d+\.) +\S/.test(line)) {
      const ordered = /^ *\d+\./.test(line);
      const items: React.ReactNode[] = [];
      while (i < lines.length && /^ *([-*]|\d+\.) +\S/.test(lines[i])) {
        items.push(<li key={`${key}-${items.length}`}>{renderInline(lines[i].replace(/^ *([-*]|\d+\.) +/, ""), `${key}-${items.length}`)}</li>);
        i++;
      }
      blocks.push(
        ordered ? <ol key={key} className="list-decimal pl-4 space-y-0.5">{items}</ol> : <ul key={key} className="list-disc pl-4 space-y-0.5">{items}</ul>,
      );
      continue;
    }
    if (line.startsWith(">")) {
      const q: string[] = [];
      while (i < lines.length && lines[i].startsWith(">")) q.push(lines[i++].replace(/^>\s?/, ""));
      blocks.push(<blockquote key={key} className="border-l-2 border-border pl-2 text-text-muted">{renderInline(q.join(" "), key)}</blockquote>);
      continue;
    }
    if (line.trim() === "") {
      i++;
      continue;
    }
    // Paragraph: always consumes at least the current line, so the loop
    // makes progress whatever the input.
    const para: string[] = [lines[i++]];
    while (i < lines.length && lines[i].trim() !== "" && !startsBlock(lines[i])) para.push(lines[i++]);
    blocks.push(<p key={key}>{renderInline(para.join(" "), key)}</p>);
  }
  return <div className="space-y-1.5 text-xs text-text break-words">{blocks}</div>;
});
