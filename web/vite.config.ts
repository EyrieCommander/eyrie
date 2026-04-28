import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Suppress Vite's "[vite] ws proxy socket error: EPIPE" noise.
// When the browser closes a tmux WebSocket, Vite's internal WS proxy
// handler catches the EPIPE from the dead upstream socket and logs it
// via the Vite logger before our proxy event handlers can intercept it.
// This is harmless (tmux session persists regardless), but the full
// stack trace clutters the dev terminal. Vite doesn't expose a config
// option to silence it, so we filter it at the logger level.
const _origConsoleError = console.error;
console.error = (...args: unknown[]) => {
  const first = typeof args[0] === "string" ? args[0] : "";
  if (first.includes("ws proxy socket error") || first.includes("write EPIPE")) return;
  _origConsoleError(...args);
};

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
  server: {
    hmr: {
      // Reduce console noise when backend is down — Vite's HMR client
      // retries every 1s by default, flooding the console with WebSocket
      // errors. A longer interval keeps the connection alive without spam.
      timeout: 30000,
    },
    proxy: {
      "/api": {
        target: "http://127.0.0.1:7200",
        ws: true,
        timeout: 0,
        proxyTimeout: 0,
        // Disable response compression so SSE events stream in real-time
        // instead of being buffered until the response completes.
        headers: { "Accept-Encoding": "identity" },
        // WHY: When the Go backend restarts (hot reload), the proxy gets
        // ECONNREFUSED for a few seconds. Vite logs noisy red errors for
        // each failed request. This handler suppresses ECONNREFUSED errors
        // and returns a quiet 503 so the frontend retries on next poll.
        configure: (proxy, _options, server) => {
          // Replace Vite's default error listener to suppress noisy
          // ECONNREFUSED logs when the Go backend is restarting.
          proxy.removeAllListeners("error");
          proxy.on("error", (err: any, _req, res) => {
            // Only log non-ECONNREFUSED errors
            if (err?.code !== "ECONNREFUSED" && err?.code !== "EPIPE") {
              server?.config?.logger?.error(`[proxy] ${err.message}`);
            }
            if (res && "writeHead" in res) {
              // HTTP response — send 503 so the frontend retries
              try {
                (res as any).writeHead(503, { "Content-Type": "application/json" });
                (res as any).end(JSON.stringify({ error: "backend restarting" }));
              } catch {
                // Response may already be sent
              }
            } else if (res && "destroy" in res) {
              // WebSocket socket — clean up so the browser reconnects
              try { (res as any).destroy(); } catch { /* already closed */ }
            }
          });
          // Suppress EPIPE/ECONNRESET on BOTH sides of the WS proxy.
          // Vite's http-proxy pipes two sockets together — the error can
          // come from either the upstream (backend) or downstream (browser)
          // socket. Catching on proxyReqWs alone misses the response side.
          for (const event of ["proxyReqWs", "open"] as const) {
            proxy.on(event, (_arg1: any, _arg2: any, socket: any) => {
              if (socket && typeof socket.on === "function" && !socket.__eyrieErrorHandled) {
                socket.__eyrieErrorHandled = true;
                socket.on("error", (err: any) => {
                  if (err?.code !== "EPIPE" && err?.code !== "ECONNRESET") {
                    server?.config?.logger?.error(`[ws proxy] ${err.message}`);
                  }
                });
              }
            });
          }
        },
      },
      "/ws": {
        target: "ws://127.0.0.1:7200",
        ws: true,
        configure: (proxy) => {
          proxy.removeAllListeners("error");
          proxy.on("error", () => {}); // suppress EPIPE/ECONNREFUSED noise
        },
      },
    },
  },
});
