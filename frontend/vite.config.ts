import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { VitePWA } from "vite-plugin-pwa";

export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: "autoUpdate",
      workbox: {
        // Precache all app-shell assets.
        globPatterns: ["**/*.{js,css,html,ico,png,svg,woff,woff2}"],

        // SPA routing: serve index.html for all navigation requests.
        // Deny-list keeps /api/* and /offline.html from being caught.
        navigateFallback: "/index.html",
        navigateFallbackDenylist: [/^\/api/, /\/offline\.html$/],

        runtimeCaching: [
          {
            // Health and lightweight read endpoints — network-first so
            // the UI always shows fresh status when online, but the
            // last-known response is served when the device is offline.
            urlPattern: /^https?:\/\/[^/]+\/api\/(health|medicines|alerts)/,
            handler: "NetworkFirst",
            options: {
              cacheName: "api-reads",
              networkTimeoutSeconds: 8,
              expiration: {
                maxEntries: 60,
                maxAgeSeconds: 60 * 60 * 24, // 24 h
              },
              cacheableResponse: { statuses: [0, 200] },
            },
          },
          {
            // Icons and other images — cache-first after first load.
            urlPattern: /\/icons\/.*\.png$/,
            handler: "CacheFirst",
            options: {
              cacheName: "icons",
              expiration: {
                maxEntries: 20,
                maxAgeSeconds: 60 * 60 * 24 * 30, // 30 days
              },
              cacheableResponse: { statuses: [0, 200] },
            },
          },
        ],
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
  },
});
