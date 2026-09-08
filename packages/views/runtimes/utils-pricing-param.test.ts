// @vitest-environment node
//
// Covers the explicit `pricings` parameter added to `estimateCost` (see
// `resolvePricing` / `PricingOverrides` in ./utils). This is a pure
// function test — no DOM, no Zustand store — so it runs under node instead
// of the package's default jsdom environment.
import { describe, it, expect } from "vitest";
import { estimateCost } from "./utils";

describe("estimateCost with an explicit pricings map", () => {
  const usage = {
    model: "totally-unmapped-model",
    input_tokens: 1_000_000,
    output_tokens: 0,
    cache_read_tokens: 0,
    cache_write_tokens: 0,
  };

  it("prices the same usage differently for two different custom-rate maps", () => {
    const cheap = estimateCost(usage, {
      "totally-unmapped-model": { input: 1, output: 2, cacheRead: 0.1, cacheWrite: 1 },
    });
    const expensive = estimateCost(usage, {
      "totally-unmapped-model": { input: 5, output: 10, cacheRead: 0.5, cacheWrite: 5 },
    });

    expect(cheap).toBeCloseTo(1, 5);
    expect(expensive).toBeCloseTo(5, 5);
    expect(cheap).not.toBe(expensive);
  });

  it("falls back to $0 for an unmapped model when no override matches", () => {
    expect(estimateCost(usage, {})).toBe(0);
  });
});
