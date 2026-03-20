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
      },
      "/ws": { target: "ws://127.0.0.1:7200", ws: true },
    },
  },
});
