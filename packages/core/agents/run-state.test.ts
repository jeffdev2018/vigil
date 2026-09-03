// @vitest-environment node
import { describe, expect, it } from "vitest";
import { normalizeRunState, runSilenceMs, runStateOf } from "./run-state";

// Canonical layer for the status matrix and the liveness threshold; the
// execution log test only checks the wiring (bucket + badge).

const T0 = Date.parse("2026-09-03T12:00:00Z");
const iso = (offsetSeconds: number) => new Date(T0 + offsetSeconds * 1000).toISOString();

describe("runStateOf", () => {
  it.each([
    ["queued", "pending"],
    ["deferred", "pending"],
    ["dispatched", "active"],
    ["running", "active"],
    ["waiting_local_directory", "blocked"],
    ["completed", "complete"],
    ["failed", "error"],
    ["cancelled", "cancelled"],
    ["something_new", "pending"],
    ["", "pending"],
  ])("maps %s to %s", (status, state) => {
    expect(runStateOf(status)).toBe(state);
  });
});

describe("normalizeRunState", () => {
  it("labels a running run unresponsive once its silence passes the threshold", () => {
    const run = { status: "running", last_activity_at: iso(-91) };
    expect(normalizeRunState(run, T0, 90)).toBe("unresponsive");
    expect(normalizeRunState({ ...run, last_activity_at: iso(-89) }, T0, 90)).toBe("active");
  });

  it("honours a deployment threshold override", () => {
    const run = { status: "running", last_activity_at: iso(-6) };
    expect(normalizeRunState(run, T0, 5)).toBe("unresponsive");
    expect(normalizeRunState(run, T0)).toBe("active");
  });

  it("never marks pending, blocked or settled runs unresponsive", () => {
    for (const status of ["queued", "deferred", "waiting_local_directory", "completed", "failed", "cancelled"]) {
      expect(normalizeRunState({ status, last_activity_at: iso(-3600) }, T0, 90)).toBe(runStateOf(status));
    }
  });

  it("falls back to started_at then dispatched_at when no activity was stamped", () => {
    expect(normalizeRunState({ status: "running", started_at: iso(-100) }, T0, 90)).toBe("unresponsive");
    expect(normalizeRunState({ status: "dispatched", dispatched_at: iso(-100) }, T0, 90)).toBe("unresponsive");
    expect(normalizeRunState({ status: "dispatched", dispatched_at: iso(-10) }, T0, 90)).toBe("active");
  });

  it("is never unresponsive without any anchor (older server, no timestamps)", () => {
    expect(normalizeRunState({ status: "running" }, T0, 90)).toBe("active");
    expect(normalizeRunState({ status: "running", last_activity_at: null, started_at: null }, T0, 90)).toBe("active");
  });

  it("clamps a client clock that runs behind the server to zero silence", () => {
    expect(runSilenceMs({ status: "running", last_activity_at: iso(+30) }, T0)).toBe(0);
    expect(normalizeRunState({ status: "running", last_activity_at: iso(+30) }, T0, 90)).toBe("active");
  });

  it("recovers as soon as a newer activity stamp arrives", () => {
    const silent = { status: "running", last_activity_at: iso(-200) };
    expect(normalizeRunState(silent, T0, 90)).toBe("unresponsive");
    expect(normalizeRunState({ ...silent, last_activity_at: iso(-1) }, T0, 90)).toBe("active");
  });
});
