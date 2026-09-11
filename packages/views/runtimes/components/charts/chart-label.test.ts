// @vitest-environment node
import { describe, expect, it } from "vitest";
import { labelOf } from "./chart-label";

describe("labelOf", () => {
  const config = { input: { label: "Entrée", color: "var(--chart-1)" } };

  it("resolves a series name through the chart config", () => {
    expect(labelOf(config, "input")).toBe("Entrée");
  });

  it("falls back to the raw dataKey when the config has no entry for it", () => {
    expect(labelOf(config, "unknown_series")).toBe("unknown_series");
  });

  it("falls back to an empty string for an undefined name", () => {
    expect(labelOf(config, undefined)).toBe("");
  });
});
