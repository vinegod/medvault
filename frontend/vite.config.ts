import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      // InjectManifest mode: we own the service-worker file (src/sw.ts) so we
      // can hook periodicsync events for daily expiration checks. The plugin
      // injects the precache manifest into our SW at build time.
      strategies: "injectManifest",
      srcDir: "src",
      filename: "sw.ts",
      registerType: "autoUpdate",
      injectManifest: {
        globPatterns: ["**/*.{js,css,html,ico,png,svg,woff,woff2}"],
      },
      manifest: false, // we provide our own public/manifest.json
    }),
  ],

  resolve: {
    alias: {
      "@": "/src",
    },
  },

  server: {
    port: 5173,
    proxy: {
      // In dev, forward all /api/* requests to the Go backend.
      // The browser sees everything on localhost:5173 so CORS never fires.
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },

  build: {
    // Output into backend/static so `go run .` serves it in prod.
    outDir: "../backend/static",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        // Keep third-party code in its own chunk so no single file passes
        // the 500 KB pre-commit limit and app updates don't re-download it.
        manualChunks: (id) => (id.includes("node_modules") ? "vendor" : undefined),
      },
    },
  },
});
