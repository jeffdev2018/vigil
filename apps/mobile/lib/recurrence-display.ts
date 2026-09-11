/**
 * Recurring issues (OS plan, table stakes) — pure helpers for the issue's
 * Recurrence section and its editing sheet.
 *
 * Mirrors the backend contract directly — `server/internal/handler/
 * issue_recurrence.go` and `server/internal/service/{recurrence,cron}.go` —
 * the same way `followup-display.ts` mirrors `followups.go`. Everything here
 * is pure (no React, no RN) so the preset arithmetic and the cron sentence
 * are testable without a simulator.
 *
 * Two halves:
 *   - presets → cron (what the sheet writes)
 *   - cron → sentence / cron → preset (what the section reads, and what
 *     re-seeds the sheet when the user edits an existing rule)
 *
 * The cron validator is a pre-flight, not the authority: `robfig/cron`'s
 * standard 5-field parser server-side is, and its refusal comes back as a
 * 400 the sheet prints verbatim. This one only saves the round-trip for the
 * obvious typos (wrong field count, a minute of 90).
 */
import { followupAbsoluteTime } from "@/lib/followup-display";

export const RECURRENCE_MODE_SCHEDULE = "schedule";
export const RECURRENCE_MODE_ON_CLOSE = "on_close";

/** Sunday-first, matching cron's day-of-week numbering (0 = Sunday). */
export const WEEKDAY_NAMES = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
] as const;

export type RecurrencePresetId =
  | "daily"
  | "weekdays"
  | "weekly"
  | "monthly"
  | "custom"
  | "on_close";

export const RECURRENCE_PRESETS: { id: RecurrencePresetId; label: string }[] = [
  { id: "daily", label: "Every day" },
  { id: "weekdays", label: "Weekdays" },
  { id: "weekly", label: "Every week" },
  { id: "monthly", label: "Every month" },
  { id: "custom", label: "Custom cron" },
  { id: "on_close", label: "When closed" },
];

/** Everything the sheet holds; `presetToCron` collapses it to one expression. */
export interface RecurrenceDraft {
  preset: RecurrencePresetId;
  /** Local wall-clock the cron fires at, read in `timezone`. */
  hour: number;
  minute: number;
  /** 0 = Sunday … 6 = Saturday. Only read by the `weekly` preset. */
  weekday: number;
  /** 1…31. Only read by the `monthly` preset. */
  monthDay: number;
  /** Raw expression for the `custom` preset. */
  customCron: string;
}

export const DEFAULT_RECURRENCE_DRAFT: RecurrenceDraft = {
  preset: "weekly",
  hour: 9,
  minute: 0,
  weekday: 1,
  monthDay: 1,
  customCron: "0 9 * * 1",
};

/** The mode a draft writes. `on_close` is the only non-scheduled one. */
export function presetToMode(preset: RecurrencePresetId): string {
  return preset === "on_close"
    ? RECURRENCE_MODE_ON_CLOSE
    : RECURRENCE_MODE_SCHEDULE;
}

/**
 * The 5-field cron a draft writes, or "" for `on_close` — which is what the
 * server expects, since it only requires a cron for mode "schedule".
 */
export function presetToCron(draft: RecurrenceDraft): string {
  const { hour, minute } = draft;
  switch (draft.preset) {
    case "daily":
      return `${minute} ${hour} * * *`;
    case "weekdays":
      return `${minute} ${hour} * * 1-5`;
    case "weekly":
      return `${minute} ${hour} * * ${draft.weekday}`;
    case "monthly":
      return `${minute} ${hour} ${draft.monthDay} * *`;
    case "custom":
      return draft.customCron.trim();
    case "on_close":
      return "";
    default:
      return draft.customCron.trim();
  }
}

/**
 * Re-seeds the sheet from a saved rule so "Edit" opens on the preset the
 * user picked rather than dropping every rule into the custom-cron box.
 * Anything that isn't one of the four preset shapes IS custom — that's the
 * honest answer, not a failure.
 */
export function cronToDraft(
  cronExpression: string,
  mode: string,
): RecurrenceDraft {
  if (mode === RECURRENCE_MODE_ON_CLOSE) {
    return { ...DEFAULT_RECURRENCE_DRAFT, preset: "on_close" };
  }
  const cron = cronExpression.trim();
  const fields = cron.split(/\s+/);
  const custom: RecurrenceDraft = {
    ...DEFAULT_RECURRENCE_DRAFT,
    preset: "custom",
    customCron: cron,
  };
  if (fields.length !== 5) return custom;
  const [min, hr, dom, month, dow] = fields;
  const minute = plainNumber(min);
  const hour = plainNumber(hr);
  if (minute === null || hour === null || month !== "*") return custom;
  const base = { ...custom, hour, minute };
  if (dom === "*" && dow === "*") return { ...base, preset: "daily" };
  if (dom === "*" && dow === "1-5") return { ...base, preset: "weekdays" };
  if (dom === "*") {
    const weekday = plainNumber(dow);
    if (weekday !== null && weekday >= 0 && weekday <= 6) {
      return { ...base, preset: "weekly", weekday };
    }
    return custom;
  }
  if (dow === "*") {
    const monthDay = plainNumber(dom);
    if (monthDay !== null && monthDay >= 1 && monthDay <= 31) {
      return { ...base, preset: "monthly", monthDay };
    }
  }
  return custom;
}

/** "09:00" from the two numbers the time picker produced. */
export function formatClock(hour: number, minute: number): string {
  return `${String(hour).padStart(2, "0")}:${String(minute).padStart(2, "0")}`;
}

/**
 * The rule in words. Falls back to the raw expression for a cron this
 * doesn't recognise — better an expression the user typed than a wrong
 * sentence about it.
 */
