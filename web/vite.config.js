import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [svelte()],
  build: {
    rollupOptions: {
      input: {
        main: fileURLToPath(new URL("./index.html", import.meta.url)),
        ranges: fileURLToPath(new URL("./ranges.html", import.meta.url)),
      },
    },
  },
  server: {
    proxy: { "/api": "http://localhost:8080" },
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.js"],
  },
});
