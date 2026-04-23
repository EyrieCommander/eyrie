import { useEffect, useRef, useState, forwardRef, useImperativeHandle } from "react";
import { Terminal as XTerm } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { WebLinksAddon } from "xterm-addon-web-links";
import "xterm/css/xterm.css";
import { createLineParser } from "../lib/terminalParser";

export interface TerminalHandle {
  /** Send a command string into the running terminal (types it, does NOT press enter). */
  sendCommand: (cmd: string) => void;
  /** Send a command and press enter. */
  runCommand: (cmd: string) => void;
}

interface TerminalProps {
  agentName: string;
  onClose?: () => void;
  /** Command to type into the terminal once connected (not auto-executed — user hits enter). */
  initialCommand?: string;
  /** Use a plain shell instead of the agent's CLI. */
  useShell?: boolean;
  /** Render inline (fills parent container) instead of as a modal overlay. */
  inline?: boolean;
  /** Named session for tmux persistence. If set and tmux is available, the
   *  session survives WebSocket disconnections (page navigations, reloads). */
  session?: string;
  /** Fired for each complete, ANSI-stripped line of terminal output. Used by
   *  the onboarding flow to detect install / configure / launch markers and
   *  advance sub-steps without waiting on filesystem polling. */
  onOutput?: (line: string) => void;
}

const COPY_HINT = "drag to select + copy \u00b7 Shift+drag to keep selection";

