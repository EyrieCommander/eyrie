// Standalone check for src/lib/safeMarkdown.tsx (main has no web test
// runner). Run from web/: node scripts/check-safe-markdown.mjs
// Needs web/node_modules (esbuild, react, react-dom).
//
// Each case renders in a worker thread with a 2 s timeout, so an input
// that makes the block loop spin fails the check instead of hanging it.
import { build } from "esbuild";
import { Worker } from "node:worker_threads";
import { fileURLToPath } from "node:url";

const entry = `
import { parentPort, workerData } from "node:worker_threads";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { SafeMarkdown } from ${JSON.stringify(fileURLToPath(new URL("../src/lib/safeMarkdown.tsx", import.meta.url)))};
parentPort.postMessage(renderToStaticMarkup(createElement(SafeMarkdown, { text: workerData.text })));
`;
const out = await build({
  stdin: { contents: entry, resolveDir: fileURLToPath(new URL("..", import.meta.url)), loader: "tsx" },
  bundle: true, write: false, format: "cjs", platform: "node", jsx: "automatic",
  define: { "process.env.NODE_ENV": '"production"' }, logLevel: "error",
});
const workerSrc = out.outputFiles[0].text;

const cases = [
  ["heading + U+2028", "# title\u2028body", (h) => h.includes("title") && h.includes("body")],
  ["heading + U+2029", "# title\u2029body", (h) => h.includes("body")],
  ["hash without space", "#notheading\nnext", (h) => h.includes("#notheading")],
  ["bare dash", "-\nx", (h) => h.includes("x")],
  ["list marker, no text", "- \nx", (h) => h.includes("x")],
  ["html stays text", "<script>alert(1)</script> **b**", (h) => !h.includes("<script>") && h.includes("&lt;script&gt;") && h.includes("<strong>b</strong>")],
  ["javascript link dropped", "[x](javascript:alert(1))", (h) => !h.includes("javascript:") && h.includes("x")],
  ["https link kept", "[x](https://example.com)", (h) => h.includes('href="https://example.com/"') && h.includes("noopener")],
  ["unclosed fence", "```\ncode", (h) => h.includes("code")],
  ["vertical tab / NEL", "a\u000b# b\u0085c", (h) => h.includes("c")],
];

let failed = 0;
for (const [name, text, ok] of cases) {
  const html = await new Promise((resolve) => {
    const w = new Worker(workerSrc, { eval: true, workerData: { text } });
    const t = setTimeout(() => { w.terminate(); resolve(null); }, 2000);
    w.on("message", (m) => { clearTimeout(t); w.terminate(); resolve(m); });
    w.on("error", (e) => { clearTimeout(t); resolve("ERROR " + e.message); });
  });
  const pass = html !== null && !String(html).startsWith("ERROR") && ok(html);
  if (!pass) failed++;
  console.log(`${pass ? "ok  " : "FAIL"} ${name}${html === null ? " (timed out: render loop hung)" : pass ? "" : " -> " + html}`);
}
console.log(`${cases.length - failed}/${cases.length} passed`);
process.exit(failed ? 1 : 0);
