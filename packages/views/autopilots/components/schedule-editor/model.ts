export type TimePattern =
  | { kind: "at"; time: string } // "HH:MM"
  | {
      kind: "every";
      interval: number; // hours: 1-23, minutes: 1-59
      unit: "minutes" | "hours";
      // null = all day. For "hours" the window's from-minute is the firing
      // minute and `to` carries the same minute; for "minutes" the window is
      // hour-granular (from :00 to :59).
      window: { from: string; to: string } | null;
      // Firing minute for "hours" patterns ("every N hours at :M"), kept in
      // sync with window.from's minute when a window is set. Carried but
      // unused for "minutes" patterns (toCron ignores it there) so toggling
      // the unit back and forth does not discard the user's minute.
      minute: number;
    };

export type DayPattern =
  | { kind: "every" }
  | { kind: "weekly"; daysOfWeek: number[] } // 0=Sun … 6=Sat, non-empty, deduped, ascending
  | { kind: "monthly"; dayOfMonth: number }; // 1-31

export interface ScheduleConfig {
  time: TimePattern;
  days: DayPattern;
  // "Sometime between 08:00 and 10:00": with an "at" time, spread the firing
  // over this many minutes after it (server picks the minute, differently
  // each day). 0 = exactly at the time. Ignored for "every" patterns.
  windowMinutes: number;
  timezone: string; // IANA
  // Non-null when the expression exceeds the structured model: the editor is
  // in advanced-only mode and this exact string round-trips verbatim.
  raw: string | null;
}

// Timezone lists live in views/common/timezone-select (browserTimezone /
// timezoneOptions) — the caller passes the resolved zone in so this module
// stays free of platform lookups.
export function getDefaultScheduleConfig(timezone: string): ScheduleConfig {
  return {
    time: { kind: "at", time: "09:00" },
    days: { kind: "every" },
    windowMinutes: 0,
    timezone,
    raw: null,
  };
}

// Position is the cron day-of-week number, so this order is load-bearing: it
// indexes the locale's day names AND is what `daysOfWeek` holds.
export const DAY_KEYS = ["sun", "mon", "tue", "wed", "thu", "fri", "sat"] as const;

/** The maximal runs of consecutive days in an ascending set: [1,2,3,4,5] → one
 *  run 1–5, [0,1,2,4] → 0–2 and 4–4. What counts as a run worth collapsing is the
 *  caller's to decide — cron collapses any pair ("0-1"), the readback wants three
 *  before it says "Mon–Wed" — but where the runs ARE is one question with one
 *  answer, and both callers used to scan for them separately. */
export function consecutiveRuns(days: number[]): Array<[number, number]> {
  const sorted = Array.from(new Set(days)).toSorted((a, b) => a - b);
  const runs: Array<[number, number]> = [];
  let i = 0;
  while (i < sorted.length) {
    let j = i;
    while (j + 1 < sorted.length && sorted[j + 1] === sorted[j]! + 1) j++;
    runs.push([sorted[i]!, sorted[j]!]);
    i = j + 1;
  }
  return runs;
}

export function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

export function timeParts(time: string): { hour: number; minute: number } {
  const [h, m] = time.split(":");
  return { hour: parseInt(h ?? "0", 10), minute: parseInt(m ?? "0", 10) };
}

/** The band a config actually writes. A structured interval pattern ("every 2
 *  hours") fires exactly on its cron and has no band to spread; an advanced
 *  expression keeps whatever band the user set, because the server applies the
 *  band to that expression's occurrences just the same. */
export function effectiveWindowMinutes(config: ScheduleConfig): number {
  return config.raw !== null || config.time.kind === "at" ? config.windowMinutes : 0;
}

/** End of the firing band ("HH:MM") for an "at" time plus a window; wraps at midnight. */
export function windowEndTime(start: string, windowMinutes: number): string {
  const { hour, minute } = timeParts(start);
  const total = (hour * 60 + minute + windowMinutes) % (24 * 60);
  return `${pad2(Math.floor(total / 60))}:${pad2(total % 60)}`;
}

/** Minutes from `start` to `end` on the same day, 0 when end is not after start. */
export function windowMinutesBetween(start: string, end: string): number {
  const a = timeParts(start);
  const b = timeParts(end);
  const diff = b.hour * 60 + b.minute - (a.hour * 60 + a.minute);
  return diff > 0 ? diff : 0;
}
