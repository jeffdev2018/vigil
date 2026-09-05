import { describe, expect, it } from "vitest";
import {
  TRIAGE_STATES,
  ageSecondsToIso,
  formatTriagePayload,
  triageEmptyMessage,
  triageStateLabel,
} from "./triage-display";

describe("TRIAGE_STATES", () => {
  it("lists the four server states in web's order", () => {
    expect(TRIAGE_STATES).toEqual([
      "pending",
      "accepted",
      "dismissed",
      "merged",
    ]);
  });
});

describe("triageStateLabel", () => {
  it("labels every known state", () => {
    expect(TRIAGE_STATES.map(triageStateLabel)).toEqual([
      "Pending",
      "Accepted",
      "Dismissed",
      "Merged",
    ]);
  });

  it("falls back to the raw value for a state the server added later", () => {
    expect(triageStateLabel("superseded")).toBe("superseded");
  });
});

describe("ageSecondsToIso", () => {
  it("maps an age in seconds onto a past timestamp", () => {
    const iso = ageSecondsToIso(3600);
    const delta = Date.now() - new Date(iso).getTime();
    expect(delta).toBeGreaterThanOrEqual(3_600_000 - 1000);
    expect(delta).toBeLessThanOrEqual(3_600_000 + 1000);
  });

  it("returns roughly now for a zero age", () => {
    expect(Math.abs(Date.now() - new Date(ageSecondsToIso(0)).getTime()))
      .toBeLessThan(1000);
  });
});

describe("formatTriagePayload", () => {
  it("pretty-prints a captured body", () => {
    expect(formatTriagePayload({ body: { a: 1 } })).toBe('{\n  "a": 1\n}');
  });

  it("returns null when there is no body", () => {
    expect(formatTriagePayload({})).toBeNull();
    expect(formatTriagePayload(undefined)).toBeNull();
    expect(formatTriagePayload({ body: {} })).toBeNull();
  });

  it("returns null when the body cannot be serialised", () => {
    const cyclic: Record<string, unknown> = {};
    cyclic.self = cyclic;
    expect(formatTriagePayload({ body: cyclic })).toBeNull();
  });

  it("returns null for a truncated capture so the caller can say so", () => {
    expect(formatTriagePayload({ truncated: true, size: 9_000_000 })).toBeNull();
  });
});

describe("triageEmptyMessage", () => {
  it("only says the queue is clear for the pending bucket", () => {
    expect(triageEmptyMessage("pending")).toContain("Queue is clear");
    for (const state of ["accepted", "dismissed", "merged"] as const) {
      expect(triageEmptyMessage(state)).not.toContain("Queue is clear");
    }
  });
});
