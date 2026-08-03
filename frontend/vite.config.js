import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const root = path.dirname(fileURLToPath(import.meta.url));
// Where `yarn dev` forwards /api (and short-code redirects) to.
const backend = process.env.KORTEX_DEV_BACKEND || "http://localhost:8090";

export default defineConfig({
  plugins: [react()],
  // REACT_APP_ is kept so existing Docker/CI build args keep working.
  envPrefix: ["VITE_", "REACT_APP_"],
  resolve: {
    alias: { "@": path.resolve(root, "src") },
  },
  server: {
    port: 3000,
    strictPort: true,
    proxy: {
      "/api": { target: backend, changeOrigin: true },
    },
  },
  preview: {
    port: 3000,
    proxy: {
      "/api": { target: backend, changeOrigin: true },
    },
  },
  build: {
    outDir: "build",
    emptyOutDir: true,
    sourcemap: false,
    chunkSizeWarningLimit: 900,
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.js"],
    css: false,
    restoreMocks: true,
    include: ["src/**/*.test.{js,jsx}"],
  },
});
