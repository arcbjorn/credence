// `defineConfig` comes from vitest/config so the `test` block typechecks.
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  // The engine is a real .wasm file loaded at runtime, not inlined: it is
  // ~437KB and only the replay screen needs it, so it stays a separate
  // asset the browser fetches on demand.
  assetsInclude: ["**/*.wasm"],
  // Relative asset paths, so the build works from a subdirectory or a
  // file:// folder rather than only at an origin root. With the default
  // "/" every asset 404s unless the site is served from the root.
  base: "./",
  build: {
    target: "es2022",
    // A judge opens this from a folder or a static host. No server, no
    // dynamic base path.
    assetsDir: "assets",
    rollupOptions: {
      output: {
        // Keep the engine out of the main bundle so the first paint does
        // not wait on it.
        manualChunks(id) {
          if (id.includes("risk_wasm")) return "engine";
          if (id.includes("node_modules/react")) return "react";
          return undefined;
        },
      },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
  },
});
