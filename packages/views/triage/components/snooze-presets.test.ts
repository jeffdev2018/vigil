// @vitest-environment node
import { describe, expect, it } from "vitest";
import { customSnoozeIso, resolveSnoozePreset } from "./snooze-presets";

// Canonical matrix for the snooze offsets; the component suite only checks
// that the menu wires a preset through to the mutation.
describe("resolveSnoozePreset", () => {
  it("adds an hour", () => {
    const now = new Date("2026-03-04T10:15:00");
    expect(resolveSnoozePreset("hour", now).toISOString()).toBe(
      new Date("2026-03-04T11:15:00").toISOString(),
    );
  });

  it("uses tonight at 18:00 while it is still ahead", () => {
    const now = new Date("2026-03-04T10:00:00");
    const evening = resolveSnoozePreset("evening", now);
    expect(evening.getHours()).toBe(18);
    expect(evening.getDate()).toBe(4);
  });

  it("rolls this evening to tomorrow once 18:00 has passed", () => {
    const now = new Date("2026-03-04T19:30:00");
    const evening = resolveSnoozePreset("evening", now);
    expect(evening.getHours()).toBe(18);
    expect(evening.getDate()).toBe(5);
  });

  it("puts tomorrow at 09:00", () => {
    const now = new Date("2026-03-04T22:00:00");
    const tomorrow = resolveSnoozePreset("tomorrow", now);
    expect(tomorrow.getDate()).toBe(5);
    expect(tomorrow.getHours()).toBe(9);
  });

  it("skips to the following Monday when today is Monday", () => {
    const monday = new Date("2026-03-02T09:30:00");
    expect(monday.getDay()).toBe(1);
    const next = resolveSnoozePreset("next_monday", monday);
    expect(next.getDay()).toBe(1);
    expect(next.getDate()).toBe(9);
  });

  it("finds the coming Monday from mid-week", () => {
    const wednesday = new Date("2026-03-04T09:30:00");
    const next = resolveSnoozePreset("next_monday", wednesday);
    expect(next.getDay()).toBe(1);
    expect(next.getDate()).toBe(9);
  });
});

describe("customSnoozeIso", () => {
  const now = new Date("2026-03-04T10:00:00");

  it("accepts a future time inside the 30-day window", () => {
    expect(customSnoozeIso("2026-03-10T09:00", now)).toBe(
      new Date("2026-03-10T09:00").toISOString(),
    );
  });

  it("rejects the past, unparseable input, and beyond 30 days", () => {
    expect(customSnoozeIso("2026-03-04T09:00", now)).toBeNull();
    expect(customSnoozeIso("", now)).toBeNull();
    expect(customSnoozeIso("not a date", now)).toBeNull();
    expect(customSnoozeIso("2026-05-04T09:00", now)).toBeNull();
  });
});
