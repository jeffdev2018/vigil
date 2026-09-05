// @vitest-environment node
import { describe, it, expect } from "vitest";
import { effectiveWindowMinutes, getDefaultScheduleConfig } from "./model";

const base = getDefaultScheduleConfig("UTC");

describe("effectiveWindowMinutes", () => {
  it("writes the band of a fixed-time schedule", () => {
    expect(effectiveWindowMinutes({ ...base, windowMinutes: 120 })).toBe(120);
  });

  it("writes no band for an interval pattern, which always fires exactly", () => {
    expect(
      effectiveWindowMinutes({
        ...base,
        windowMinutes: 120,
        time: { kind: "every", interval: 2, unit: "hours", window: null, minute: 0 },
      }),
    ).toBe(0);
  });

  it("keeps the band of an advanced expression — the server spreads it too", () => {
    expect(
      effectiveWindowMinutes({
        ...base,
        windowMinutes: 120,
        time: { kind: "every", interval: 2, unit: "hours", window: null, minute: 0 },
        raw: "0 8 * * 1-5",
      }),
    ).toBe(120);
  });
});
