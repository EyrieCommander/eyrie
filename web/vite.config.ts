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
        ws: true,
        timeout: 0,
        proxyTimeout: 0,
        // Disable response compression so SSE events stream in real-time
        // instead of being buffered until the response completes.
        headers: { "Accept-Encoding": "identity" },
      },
      "/ws": { target: "ws://127.0.0.1:7200", ws: true },
    },
  },
});
