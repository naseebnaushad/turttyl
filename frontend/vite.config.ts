import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The built files go to dist/, which the Go binary embeds (see embed.go).
export default defineConfig({
  plugins: [react()],
  build: { outDir: "dist", emptyOutDir: true, sourcemap: false, chunkSizeWarningLimit: 1500 },
});
