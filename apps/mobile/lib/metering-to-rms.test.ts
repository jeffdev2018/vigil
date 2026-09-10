// @vitest-environment node
import { describe, expect, it } from "vitest";
import { meteringToRms } from "./metering-to-rms";

describe("meteringToRms", () => {
  it("returns 0 for missing or non-finite metering", () => {
    expect(meteringToRms(undefined)).toBe(0);
    expect(meteringToRms(null)).toBe(0);
    expect(meteringToRms(Number.NaN)).toBe(0);
  });

  it("maps 0 dB to full amplitude and −∞-ish silence toward 0", () => {
    expect(meteringToRms(0)).toBe(1);
    expect(meteringToRms(-60)).toBeCloseTo(0.001, 5);
  });

  it("places typical speech (~−34 dB) near the web VAD speech threshold", () => {
    expect(meteringToRms(-34)).toBeCloseTo(0.02, 2);
  });
});
