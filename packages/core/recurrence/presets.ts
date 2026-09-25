// Recurring issues — the cron a person actually means. The server owns cron
// evaluation (GET /api/autopilots/cron-preview and the payload's `next_runs`);
// this file only builds the four expressions the preset radio can produce and
// reads one back, so a saved rule re-opens on the preset it was filed with
// instead of falling to "custom".

import type { IssueRecurrenceMode } from "../types";

export const RECURRENCE_PRESETS = ["daily", "weekdays", "weekly", "monthly", "custom", "on_close"] as const;
export type RecurrencePreset = (typeof RECURRENCE_PRESETS)[number];

/** Cron day-of-week, Sunday = 0 — the field's own numbering, not the UI's. */
export type RecurrenceWeekday = 0 | 1 | 2 | 3 | 4 | 5 | 6;

export const DEFAULT_RECURRENCE_HOUR = 9;
export const DEFAULT_RECURRENCE_MINUTE = 0;
export const DEFAULT_RECURRENCE_WEEKDAY: RecurrenceWeekday = 1;
export const DEFAULT_RECURRENCE_MONTH_DAY = 1;

function clamp(n: number, min: number, max: number): number {
  if (!Number.isFinite(n)) return min;
  return Math.min(max, Math.max(min, Math.trunc(n)));
}

export function dailyCron(hour: number, minute: number): string {
  return `${clamp(minute, 0, 59)} ${clamp(hour, 0, 23)} * * *`;
}

export function weekdaysCron(hour: number, minute: number): string {
  return `${clamp(minute, 0, 59)} ${clamp(hour, 0, 23)} * * 1-5`;
}

export function weeklyCron(weekday: number, hour: number, minute: number): string {
  return `${clamp(minute, 0, 59)} ${clamp(hour, 0, 23)} * * ${clamp(weekday, 0, 6)}`;
}

export function monthlyCron(day: number, hour: number, minute: number): string {
  return `${clamp(minute, 0, 59)} ${clamp(hour, 0, 23)} ${clamp(day, 1, 31)} * *`;
}

/** "9:5" reads as "09:05" wherever a rule is shown; minutes are never bare. */
export function formatRecurrenceClock(hour: number, minute: number): string {
  return `${String(clamp(hour, 0, 23)).padStart(2, "0")}:${String(clamp(minute, 0, 59)).padStart(2, "0")}`;
}

/**
 * What a saved rule says, in parts the view can translate. `custom` carries
 * the expression verbatim because nothing else can be said about it truthfully
 * — the five locales render it as "Custom (0 9 * * 1)".
 */
export type RecurrenceDescriptor =
  | { kind: "on_close" }
  | { kind: "daily"; hour: number; minute: number }
  | { kind: "weekdays"; hour: number; minute: number }
  | { kind: "weekly"; hour: number; minute: number; weekday: RecurrenceWeekday }
  | { kind: "monthly"; hour: number; minute: number; day: number }
  | { kind: "custom"; cron: string };

function plainInt(field: string, min: number, max: number): number | null {
  if (!/^\d{1,2}$/.test(field)) return null;
  const n = Number(field);
  return n >= min && n <= max ? n : null;
}

/**
 * The preset a cron came from, or `custom`. Only the shapes the builders above
 * emit are recognised: a list ("0 9 * * 1,3") or a step ("every five minutes")
 * is a real expression the radio cannot represent, and pretending otherwise would rewrite the user's
 * rule on the next save.
 */
export function describeRecurrenceCron(cron: string): RecurrenceDescriptor {
  const fields = cron.trim().split(/\s+/);
  const custom: RecurrenceDescriptor = { kind: "custom", cron: cron.trim() };
  if (fields.length !== 5) return custom;
  const [minField = "", hourField = "", domField = "", monField = "", dowField = ""] = fields;
  const minute = plainInt(minField, 0, 59);
  const hour = plainInt(hourField, 0, 23);
  if (minute === null || hour === null || monField !== "*") return custom;

  if (domField === "*") {
    if (dowField === "*") return { kind: "daily", hour, minute };
    if (dowField === "1-5") return { kind: "weekdays", hour, minute };
    const weekday = plainInt(dowField, 0, 6);
    if (weekday !== null) return { kind: "weekly", hour, minute, weekday: weekday as RecurrenceWeekday };
    return custom;
  }
  if (dowField === "*") {
    const day = plainInt(domField, 1, 31);
    if (day !== null) return { kind: "monthly", hour, minute, day };
  }
  return custom;
}

/** Mode first: an `on_close` rule has no cron to read. */
export function describeRecurrence(mode: IssueRecurrenceMode, cron: string): RecurrenceDescriptor {
  if (mode === "on_close") return { kind: "on_close" };
  return describeRecurrenceCron(cron);
}

export function recurrencePresetOf(mode: IssueRecurrenceMode, cron: string): RecurrencePreset {
  return describeRecurrence(mode, cron).kind;
}

/** The browser's zone, so the dialog opens on the clock the user reads. */
export function localTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}
