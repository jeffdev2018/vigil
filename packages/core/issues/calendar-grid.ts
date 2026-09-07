import { dateOnlyToUTCDate } from "./date";

/**
 * The month grid behind the calendar view (F30 / JEF-34).
 *
 * Everything here is UTC-day-aligned, for the same reason the Gantt's axis is:
 * `due_date` is a CALENDAR DAY transported as "YYYY-MM-DD", so anchoring it to
 * local midnight would put an issue due Mar 1 into the Feb 28 cell for every
 * viewer west of UTC. The rule is "parse UTC, format with timeZone: UTC", and
 * `dateOnlyToUTCDate` is the one parser allowed in this file.
 *
 * The grid is ALWAYS 42 cells (6 weeks × 7 days). A fixed shape means the
 * calendar does not change height between months, which is what makes paging
 * through it readable — and it removes the "does February need 5 or 6 rows"
 * branch entirely. Cells outside the month are marked `inMonth: false` and
 * still hold their issues: an issue due Mar 1 shown in February's trailing
 * cell is information, not noise.
 *
 * Pure (no React, no DOM, no `new Date()` on a local clock in the layout path)
 * so the whole thing is testable without a browser and shareable with mobile.
 */

const MS_PER_DAY = 24 * 60 * 60 * 1000;
export const CALENDAR_CELL_COUNT = 42;

export interface CalendarMonth {
  /** 1-12. */
  month: number;
  year: number;
}

export interface CalendarCell<T> {
  /** "YYYY-MM-DD" of this cell's day, UTC. */
  date: string;
  /** UTC midnight of this cell's day. */
  utcDate: Date;
  /** 0 = Sunday … 6 = Saturday, in UTC. */
  weekday: number;
  /** False for the leading/trailing days that belong to a neighbouring month. */
  inMonth: boolean;
  isWeekend: boolean;
  isToday: boolean;
  items: T[];
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** "YYYY-MM-DD" of a UTC-anchored Date. */
export function utcDateKey(d: Date): string {
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
}

/** The month `delta` months away, normalized. `delta` may be negative. */
export function shiftMonth(month: CalendarMonth, delta: number): CalendarMonth {
  const zero = month.year * 12 + (month.month - 1) + delta;
  return { year: Math.floor(zero / 12), month: (((zero % 12) + 12) % 12) + 1 };
}

/**
 * The month containing `today`, read in UTC.
 *
 * UTC, not local: the grid buckets issues by their UTC calendar day, so opening
 * the calendar at the LOCAL month would land a viewer in UTC+13 on a month
 * whose first cell is already yesterday's UTC day — a one-day-wide disagreement
 * between "the month you are looking at" and "the day an issue is filed under".
 */
export function currentCalendarMonth(today: Date = new Date()): CalendarMonth {
  return { year: today.getUTCFullYear(), month: today.getUTCMonth() + 1 };
}

/**
 * Builds the 42-cell grid for `month`, distributing `items` by the
 * "YYYY-MM-DD" that `dateOf` returns for each.
 *
 * `weekStartsOn` is 0 (Sunday) by default; 1 gives a Monday-first grid.
 * Items whose date is unparseable, absent, or outside the rendered window are
 * dropped — the grid renders a month, and an item with no day has no cell.
 */
export function buildCalendarGrid<T>(
  month: CalendarMonth,
  items: T[],
  dateOf: (item: T) => string | null | undefined,
  options: { today?: Date | null; weekStartsOn?: 0 | 1 } = {},
): CalendarCell<T>[] {
  const weekStartsOn = options.weekStartsOn ?? 0;
  const firstOfMonth = new Date(Date.UTC(month.year, month.month - 1, 1));
  const leading = (firstOfMonth.getUTCDay() - weekStartsOn + 7) % 7;
  const gridStart = new Date(firstOfMonth.getTime() - leading * MS_PER_DAY);

  const todayKey = options.today ? utcDateKey(options.today) : null;

  // One bucket pass, not one filter per cell: a month of 500 issues would
  // otherwise be 21,000 comparisons.
  const byDay = new Map<string, T[]>();
  for (const item of items) {
    const parsed = dateOnlyToUTCDate(dateOf(item));
    if (!parsed) continue;
    const key = utcDateKey(parsed);
    const bucket = byDay.get(key);
    if (bucket) bucket.push(item);
    else byDay.set(key, [item]);
  }

  const cells: CalendarCell<T>[] = [];
  for (let i = 0; i < CALENDAR_CELL_COUNT; i++) {
    const utcDate = new Date(gridStart.getTime() + i * MS_PER_DAY);
    const date = utcDateKey(utcDate);
    const weekday = utcDate.getUTCDay();
    cells.push({
      date,
      utcDate,
      weekday,
      inMonth: utcDate.getUTCMonth() === month.month - 1 && utcDate.getUTCFullYear() === month.year,
      isWeekend: weekday === 0 || weekday === 6,
      isToday: todayKey === date,
      items: byDay.get(date) ?? [],
    });
  }
  return cells;
}

/** True when no cell inside the month itself holds an item — the state the
 *  calendar has to name explicitly rather than showing an unexplained empty
 *  grid. Trailing cells from the neighbouring months do not count: they are
 *  context, not this month's content. */
export function calendarMonthIsEmpty<T>(cells: CalendarCell<T>[]): boolean {
  return cells.every((cell) => !cell.inMonth || cell.items.length === 0);
}
