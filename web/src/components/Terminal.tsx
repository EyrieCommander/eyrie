import { useEffect, useRef, useState } from "react";
import { Terminal as XTerm } from "xterm";
import { FitAddon } from "xterm-addon-fit";
import { WebLinksAddon } from "xterm-addon-web-links";
import "xterm/css/xterm.css";

interface TerminalProps {
  agentName: string;
  onClose: () => void;
}

export default function Terminal({ agentName, onClose }: TerminalProps) {
  const terminalRef = useRef<HTMLDivElement>(null);
  const xtermRef = useRef<XTerm | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const initializedRef = useRef(false);
  const cleanupCountRef = useRef(0);
  const onCloseRef = useRef(onClose);
  const [status, setStatus] = useState<"connecting" | "connected" | "closed">("connecting");

  // Update ref when onClose changes
  onCloseRef.current = onClose;

  useEffect(() => {
    if (!terminalRef.current) return;

    // Prevent double initialization in React Strict Mode
    if (initializedRef.current) {
      return;
    }
    initializedRef.current = true;

    // Scale terminal font with the global zoom level
    const zoomPct = parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;
    const termFontSize = Math.round(13 * zoomPct);

    // Create terminal instance
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
      rows: 24,
      cols: 80,
    });

    xtermRef.current = term;

    // Open terminal first
    term.open(terminalRef.current);

    // Add and fit addon after terminal is opened
    const fitAddon = new FitAddon();
    fitAddonRef.current = fitAddon;
    term.loadAddon(fitAddon);
    term.loadAddon(new WebLinksAddon());

    // Fit after a small delay to ensure container has dimensions
    setTimeout(() => {
      try {
        fitAddon.fit();
      } catch (err) {
        // Silently ignore fit errors
      }
    }, 50);

    // Focus the terminal so user can type immediately
    term.focus();

    // Connect WebSocket
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = `${protocol}//${window.location.host}/api/agents/${agentName}/terminal/ws`;
    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.binaryType = "arraybuffer";

    ws.onopen = () => {
      console.log(`Terminal WebSocket connected for ${agentName}`);
      // Send initial terminal size
      const cols = term.cols;
      const rows = term.rows;
      ws.send(`resize:${rows}:${cols}`);
    };

    ws.onmessage = (event) => {
      // Write output from backend to terminal
      if (event.data instanceof ArrayBuffer) {
        const data = new Uint8Array(event.data);
        term.write(data);
        // Mark as connected on first data received
        if (status === "connecting") {
          setStatus("connected");
        }
      }
    };

    ws.onerror = () => {
      // Suppress error logging - onclose will handle it
    };

    ws.onclose = (event) => {
      console.log(`Terminal WebSocket closed: code=${event.code}, reason=${event.reason || "(no reason)"}`);
      setStatus("closed");

      // Only show message if we've connected (not during initial connection failure)
      if (status === "connecting") {
        term.writeln("\r\n\x1b[1;31mFailed to connect\x1b[0m");
      } else if (event.reason && event.reason.includes("process exited")) {
        term.writeln("\r\n\x1b[1;33mProcess exited\x1b[0m");
      } else if (event.code !== 1000) {
        // Abnormal closure
        term.writeln("\r\n\x1b[1;31mConnection lost\x1b[0m");
      } else {
        term.writeln("\r\n\x1b[1;33mConnection closed\x1b[0m");
      }
    };

    // Send user input to backend
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data);
      }
    });

    // Handle Ctrl+Escape to close terminal (leaves Escape free for vim/TUIs)
    term.attachCustomKeyEventHandler((event) => {
      if (event.ctrlKey && event.key === "Escape" && event.type === "keydown") {
        onCloseRef.current();
        return false; // Prevent default handling
      }
      return true; // Allow other keys to be handled normally
    });

    // Handle terminal resize
    const handleResize = () => {
      fitAddon.fit();
      if (ws.readyState === WebSocket.OPEN) {
        const cols = term.cols;
        const rows = term.rows;
        ws.send(`resize:${rows}:${cols}`);
      }
    };

    window.addEventListener("resize", handleResize);

    // Cleanup - but skip the first cleanup (Strict Mode's double-mount)
    return () => {
      cleanupCountRef.current++;

      // Skip first cleanup in Strict Mode (it will remount)
      if (cleanupCountRef.current === 1) {
        console.log("Skipping first cleanup (Strict Mode)");
        return;
      }

      console.log("Running cleanup - closing terminal");
      window.removeEventListener("resize", handleResize);
      if (wsRef.current) {
        const ws = wsRef.current;
        // Only close if still open or connecting
        if (ws.readyState === WebSocket.OPEN) {
          ws.close(1000, "user closed terminal");
        } else if (ws.readyState === WebSocket.CONNECTING) {
          ws.close();
        }
        // If already closing/closed, don't try to close again
      }
      if (xtermRef.current) {
        xtermRef.current.dispose();
      }
    };
  }, [agentName]);

  return (
    <div
      className="fixed inset-0 bg-black/80 z-50 flex items-center justify-center p-4"
      onClick={onClose}
    >
      <div
        className="bg-bg border border-border rounded-lg shadow-2xl w-full max-w-6xl h-[80vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="px-4 py-3 border-b border-border flex items-center justify-between">
          <div className="flex items-center gap-2">
            <div className="text-sm font-semibold text-fg">
              {agentName} terminal
            </div>
            <div className="text-xs text-fg-muted">
              press ctrl+esc to close
            </div>
          </div>
          <button
            onClick={onClose}
            className="text-xs text-fg-muted hover:text-fg transition-colors px-2 py-1 rounded hover:bg-fg-muted/5"
          >
            close
          </button>
        </div>

        {/* Terminal container */}
        <div className="flex-1 p-2 overflow-hidden relative">
          {status === "connecting" && (
            <div className="absolute inset-0 flex items-center justify-center bg-black/90 z-10">
              <div className="text-center">
                <div className="text-fg text-sm mb-2">
                  Starting {agentName}...
                </div>
                <div className="text-fg-muted text-xs">
                  this may take a few seconds
                </div>
              </div>
            </div>
          )}
          <div
            ref={terminalRef}
            className="w-full h-full"
            style={{ minHeight: '400px', minWidth: '600px' }}
          />
        </div>
      </div>
    </div>
  );
}
