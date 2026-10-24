import { defineConfig } from "vite";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

export default defineConfig({
  clearScreen: false,
  plugins: [{
    name: "racomi-offline-notices",
    resolveId(id) {
      if (id === "virtual:racomi-notices") return "\0virtual:racomi-notices";
    },
    load(id) {
      if (id !== "\0virtual:racomi-notices") return;
      // Keep the existing distributable notices as the single source while
      // embedding their text for offline use in both dev and packaged builds.
      return [
        ["notices", "THIRD_PARTY_NOTICES.txt"],
        ["sparkleLicense", "Sparkle-LICENSE.txt"],
      ].map(([name, file]) => {
        const path = fileURLToPath(new URL(`./public/${file}`, import.meta.url));
        this.addWatchFile(path);
        return `export const ${name} = ${JSON.stringify(readFileSync(path, "utf8"))};`;
      }).join("\n");
    },
  }],
  server: {
    host: "127.0.0.1",
    port: 9245,
    strictPort: true,
  },
  build: {
    target: "es2022",
  },
});
