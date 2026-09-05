import { defineConfig } from "vitest/config";
import path from "path";

export default defineConfig({
  test: {
    // Node >= 25 ships a global localStorage that has no methods unless
    // --localstorage-file is set, and it shadows jsdom's Storage in tests.
    execArgv: ["--no-experimental-webstorage"],
    environment: "node",
    globals: true,
    include: ["**/*.test.{ts,tsx}"],
    exclude: ["node_modules/**", ".next/**", ".source/**"],
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "."),
    },
  },
});
