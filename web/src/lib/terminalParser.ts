// Terminal output parser for framework-install progress.
//
// The Terminal component pipes PTY bytes from the WebSocket straight into xterm;
// this module layers a line-oriented parser on top of the same byte stream so
// FrameworksPhase can advance sub-steps as the install / configure / launch
// commands emit recognisable markers. Without this, the UI would have to wait
// on the 5-second filesystem-polling loop to notice that anything happened.

// ANSI CSI (Control Sequence Introducer) — colour/cursor/line-clear sequences.
const ANSI_CSI = /\x1b\[[0-?]*[ -/]*[@-~]/g;
// ANSI OSC (Operating System Command) — title setting etc., terminated by BEL.
const ANSI_OSC = /\x1b\][^\x07]*\x07/g;

/** Strip ANSI escape sequences so regex patterns see plain text. */
export function stripAnsi(input: string): string {
  return input.replace(ANSI_CSI, "").replace(ANSI_OSC, "");
}

/**
 * Create a stateful line parser. Feed in raw PTY chunks (bytes or string);
 * the parser buffers partial lines until \n/\r/\r\n arrives, strips ANSI,
 * then invokes `onLine` for each complete line with `.trim()` applied.
 *
 * Empty lines are dropped — they're noise from shell redraws.
 */
export function createLineParser(
  onLine: (line: string) => void,
): (chunk: Uint8Array | string) => void {
  let buf = "";
  const decoder = new TextDecoder();
  return (chunk) => {
    const text =
      typeof chunk === "string"
        ? chunk
        : decoder.decode(chunk, { stream: true });
    buf += text;
    // Split on \r\n, lone \r, or \n. Keep trailing partial piece in the buffer.
    const parts = buf.split(/\r\n|\r|\n/);
    buf = parts.pop() ?? "";
    for (const raw of parts) {
      const line = stripAnsi(raw).trim();
      if (line) onLine(line);
    }
  };
}

export type SubStepId = "install" | "configure" | "api_key" | "launch";

export interface PatternMatch {
  kind: string;
  /** First capture group, if present. */
  captured?: string;
}

/**
 * Per-sub-step regex library. Parsed lines are matched against the CURRENT
 * sub-step's patterns only, to reduce false positives from stale output still
 * scrolling in the terminal.
 *
 * `install` patterns cover eyrie's own CLI markers, cargo, npm, and generic
 * install scripts. `launch` patterns watch for gateway-ready signals.
 * `api_key` has no terminal-side detection — the KeyVault poll / form submit
 * handles state transitions there.
 */
export const PATTERNS: Record<SubStepId, Array<{ kind: string; re: RegExp }>> = {
  install: [
    // eyrie CLI phase marker: "Phase 2/4: config"
    { kind: "eyrie_phase", re: /Phase (\d)\/(\d): (\w+)/ },
    // eyrie CLI success: "✓ Binary installed"
    { kind: "eyrie_binary_done", re: /✓ Binary installed/ },
    // cargo: "   Installed <crate> v1.2.3 (/path)"
    { kind: "cargo_done", re: /^\s*Installed \S+ v\S+/ },
    // Script installer: "Installed zeroclaw to /usr/local/bin"
    { kind: "script_done", re: /Installed \S+ (?:to |at )/ },
    // npm: "changed 42 packages in 3s"
    { kind: "npm_done", re: /changed \d+ packages/ },
  ],
  configure: [
    // eyrie CLI: "✓ Configuration ready"
    { kind: "config_ready", re: /✓ Configuration ready/ },
    // Generic: "saving config to /..."
    { kind: "saving", re: /saving config to /i },
    // Framework onboard wizards often print "<Framework> is ready!"
    { kind: "framework_ready", re: /is ready!$/ },
  ],
  api_key: [],
  launch: [
    { kind: "gateway_started", re: /Gateway started on/i },
    { kind: "listening", re: /Listening on :\d+/ },
    // zeroclaw / picoclaw "ready" status line after `service start`.
    // Alternation is grouped so "service" is required before both
    // "ready" and "running on" — without the group, "running on" would
    // match anywhere (e.g. "running on empty").
    { kind: "service_ready", re: /service (?:(?:is )?ready|running on)/i },
  ],
};

/**
 * Match a single line against the given sub-step's pattern list. Returns the
 * first matching pattern's kind + first capture group (if any), or null.
 */
export function matchLine(line: string, step: SubStepId): PatternMatch | null {
  for (const p of PATTERNS[step]) {
    const m = line.match(p.re);
    if (m) return { kind: p.kind, captured: m[1] };
  }
  return null;
}
