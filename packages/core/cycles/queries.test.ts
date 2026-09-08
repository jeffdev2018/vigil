// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Cycle } from "../types";
import { capacityFill, cycleProgress, cyclesByStatus } from "./queries";

// Canonical matrix for the capacity math (F29). The cycle detail suite mounts
// one happy path and points here rather than re-running these cases in a DOM.

describe("capacityFill", () => {
  it("reports an undeclared capacity as undeclared, not as zero", () => {
    // A null capacity means nobody set one. Treating it as 0 would make every
    // cycle without a declared capacity render as permanently over it.
    expect(capacityFill({ capacity: null, load: 4 })).toEqual({
      declared: false,
      ratio: 0,
      over: false,
    });
  });

  it("treats a zero capacity as undeclared too", () => {
    // A zero-width bar can only ever render full or empty and would report
    // every load as an overflow. There is nothing useful to draw.
    expect(capacityFill({ capacity: 0, load: 3 }).declared).toBe(false);
  });

  it("fills proportionally below capacity", () => {
    expect(capacityFill({ capacity: 8, load: 2 })).toEqual({
      declared: true,
      ratio: 0.25,
      over: false,
    });
  });

  it("is exactly full, and not over, at capacity", () => {
    expect(capacityFill({ capacity: 5, load: 5 })).toEqual({
      declared: true,
      ratio: 1,
      over: false,
    });
  });

  it("clamps the bar at full but still reports the overflow", () => {
    // The bar cannot render past 100%, so `over` is what the label needs in
    // order to say how far past capacity the cycle actually is.
    expect(capacityFill({ capacity: 4, load: 10 })).toEqual({
      declared: true,
      ratio: 1,
      over: true,
    });
  });

  it("is empty at zero load", () => {
    expect(capacityFill({ capacity: 4, load: 0 })).toEqual({
      declared: true,
      ratio: 0,
      over: false,
    });
  });
});

describe("cycleProgress", () => {
  it("is zero for a cycle with no issues", () => {
    // Not NaN: an empty cycle divides by zero, and a NaN width silently drops
    // the bar rather than rendering it empty.
    expect(cycleProgress({ issue_count: 0, done_count: 0 })).toBe(0);
  });

  it("is the done ratio", () => {
    expect(cycleProgress({ issue_count: 4, done_count: 1 })).toBe(0.25);
  });

  it("clamps above one", () => {
    // Counts come from two aggregates; a stale pair must never overflow the bar.
    expect(cycleProgress({ issue_count: 2, done_count: 5 })).toBe(1);
  });
});

const cycle = (over: Partial<Cycle>): Cycle => ({
  id: "c", workspace_id: "ws", project_id: "p", name: "Cycle", description: "",
  start_date: "2026-03-01", end_date: "2026-03-14", rollover: true, closed_at: null,
  status: "active", late: false, load_unit: "issues", load_property_id: null,
  issue_count: 0, done_count: 0,
  capacity: {
    human: { capacity: null, load: 0 },
    agent: { capacity: null, load: 0 },
    unassigned_load: 0,
  },
  created_at: "", updated_at: "", ...over,
});

describe("cyclesByStatus", () => {
  it("keeps only the asked-for status and preserves the server's order", () => {
    const list = [
      cycle({ id: "a", status: "active" }),
      cycle({ id: "u", status: "upcoming" }),
      cycle({ id: "b", status: "active" }),
    ];
    expect(cyclesByStatus(list, "active").map((c) => c.id)).toEqual(["a", "b"]);
    expect(cyclesByStatus(list, "closed")).toEqual([]);
  });
});
