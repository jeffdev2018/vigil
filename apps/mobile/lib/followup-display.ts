/**
 * Scheduled wake-ups (JEF-373) — pure helpers for the issue's Follow-ups
 * section, its scheduling sheet and the agenda's wake-up rows.
 *
 * No web/desktop equivalent existed when this landed (the web side is being
 * built in parallel), so this mirrors the backend contract directly —
 * `server/internal/handler/followups.go` and
 * `server/internal/service/followup.go` — rather than a `packages/views`
 * component. Everything here is pure (no React, no RN) so the quick-choice
 * arithmetic is testable without a simulator.
 *
 * Timezone note: the quick choices are computed in the DEVICE's local
 * calendar ("tomorrow 9:00" means 9:00 where the person is standing) and
 * sent as an RFC 3339 instant with its offset, which is what the server
 * parses. The server's own ceiling is 1 minute … 30 days ahead.
 */

/** Server-side bounds, mirrored so the sheet can refuse before the round-trip. */
export const FOLLOWUP_MIN_LEAD_MS = 60_000;
export const FOLLOWUP_MAX_LEAD_MS = 30 * 24 * 60 * 60 * 1000;
export const FOLLOWUP_NOTE_MAX = 500;

/** RFC 3339 with the device's UTC offset, e.g. "2026-09-11T09:00:00+02:00". */
export function toRfc3339(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  const offsetMinutes = -date.getTimezoneOffset();
  const sign = offsetMinutes >= 0 ? "+" : "-";
  const abs = Math.abs(offsetMinutes);
  const offset =
    offsetMinutes === 0
      ? "Z"
      : `${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}` +
    `T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}${offset}`
  );
}

export type FollowupQuickChoiceId = "in_1h" | "tomorrow_9" | "next_monday_9";

export interface FollowupQuickChoice {
  id: FollowupQuickChoiceId;
  label: string;
  /** The instant to fire at, in the device's local calendar. */
  at: Date;
}

function atLocalTime(base: Date, addDays: number, hour: number): Date {
  return new Date(
    base.getFullYear(),
    base.getMonth(),
    base.getDate() + addDays,
    hour,
    0,
    0,
    0,
  );
}

/**
 * "Next Monday" is the Monday of the following week — never today even when
 * today IS Monday, because "come back to this next Monday" said on a Monday
 * morning means seven days out, not in a few hours. Same reading as the
 * autopilot draft prompt's "every Monday".
 */
export function nextMondayAt9(now: Date): Date {
  const daysUntilMonday = ((8 - now.getDay()) % 7) || 7;
  return atLocalTime(now, daysUntilMonday, 9);
}

/** The three one-tap choices, computed against `now` in the device timezone. */
export function followupQuickChoices(now: Date): FollowupQuickChoice[] {
  const tomorrow9 = atLocalTime(now, 1, 9);
  return [
    { id: "in_1h", label: "In 1 hour", at: new Date(now.getTime() + 3_600_000) },
    { id: "tomorrow_9", label: "Tomorrow, 9:00", at: tomorrow9 },
    { id: "next_monday_9", label: "Next Monday, 9:00", at: nextMondayAt9(now) },
  ];
}

/**
 * Why this instant can't be scheduled, or null when it can. Mirrors
 * `service.ScheduleFollowup`'s own window so the sheet doesn't spend a
 * round-trip to learn the user picked a time in the past.
 */
export function followupWhenError(at: Date, now: Date): string | null {
  const delta = at.getTime() - now.getTime();
  if (Number.isNaN(delta)) return "Pick a date and time.";
  if (delta < FOLLOWUP_MIN_LEAD_MS) return "Pick a time at least a minute from now.";
  if (delta > FOLLOWUP_MAX_LEAD_MS) return "A follow-up can be at most 30 days away.";
  return null;
}

export function followupNoteError(note: string): string | null {
  return note.length > FOLLOWUP_NOTE_MAX
    ? `The note is limited to ${FOLLOWUP_NOTE_MAX} characters.`
    : null;
}

