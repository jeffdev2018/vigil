import { dateOnlyToUTCDate, todayDateOnly } from "../issues/date";
import type { Project } from "../types/project";

/**
 * Data shaping for the roadmap project timeline (JEF-247).
 *
 * Pure and DOM-free: rows carry UTC-midnight Dates (via dateOnlyToUTCDate, so
 * "YYYY-MM-DD" never shifts with the viewer's timezone) and the caller turns
 * them into pixels with dateToOffset. A project with only one of start/due
 * gets a one-day marker; a project with neither is unscheduled and has no row.
 */

const MS_PER_DAY = 24 * 60 * 60 * 1000;

export interface RoadmapTimelineRow {
  project: Project;
  startDate: Date;
  dueDate: Date;
  /** done_count / issue_count clamped to [0, 1]; 0 when the project has no issues. */
  progress: number;
}

export interface RoadmapTimeline {
  rangeStart: Date;
  rangeEnd: Date;
  /** Scheduled projects, sorted by startDate then dueDate. */
  rows: RoadmapTimelineRow[];
  /** Projects with neither start_date nor due_date. */
  unscheduled: Project[];
}

/**
 * Builds the timeline model from a project list. `today` anchors the range
 * (the range always covers it) and defaults to the viewer's local calendar
 * day — pass it explicitly in tests for determinism.
 */
export function buildRoadmapTimeline(projects: Project[], today?: Date): RoadmapTimeline {
  const anchor = today ?? dateOnlyToUTCDate(todayDateOnly()) ?? new Date();
  const rows: RoadmapTimelineRow[] = [];
  const unscheduled: Project[] = [];

  for (const project of projects) {
    const start = dateOnlyToUTCDate(project.start_date);
    const due = dateOnlyToUTCDate(project.due_date);
    if (!start && !due) {
      unscheduled.push(project);
      continue;
    }
    const inverted = start && due && start.getTime() > due.getTime();
    const startDate = start && due ? (inverted ? due : start) : (start ?? due)!;
    const dueDate = start && due ? (inverted ? start : due) : (start ?? due)!;
    rows.push({ project, startDate, dueDate, progress: projectProgress(project) });
  }

  rows.sort(
    (a, b) =>
      a.startDate.getTime() - b.startDate.getTime() ||
      a.dueDate.getTime() - b.dueDate.getTime(),
  );

  let min = anchor.getTime();
  let max = anchor.getTime();
  for (const row of rows) {
    min = Math.min(min, row.startDate.getTime());
    max = Math.max(max, row.dueDate.getTime());
  }
  return {
    rangeStart: new Date(min - MS_PER_DAY),
    rangeEnd: new Date(max + MS_PER_DAY),
    rows,
    unscheduled,
  };
}

function projectProgress(project: Project): number {
  if (project.issue_count <= 0) return 0;
  return Math.min(Math.max(project.done_count / project.issue_count, 0), 1);
}

/**
 * Position of `date` inside [rangeStart, rangeEnd] as a 0–1 fraction, clamped
 * so dates outside the range land on the edges instead of overflowing.
 */
export function dateToOffset(date: Date, rangeStart: Date, rangeEnd: Date): number {
  const span = rangeEnd.getTime() - rangeStart.getTime();
  if (span <= 0) return 0;
  const offset = (date.getTime() - rangeStart.getTime()) / span;
  return Math.min(Math.max(offset, 0), 1);
}
