// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildGanttArrows, ganttBarGeometry, type GanttBarGeometry } from "./gantt-arrows";
import type { IssueDependencyEdge } from "../types";

// The arrow layer's geometry contract. The component suite keeps the "one
// arrow per blocks dependency" wiring and points back here for the matrix.

const RANGE_START = new Date(Date.UTC(2026, 2, 1));
const TOTAL_DAYS = 40;
const OPTS = { dayPx: 10, rowHeight: 36 };

function edge(id: string, from: string, to: string, type = "blocks"): IssueDependencyEdge {
  return { id, from, to, type };
}

describe("ganttBarGeometry", () => {
  it("maps a start/due pair to inclusive day offsets", () => {
    const bar = ganttBarGeometry(
      { start_date: "2026-03-03", due_date: "2026-03-05" },
      0,
      RANGE_START,
      TOTAL_DAYS,
    );
    // Mar 3 is day 2; the bar covers through Mar 5, so it ends at day 5.
    expect(bar).toEqual({ row: 0, startDay: 2, endDay: 5 });
  });

  it("gives a one-day bar to an issue carrying only one date", () => {
    expect(ganttBarGeometry({ due_date: "2026-03-04" }, 1, RANGE_START, TOTAL_DAYS)).toEqual({
      row: 1,
      startDay: 3,
      endDay: 4,
    });
  });

  it("normalizes inverted dates the way the row draws them", () => {
    // start > due is a data anomaly the row draws min..max; the arrow has to
    // land on the bar that is actually on screen, not on the literal pair.
    const bar = ganttBarGeometry(
      { start_date: "2026-03-08", due_date: "2026-03-04" },
      0,
      RANGE_START,
      TOTAL_DAYS,
    );
    expect(bar).toEqual({ row: 0, startDay: 3, endDay: 8 });
  });

  it("has no geometry for an undated issue", () => {
    expect(ganttBarGeometry({}, 0, RANGE_START, TOTAL_DAYS)).toBeNull();
    expect(
      ganttBarGeometry({ start_date: null, due_date: null }, 0, RANGE_START, TOTAL_DAYS),
    ).toBeNull();
  });

  it("clamps a bar that runs past the drawn window", () => {
    const bar = ganttBarGeometry(
      { start_date: "2026-03-30", due_date: "2026-06-01" },
      0,
      RANGE_START,
      TOTAL_DAYS,
    );
    expect(bar?.endDay).toBe(TOTAL_DAYS);
  });
});

describe("buildGanttArrows", () => {
  const geometry = new Map<string, GanttBarGeometry>([
    ["a", { row: 0, startDay: 2, endDay: 5 }],
    ["b", { row: 3, startDay: 7, endDay: 10 }],
  ]);

  it("draws one arrow per blocks dependency", () => {
    const arrows = buildGanttArrows([edge("e1", "a", "b")], geometry, OPTS);
    expect(arrows).toHaveLength(1);
    expect(arrows[0]!.id).toBe("e1");
    // Leaves the blocker's right edge (day 5 -> x 50) at its row centre
    // (row 0 -> y 18) and arrives at the blocked bar's left edge (day 7 ->
    // x 70) at row 3's centre (y 126).
    expect(arrows[0]!.path.startsWith("M 50 18")).toBe(true);
    expect(arrows[0]!.headX).toBe(70);
    expect(arrows[0]!.headY).toBe(126);
  });

  it("ignores related edges, which carry no direction", () => {
    expect(buildGanttArrows([edge("e1", "a", "b", "related")], geometry, OPTS)).toHaveLength(0);
  });

  // Acceptance 9: only DATED issues get an arrow. An edge pointing at a bar
  // that is not on the canvas has nothing to connect to.
  it("skips an edge whose either end has no bar", () => {
    expect(buildGanttArrows([edge("e1", "a", "missing")], geometry, OPTS)).toHaveLength(0);
    expect(buildGanttArrows([edge("e1", "missing", "b")], geometry, OPTS)).toHaveLength(0);
  });

  it("routes around instead of doubling back when the target starts first", () => {
    // A schedule that already violates its own dependency: the blocked issue
    // starts before its blocker ends. The elbow must not be placed inside the
    // bars, or the path draws back through them.
    const overlapping = new Map<string, GanttBarGeometry>([
      ["a", { row: 0, startDay: 5, endDay: 20 }],
      ["b", { row: 1, startDay: 2, endDay: 6 }],
    ]);
    const [arrow] = buildGanttArrows([edge("e1", "a", "b")], overlapping, OPTS);
    // x1 = 200, x2 = 20: the elbow sits BEFORE the target (20 - stub 8 = 12),
    // not after the source, so the path never crosses back through the bars.
    expect(arrow!.path).toBe("M 200 18 H 12 V 54 H 20");
  });

  it("returns nothing for an empty edge list", () => {
    expect(buildGanttArrows([], geometry, OPTS)).toEqual([]);
  });
});
