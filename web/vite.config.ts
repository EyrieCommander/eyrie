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
    proxy: {
      "/api": {
        target: "http://127.0.0.1:7200",
        ws: true, // Enable WebSocket proxying for /api paths
        // SSE streams (chat, briefing, install) can run for minutes while
        // agents process tool calls — disable proxy timeout so the
        // connection isn't dropped mid-stream.
        timeout: 0,
        proxyTimeout: 0,
      },
      "/ws": { target: "ws://127.0.0.1:7200", ws: true },
    },
  },
});
