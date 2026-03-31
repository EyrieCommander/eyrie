import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

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
          // Remove Vite's default error listener to prevent double-logging
          proxy.removeAllListeners("error");
          proxy.on("error", (err: any, _req, res) => {
            // Only suppress connection-refused (backend restarting)
            if (err?.code !== "ECONNREFUSED") {
              server?.config?.logger?.error(`[proxy] ${err.message}`);
            }
            if (res && "writeHead" in res) {
              try {
                (res as any).writeHead(503, { "Content-Type": "application/json" });
                (res as any).end(JSON.stringify({ error: "backend restarting" }));
              } catch {
                // Response may already be sent
              }
            }
          });
        },
      },
      "/ws": { target: "ws://127.0.0.1:7200", ws: true },
    },
  },
});