export function cronSentence(cronExpression: string): string {
  const fields = cronExpression.trim().split(/\s+/);
  if (fields.length !== 5) return cronExpression.trim();
  const [min, hr, dom, month, dow] = fields;
  const minute = plainNumber(min);
  const hour = plainNumber(hr);
  if (minute === null || hour === null || month !== "*") {
    return cronExpression.trim();
  }
  const at = ` at ${formatClock(hour, minute)}`;
  if (dom === "*" && dow === "*") return `Every day${at}`;
  if (dom === "*" && dow === "1-5") return `Every weekday${at}`;
  if (dom === "*") {
    const weekday = plainNumber(dow);
    if (weekday !== null && weekday >= 0 && weekday <= 6) {
      return `Every ${WEEKDAY_NAMES[weekday]}${at}`;
    }
    return cronExpression.trim();
  }
  if (dow === "*") {
    const monthDay = plainNumber(dom);
    if (monthDay !== null && monthDay >= 1 && monthDay <= 31) {
      return `On the ${ordinal(monthDay)} of each month${at}`;
    }
  }
  return cronExpression.trim();
}

/**
 * What the section prints under "Recurrence":
 * "Every Monday at 09:00 · Europe/Paris" or "When the current one is closed".
 *
 * `mode` is an open server enum, so the switch has a default branch (root
 * CLAUDE.md, API compatibility) — an unknown mode reads as its cron rather
 * than as nothing at all.
 */
export function recurrenceSentence(recurrence: {
  mode: string;
  cron_expression: string;
  timezone: string;
}): string {
  switch (recurrence.mode) {
    case RECURRENCE_MODE_ON_CLOSE:
      return "When the current one is closed";
    case RECURRENCE_MODE_SCHEDULE:
      return withZone(cronSentence(recurrence.cron_expression), recurrence.timezone);
    default:
      return recurrence.cron_expression
        ? withZone(cronSentence(recurrence.cron_expression), recurrence.timezone)
        : "Recurring";
  }
}

function withZone(sentence: string, timezone: string): string {
  const tz = timezone.trim();
  return tz ? `${sentence} · ${tz}` : sentence;
}

const CRON_FIELD_NAMES = ["minute", "hour", "day of month", "month", "weekday"];
// robfig's standard 5-field parser (server/internal/service/cron.go).
// Weekday tops out at 6 — 7 is NOT an alias for Sunday there.
const CRON_FIELD_RANGES: [number, number][] = [
  [0, 59],
  [0, 23],
  [1, 31],
  [1, 12],
  [0, 6],
];

/**
 * Why this expression can't be sent, or null when it can. Descriptors
 * (`@daily`, `@every 1h`) are handed straight to the server: robfig accepts
 * a set this doesn't try to enumerate.
 */
export function cronError(expression: string): string | null {
  const expr = expression.trim();
  if (expr === "") return "Enter a cron expression.";
  if (expr.startsWith("@")) return null;
  const fields = expr.split(/\s+/);
  if (fields.length !== 5) {
    return "A cron expression has 5 fields: minute hour day month weekday.";
  }
  for (let i = 0; i < 5; i++) {
    if (!cronFieldOk(fields[i], CRON_FIELD_RANGES[i])) {
      return `"${fields[i]}" is not a valid ${CRON_FIELD_NAMES[i]}.`;
    }
  }
  return null;
}

function cronFieldOk(field: string, [min, max]: [number, number]): boolean {
  if (field === "") return false;
  return field.split(",").every((term) => {
    const parts = term.split("/");
    if (parts.length > 2) return false;
    const [range, step] = parts;
    if (step !== undefined && !/^[1-9]\d*$/.test(step)) return false;
    if (range === "*") return true;
    const bounds = range.split("-");
    if (bounds.length > 2) return false;
    const lo = plainNumber(bounds[0]);
    if (lo === null || lo < min || lo > max) return false;
    if (bounds.length === 1) return true;
    const hi = plainNumber(bounds[1]);
    return hi !== null && hi >= lo && hi <= max;
  });
}

/** A bare non-negative integer, or null for anything else ("*", "1-5", ""). */
function plainNumber(value: string | undefined): number | null {
  if (value === undefined || !/^\d+$/.test(value)) return null;
  return Number(value);
}

/** "1st" / "2nd" / "3rd" / "11th" / "21st". */
export function ordinal(n: number): string {
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

/** "Mon 14 Sep, 09:00" per run — the same formatter follow-ups use. */
export function formatRecurrenceRuns(nextRuns: string[]): string[] {
  return nextRuns.map(followupAbsoluteTime).filter((s) => s !== "");
}

/**
 * The message the sheet shows inline. 400 is the cron / timezone / mode
 * refusal and 403 is "only a member sets a recurrence" — both are
 * self-explanatory server-side, so they are shown verbatim rather than
 * reworded into something less precise.
 */
export function recurrenceSetError(err: unknown): string {
  const status =
    err && typeof err === "object" && "status" in err
      ? (err as { status?: unknown }).status
      : undefined;
  const body = (err as { body?: unknown } | null)?.body;
  const serverMessage =
    body &&
    typeof body === "object" &&
    typeof (body as { error?: unknown }).error === "string"
      ? (body as { error: string }).error
      : "";
  if (status === 403) {
    return serverMessage || "Only a member of this workspace can set a recurrence.";
  }
  if (serverMessage) return serverMessage;
  if (err instanceof Error && err.message) return err.message;
  return "Could not save the recurrence. Try again in a moment.";
}

/**
 * The device's IANA zone, which is what the sheet proposes. Falls back to UTC
 * — the server's own default — when Hermes has no Intl data for the build.
 */
export function deviceTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}