const Terminal = forwardRef<TerminalHandle, TerminalProps>(function Terminal(
  { agentName, onClose, initialCommand, useShell, inline, session, onOutput },
  ref,
) {
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const initializedRef = useRef(false);
  const setupDoneRef = useRef(false);
  const onCloseRef = useRef(onClose);
  const onOutputRef = useRef(onOutput);
  const [status, setStatus] = useState<"connecting" | "connected" | "closed">("connecting");
  const statusRef = useRef(status);

  onCloseRef.current = onClose;
  onOutputRef.current = onOutput;
  statusRef.current = status;

  // Expose sendCommand / runCommand to parent via ref
  useImperativeHandle(ref, () => ({
    sendCommand: (cmd: string) => {
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(cmd);
      }
    },
    runCommand: (cmd: string) => {
      const ws = wsRef.current;
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(cmd + "\n");
      }
    },
  }));

  useEffect(() => {
    if (!terminalRef.current) return;
    if (initializedRef.current) return;
    initializedRef.current = true;

    const zoomPct = parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;
    const termFontSize = Math.round(13 * zoomPct);

    const term = new XTerm({
      cursorBlink: true,
      fontSize: termFontSize,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: "#000000",
        foreground: "#ffffff",
        cursor: "#ffffff",
        selectionBackground: "rgba(255, 255, 255, 0.3)",
      },
      rows: inline ? 20 : 24,
      cols: 80,
    });

    xtermRef.current = term;
    term.open(terminalRef.current);

    const fitAddon = new FitAddon();
    fitAddonRef.current = fitAddon;
    term.loadAddon(fitAddon);
    term.loadAddon(new WebLinksAddon());

    setTimeout(() => {
      try { fitAddon.fit(); } catch { /* ignore */ }
    }, 50);

    term.focus();

    // Connect WebSocket
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const sessionParam = session ? `?session=${encodeURIComponent(session)}` : "";
    const wsUrl = useShell
      ? `${protocol}//${window.location.host}/api/terminal/ws${sessionParam}`
      : `${protocol}//${window.location.host}/api/agents/${agentName}/terminal/ws`;
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;
    ws.binaryType = "arraybuffer";

    ws.onopen = () => {
      // Re-fit after connection to ensure terminal fills container
      try { fitAddon.fit(); } catch { /* ignore */ }
      ws.send(`resize:${term.rows}:${term.cols}`);
    };

    // Line-oriented parser tees the same byte stream that goes into xterm, so
    // FrameworksPhase can detect install / launch success markers in real time.
    // Read onOutputRef on each line so parent re-renders with new callbacks
    // take effect immediately (without resetting the buffer).
    const lineParser = createLineParser((line) => {
      onOutputRef.current?.(line);
    });

    let sentInitialCommand = false;
    ws.onmessage = (event) => {
      if (event.data instanceof ArrayBuffer) {
        const bytes = new Uint8Array(event.data);
        term.write(bytes);
        lineParser(bytes);
        if (statusRef.current === "connecting") setStatus("connected");
        if (initialCommand && !sentInitialCommand) {
          sentInitialCommand = true;
          setTimeout(() => {
            if (ws.readyState === WebSocket.OPEN) ws.send(initialCommand);
          }, 300);
        }
      }
    };

    ws.onerror = () => {};

    ws.onclose = (event) => {
      setStatus("closed");
      if (event.code === 1000 && onCloseRef.current) {
        setTimeout(() => onCloseRef.current?.(), 300);
        return;
      }
      if (statusRef.current === "connecting") {
        term.writeln("\r\n\x1b[1;31mFailed to connect\x1b[0m");
      } else {
        term.writeln("\r\n\x1b[1;31mConnection lost\x1b[0m");
      }
    };

    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(data);
    });

    term.attachCustomKeyEventHandler((event) => {
      if (event.type !== "keydown") return true;
      // Cmd+C (Mac) / Ctrl+Shift+C (Linux): copy selected text via the
      // clipboard API. xterm.js renders on <canvas>, so native browser
      // selection doesn't work — we read from xterm's internal selection.
      if (event.key === "c") {
        const isMacCopy = event.metaKey && !event.ctrlKey;
        const isLinuxCopy = event.ctrlKey && event.shiftKey;
        if ((isMacCopy || isLinuxCopy) && term.hasSelection()) {
          navigator.clipboard.writeText(term.getSelection()).catch(() => {});
          return false;
        }
      }
      // Cmd+V (Mac) / Ctrl+Shift+V (Linux): paste from clipboard
      if (event.key === "v") {
        const isMacPaste = event.metaKey && !event.ctrlKey;
        const isLinuxPaste = event.ctrlKey && event.shiftKey;
        if (isMacPaste || isLinuxPaste) {
          navigator.clipboard.readText()
            .then((text) => { if (ws.readyState === WebSocket.OPEN) ws.send(text); })
            .catch(() => {});
          return false;
        }
      }
      if (onCloseRef.current && event.ctrlKey && event.key === "Escape") {
        onCloseRef.current();
        return false;
      }
      return true;
    });

    const handleResize = () => {
      fitAddon.fit();
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(`resize:${term.rows}:${term.cols}`);
      }
    };
    window.addEventListener("resize", handleResize);

    // Suppress the browser context menu unless the user has a Shift+drag
    // selection they might want to copy via right-click → Copy.
    const handleContextMenu = (e: MouseEvent) => {
      const hasNativeSelection = (window.getSelection()?.toString().length ?? 0) > 0;
      if (!hasNativeSelection) e.preventDefault();
    };
    terminalRef.current?.addEventListener("contextmenu", handleContextMenu);
    const termEl = terminalRef.current;

    setupDoneRef.current = true;

    return () => {
      if (!setupDoneRef.current) return; // Not yet initialized — nothing to tear down

      window.removeEventListener("resize", handleResize);
      termEl?.removeEventListener("contextmenu", handleContextMenu);
      if (wsRef.current) {
        const w = wsRef.current;
        if (w.readyState === WebSocket.OPEN) w.close(1000, "user closed terminal");
        else if (w.readyState === WebSocket.CONNECTING) w.close();
      }
      if (xtermRef.current) xtermRef.current.dispose();
      setupDoneRef.current = false;
      initializedRef.current = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps -- useShell, session,
    // inline, and initialCommand are structurally stable (parent remounts via
    // key= on change), so they are intentionally omitted from deps.
  }, [agentName]);

  // ── Inline mode: render directly in parent container ────────────────
  if (inline) {
    return (
      <div className="rounded border border-border bg-black overflow-hidden flex flex-col h-full">
        <div className="flex-1 p-1 overflow-hidden relative min-h-0">
          {status === "connecting" && (
            <div className="absolute inset-0 flex items-center justify-center bg-black/90 z-10">
              <div className="text-center">
                <div className="text-white text-xs mb-1">connecting...</div>
              </div>
            </div>
          )}
          <div ref={terminalRef} className="w-full h-full" />
        </div>
        <div className="px-2 py-0.5 text-[10px] text-neutral-500 border-t border-neutral-800 select-none">
          {COPY_HINT}
        </div>
      </div>
    );
  }

  // ── Overlay mode: modal covering the screen ─────────────────────────
  return (
    <div
      className="fixed inset-0 bg-black/80 z-50 flex items-center justify-center p-4"
      onClick={onClose}
    >
      <div
        className="bg-bg border border-border rounded-lg shadow-2xl w-full max-w-6xl h-[80vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="px-4 py-3 border-b border-border flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="text-sm font-semibold text-fg">{agentName} terminal</div>
            <div className="text-xs text-fg-muted">ctrl+esc to close · {COPY_HINT}</div>
          </div>
          <button
            onClick={onClose}
            className="text-xs text-fg-muted hover:text-fg transition-colors px-2 py-1 rounded hover:bg-fg-muted/5"
          >
            close
          </button>
        </div>
        <div className="flex-1 p-2 overflow-hidden relative">
          {status === "connecting" && (
            <div className="absolute inset-0 flex items-center justify-center bg-black/90 z-10">
              <div className="text-center">
                <div className="text-fg text-sm mb-2">Starting {agentName}...</div>
                <div className="text-fg-muted text-xs">this may take a few seconds</div>
              </div>
            </div>
          )}
          <div ref={terminalRef} className="w-full h-full" style={{ minHeight: "400px", minWidth: "600px" }} />
        </div>
      </div>
    </div>
  );
});

export default Terminal;
