import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  test: {
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
