import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The backend the dev server proxies to.
const backend = "http://127.0.0.1:8080";

export default defineConfig({
  // The Go binary serves the build under /app/, so every asset URL must be
  // relative to that prefix.
  base: "/app/",
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": { target: backend, changeOrigin: false },
      "/auth": { target: backend, changeOrigin: false },
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    restoreMocks: true,
  },
});
