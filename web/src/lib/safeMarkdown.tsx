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

function safeHref(href: string): string | null {
  try {
    const u = new URL(href);
    return u.protocol === "https:" || u.protocol === "http:" ? u.toString() : null;
  } catch {
    return null;
  }
}

const INLINE = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\s][^*]*\*)|(_[^_\s][^_]*_)|(\[[^\]]+\]\([^)\s]+\))/;

export function renderInline(text: string, keyBase = "i"): React.ReactNode[] {
  const out: React.ReactNode[] = [];
  let rest = text;
  let k = 0;
  while (rest.length > 0) {
    const m = INLINE.exec(rest);
    if (!m) {
      out.push(rest);
      break;
    }
    if (m.index > 0) out.push(rest.slice(0, m.index));
    const tok = m[0];
    const key = `${keyBase}-${k++}`;
    if (tok.startsWith("`")) {
      out.push(<code key={key} className="rounded bg-bg px-1 font-mono text-[11px]">{tok.slice(1, -1)}</code>);
    } else if (tok.startsWith("**")) {
      out.push(<strong key={key}>{renderInline(tok.slice(2, -2), key)}</strong>);
    } else if (tok.startsWith("[")) {
      const close = tok.indexOf("](");
      const label = tok.slice(1, close);
      const href = safeHref(tok.slice(close + 2, -1));
      out.push(
        href ? (
          <a key={key} href={href} target="_blank" rel="noopener noreferrer" className="text-accent underline">
            {label}
          </a>
        ) : (
          <span key={key}>{label}</span>
        ),
      );
    } else {
      out.push(<em key={key}>{renderInline(tok.slice(1, -1), key)}</em>);
    }
    rest = rest.slice(m.index + tok.length);
  }
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

export function SafeMarkdown({ text }: { text: string }) {
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
}
