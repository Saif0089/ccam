import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The three surfaces build to one static bundle, embedded into the Go binaries
// and served by the panel. base "./" keeps asset URLs relative so it works
// served at "/" (Vercel) or "/panel/" (a machine's local reverse proxy).
export default defineConfig({
  plugins: [react()],
  base: "./",
  build: { outDir: "../panel/webui", emptyOutDir: true },
  server: { proxy: { "/api": "http://127.0.0.1:47933" } },
});
