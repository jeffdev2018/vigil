import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
    // Node >= 25 ships a global localStorage that has no methods unless
    // --localstorage-file is set, and it shadows jsdom's Storage in tests.
    execArgv: ["--no-experimental-webstorage"],
    environment: "jsdom",
    globals: true,
    setupFiles: ["./test/setup.ts"],
    include: ["**/*.test.{ts,tsx}"],
    // Pin the timezone so date-rendering assertions (e.g. billing seat
    // summaries) do not depend on the host machine's offset. CI runners are
    // UTC; without this pin the same suite goes red on machines behind UTC.
    env: {
      TZ: "UTC",
    },
  },
});
