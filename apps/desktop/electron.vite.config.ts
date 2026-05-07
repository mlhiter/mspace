import { resolve } from "node:path";
import { defineConfig, externalizeDepsPlugin } from "electron-vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  main: {
    plugins: [externalizeDepsPlugin()],
  },
  preload: {
    plugins: [externalizeDepsPlugin()],
  },
  renderer: {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@": resolve("src/renderer/src"),
        "@mspace/core": resolve("../../packages/core/src/index.ts"),
        "@mspace/ui/components": resolve("../../packages/ui/src/components"),
        "@mspace/ui/lib": resolve("../../packages/ui/src/lib"),
        "@mspace/ui": resolve("../../packages/ui/src/index.tsx"),
        "@mspace/views": resolve("../../packages/views/src/index.ts"),
      },
    },
  },
});
