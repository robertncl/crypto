import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In development the SPA runs on :5173 and proxies API + WebSocket traffic to
// the Go backend on :8080, so the browser talks to a single origin and there is
// no CORS or mixed-origin WebSocket handshake to manage.
const BACKEND = process.env.BACKEND_URL ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: BACKEND, changeOrigin: true },
      "/ws": { target: BACKEND, ws: true, changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    sourcemap: false,
  },
});