/**
 * "in 3 h", "in 2 days", "tomorrow at 09:00" style lead-in for a pending
 * follow-up. Kept coarse on purpose: a wake-up 26 days out doesn't need a
 * minute count, and the absolute time is rendered next to it.
 */
export function followupRelativeTime(firesAt: string, now: Date): string {
  const at = new Date(firesAt);
  if (Number.isNaN(at.getTime())) return "";
  const ms = at.getTime() - now.getTime();
  if (ms <= 0) return "due now";
  const minutes = Math.round(ms / 60_000);
  if (minutes < 60) return `in ${minutes} min`;
  const hours = Math.round(ms / 3_600_000);
  if (hours < 24) return `in ${hours} h`;
  const days = Math.round(ms / 86_400_000);
  return days === 1 ? "in 1 day" : `in ${days} days`;
}

/** "Mon 11 Sep, 09:00" in the device's locale + timezone. */
export function followupAbsoluteTime(firesAt: string): string {
  const at = new Date(firesAt);
  if (Number.isNaN(at.getTime())) return "";
  return at.toLocaleString(undefined, {
    weekday: "short",
    day: "numeric",
    month: "short",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * Who put this wake-up on the calendar. `scheduled_by_type` is "member" or
 * "agent" on the wire; the default branch keeps an unknown future value
 * readable rather than dropping the line (root CLAUDE.md, server-driven
 * enums need a default).
 */
export function followupScheduledByLabel(
  followup: { scheduled_by_type: string; agent_name: string },
  memberName: string | null,
): string {
  switch (followup.scheduled_by_type) {
    case "member":
      return memberName ? `Scheduled by ${memberName}` : "Scheduled by a teammate";
    case "agent":
      return `Scheduled by ${followup.agent_name || "an agent"}`;
    default:
      return "Scheduled";
  }
}

/**
 * The message the scheduling sheet shows inline. A 429 is the daily budget
 * — the server's own sentence names which ceiling was hit (per agent or per
 * workspace), so it is shown verbatim rather than reworded. A 400 is the
 * window / missing-agent rule, also self-explanatory server-side.
 */
export function followupScheduleError(err: unknown): string {
  const status =
    err && typeof err === "object" && "status" in err
      ? (err as { status?: unknown }).status
      : undefined;
  const body = (err as { body?: unknown } | null)?.body;
  const serverMessage =
    body && typeof body === "object" && typeof (body as { error?: unknown }).error === "string"
      ? ((body as { error: string }).error)
      : "";
  if (status === 429) {
    return serverMessage || "This workspace has used up its follow-ups for today.";
  }
  if (serverMessage) return serverMessage;
  if (err instanceof Error && err.message) return err.message;
  return "Could not schedule the follow-up. Try again in a moment.";
}

/** Cancel refuses a follow-up that already fired (409) or is gone (404). */
export function followupCancelError(err: unknown): [string, string] {
  const status =
    err && typeof err === "object" && "status" in err
      ? (err as { status?: unknown }).status
      : undefined;
  if (status === 409) {
    return [
      "Already done",
      "This follow-up already fired or was cancelled. Pull to refresh the issue.",
    ];
  }
  if (status === 404) {
    return ["Not found", "This follow-up is no longer on the issue."];
  }
  return ["Could not cancel", followupScheduleError(err)];
}

/**
 * A 503 from draft/propose is not the user's fault: no model is wired up.
 * A 502 is the model answering badly, which is worth retrying.
 */
export function autopilotDraftError(err: unknown): string {
  const status =
    err && typeof err === "object" && "status" in err
      ? (err as { status?: unknown }).status
      : undefined;
  if (status === 503) {
    return "No model is configured to read a sentence. Create the autopilot from web or desktop instead.";
  }
  if (status === 502) {
    return "The model could not turn that into a schedule. Try rewording it.";
  }
  return followupScheduleError(err);
}

/** "Mon 14 Sep, 09:00" for each of the three preview runs. */
export function formatNextRuns(nextRuns: string[]): string[] {
  return nextRuns.map(followupAbsoluteTime).filter((s) => s !== "");
}
