import { dateOnlyToUTCDate } from "./date";
import type { IssueDependencyEdge } from "../types";

/**
 * Geometry for the Gantt's dependency arrows (F30 / JEF-34).
 *
 * Pure and DOM-free: the arrow layer is one absolutely-positioned SVG over the
 * whole track, and every coordinate is derived from a row's INDEX and its
 * dates — never from a measured element. That is what lets the rows be
 * virtualized: an arrow between two bars neither of which is currently mounted
 * still draws in the right place, because nothing about it depends on the DOM.
 */

export interface GanttBarGeometry {
  /** Row index in the drawn order, 0-based. */
  row: number;
  /** Day offset of the bar's left edge from the timeline start. */
  startDay: number;
  /** Day offset of the bar's right edge (exclusive). */
  endDay: number;
}

export interface GanttArrow {
  id: string;
  /** SVG path in timeline pixel space (x = days * dayPx, y = rows * rowHeight). */
  path: string;
  /** Arrow head position, so the caller can render a marker without re-deriving it. */
  headX: number;
  headY: number;
}

export interface GanttArrowOptions {
  dayPx: number;
  rowHeight: number;
}

/**
 * Bar geometry for one issue, in the same coordinate system `ScheduledRow`
 * uses: day offsets from `rangeStart`, clamped to the drawn window.
 *
 * Returns null for an issue with no date at all — it has no bar, so nothing can
 * connect to it. An issue with only ONE of the two dates gets a one-day marker,
 * which is exactly what the row renders.
 */
export function ganttBarGeometry(
  issue: { start_date?: string | null; due_date?: string | null },
  row: number,
  rangeStart: Date,
  totalDays: number,
): GanttBarGeometry | null {
  const MS_PER_DAY = 24 * 60 * 60 * 1000;
  const start = dateOnlyToUTCDate(issue.start_date);
  const due = dateOnlyToUTCDate(issue.due_date);
  if (!start && !due) return null;
  // start > due is a data anomaly the row already normalizes to min/max; do the
  // same here so the arrow lands on the bar that is actually drawn.
  const inverted = start && due && start.getTime() > due.getTime();
  const from = start && due ? (inverted ? due : start) : (start ?? due)!;
  const to = start && due ? (inverted ? start : due) : (start ?? due)!;
  const startDay = Math.max(Math.round((from.getTime() - rangeStart.getTime()) / MS_PER_DAY), 0);
  const endDay = Math.min(
    Math.round((to.getTime() - rangeStart.getTime()) / MS_PER_DAY) + 1,
    totalDays,
  );
  if (endDay <= startDay) return null;
  return { row, startDay, endDay };
}

/**
 * Turns dependency edges into drawable arrows.
 *
 * Only `blocks` edges are drawn. `related` is symmetric and carries no
 * direction, so an arrow would assert an ordering the data does not have; the
 * detail panel lists those instead. An edge whose either end has no bar — an
 * undated issue, or one outside the drawn window — is skipped rather than
 * drawn to a guessed position.
 *
 * The path is a three-segment orthogonal elbow (out of the source's right edge,
 * across, down/up, into the target's left edge). Not a straight line: on a
 * dense chart straight diagonals cross every bar between the two rows and stop
 * being followable.
 */
export function buildGanttArrows(
  edges: IssueDependencyEdge[],
  geometryById: Map<string, GanttBarGeometry>,
  { dayPx, rowHeight }: GanttArrowOptions,
): GanttArrow[] {
  const arrows: GanttArrow[] = [];
  for (const edge of edges) {
    if (edge.type !== "blocks") continue;
    const from = geometryById.get(edge.from);
    const to = geometryById.get(edge.to);
    if (!from || !to) continue;

    const x1 = from.endDay * dayPx;
    const y1 = from.row * rowHeight + rowHeight / 2;
    const x2 = to.startDay * dayPx;
    const y2 = to.row * rowHeight + rowHeight / 2;

    // Elbow x: a short stub past the source, but never past the target's left
    // edge — when the target starts before the blocker ends (a schedule that
    // already violates its own dependency), the path routes AROUND rather than
    // doubling back through the bars.
    const stub = Math.max(8, dayPx / 2);
    const elbowX = x2 > x1 + stub ? x1 + stub : x2 - stub;
    arrows.push({
      id: edge.id,
      path: `M ${x1} ${y1} H ${elbowX} V ${y2} H ${x2}`,
      headX: x2,
      headY: y2,
    });
  }
  return arrows;
}
