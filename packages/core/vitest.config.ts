import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // Node >= 25 ships a global localStorage that has no methods unless
    // --localstorage-file is set, and it shadows jsdom's Storage in tests.
    execArgv: ["--no-experimental-webstorage"],
    globals: true,
    include: ["**/*.test.{ts,tsx}"],
    passWithNoTests: true,
  },
});
