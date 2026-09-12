/**
 * Native calendar (OS plan, chantier 19) — pure display helpers for the
 * mobile Agenda screen and the calendar-event detail sheet.
 *
 * No web/desktop equivalent exists yet to mirror (packages/views/ for this
 * feature is being built in a separate worktree in parallel — see
 * apps/docs/content/docs/calendar.mdx for the backend contract this mirrors
 * instead). Kept pure (no React, no DOM) so it's directly testable and easy
 * to reconcile with the web version once it lands.
 */
import type {
  AgendaCycle,
  AgendaIssue,
  AgendaMeeting,
  CalendarAgenda,
  CalendarEventEntry,
  CalendarParticipant,
  AgendaFollowup,
} from "@multica/core/types";


export interface AgendaDayGroup<T> {
  /** "YYYY-MM-DD" in the asked timezone. */
  date: string;
  items: T[];
}

/**
 * Group items into calendar days IN A GIVEN IANA TIMEZONE. An event's own
 * `timezone` field decides which day it lands in for its own row (matches
 * the docs: "Each day carries the workspace's events... in a timezone"),
 * not the viewer's device timezone — two people in different timezones
 * looking at the same agenda would otherwise see an event drift across the
 * midnight boundary differently, which would break the "same day" mental
 * model the agenda promises.
 */
export function dayKeyInTimezone(iso: string, timezone: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "invalid-date";
  try {
    // en-CA gives YYYY-MM-DD directly — no manual reassembly of parts.
    return new Intl.DateTimeFormat("en-CA", {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(date);
  } catch {
    // Unknown/invalid IANA zone (shouldn't happen — server validates on
    // write) — fall back to UTC rather than throwing on render.
    return new Intl.DateTimeFormat("en-CA", {
      timeZone: "UTC",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(date);
  }
}

/**
 * Group calendar events by their own day (`starts_at` read in the event's
 * `timezone`), sorted by day then by start time within the day.
 */
export function groupEventsByDay(
  events: CalendarEventEntry[],
): AgendaDayGroup<CalendarEventEntry>[] {
  const groups = new Map<string, CalendarEventEntry[]>();
  for (const event of events) {
    const key = dayKeyInTimezone(event.starts_at, event.timezone || "UTC");
    const bucket = groups.get(key) ?? [];
    bucket.push(event);
    groups.set(key, bucket);
  }
  const result: AgendaDayGroup<CalendarEventEntry>[] = [];
  for (const [date, items] of groups) {
    items.sort(
      (a, b) => new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime(),
    );
    result.push({ date, items });
  }
  return result.sort((a, b) => a.date.localeCompare(b.date));
}

/** "9:00 AM – 10:00 AM (America/New_York)", or "All day" for an all-day event. */
export function formatEventTimeRange(event: CalendarEventEntry): string {
  if (event.all_day) return "All day";
  const tz = event.timezone || "UTC";
  const fmt = new Intl.DateTimeFormat("en-US", {
    hour: "numeric",
    minute: "2-digit",
    timeZone: tz,
  });
  const start = fmt.format(new Date(event.starts_at));
  const end = fmt.format(new Date(event.ends_at));
  return `${start} – ${end}`;
}

/** Response label for a participant's own answer — mirrors the wire enum. */
export function responseLabel(response: CalendarParticipant["response"]): string {
  switch (response) {
    case "accepted":
      return "Accepted";
    case "declined":
      return "Declined";
    case "tentative":
      return "Tentative";
    case "pending":
      return "Pending";
    default:
      return response;
  }
}

/** Status label + whether it reads as a pending decision (proposed). */
export function statusLabel(status: CalendarEventEntry["status"]): string {
  switch (status) {
    case "proposed":
      return "Awaits a decision";
    case "scheduled":
      return "Scheduled";
    case "cancelled":
      return "Cancelled";
    default:
      return status;
  }
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** "YYYY-MM-DD" of a Date read in the VIEWER's local calendar (not UTC) —
 *  used for the Agenda screen's window boundaries, which page by the
 *  viewer's own "today", not a server timezone. */
export function localDateKey(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** `count` consecutive local calendar days starting at `start`, as
 *  "YYYY-MM-DD" strings. Used to render a fixed 14-day agenda window even
 *  for days that carry nothing. */
export function enumerateLocalDays(start: Date, count: number): string[] {
  const out: string[] = [];
  const cursor = new Date(start.getFullYear(), start.getMonth(), start.getDate());
  for (let i = 0; i < count; i++) {
    out.push(localDateKey(cursor));
    cursor.setDate(cursor.getDate() + 1);
  }
  return out;
}

export interface AgendaDay {
  /** "YYYY-MM-DD", local calendar day. */
  date: string;
  cycles: AgendaCycle[];
  events: CalendarEventEntry[];
  issuesDue: AgendaIssue[];
  meetings: AgendaMeeting[];
  /** Scheduled wake-ups of an issue's agent (JEF-373). */
  followups: AgendaFollowup[];
}

/**
 * Merge the four agenda sources (GET /api/calendar/agenda) into one
 * per-day list, ordered by `windowDates` (from `enumerateLocalDays`) so
 * every rendered day appears even when it carries nothing.
 *
 * Per-source day key:
 *   - events: the event's OWN `timezone` (see `dayKeyInTimezone` — matches
 *     what the day-of-week / day-of-month the event's own participants see).
 *   - issues_due: `due_date` is already a date-only "YYYY-MM-DD" — used as-is.
 *   - meetings: no per-item timezone on the wire, so grouped in the
 *     viewer's own device timezone (`deviceTimezone`).
 *   - cycles: span `start_date`..`end_date` (date-only) — attached to every
 *     window day inside that inclusive range, rendered as a "band".
 *   - followups: `fires_at` is an instant with no per-item timezone on the
 *     wire, so grouped in the viewer's own device timezone, like meetings.
 *
 * An item whose computed day falls outside `windowDates` (a boundary
 * rounding edge — e.g. an event a minute before local midnight in a
 * timezone far from the server's `from`/`to` cutoffs) is dropped rather
 * than growing a day the caller didn't ask to render; the agenda window is
 * refetched wide enough in practice (see more/calendar.tsx) that this
 * should not lose real data.
 */
export function buildAgendaDays(
  agenda: Pick<CalendarAgenda, "events" | "issues_due" | "cycles" | "meetings"> & {
    followups?: AgendaFollowup[];
  },
  windowDates: string[],
  deviceTimezone: string,
): AgendaDay[] {
  const byDate = new Map<string, AgendaDay>();
  for (const date of windowDates) {
    byDate.set(date, {
      date,
      cycles: [],
      events: [],
      issuesDue: [],
      meetings: [],
      followups: [],
    });
  }
  for (const event of agenda.events) {
    const day = byDate.get(dayKeyInTimezone(event.starts_at, event.timezone || "UTC"));
    day?.events.push(event);
  }
  for (const issue of agenda.issues_due) {
    byDate.get(issue.due_date)?.issuesDue.push(issue);
  }
  for (const meeting of agenda.meetings) {
    byDate.get(dayKeyInTimezone(meeting.started_at, deviceTimezone))?.meetings.push(meeting);
  }
  for (const cycle of agenda.cycles) {
    for (const date of windowDates) {
      if (date >= cycle.start_date && date <= cycle.end_date) {
        byDate.get(date)?.cycles.push(cycle);
      }
    }
  }
  for (const followup of agenda.followups ?? []) {
    byDate
      .get(dayKeyInTimezone(followup.fires_at, deviceTimezone))
      ?.followups.push(followup);
  }
  for (const day of byDate.values()) {
    day.events.sort(
      (a, b) => new Date(a.starts_at).getTime() - new Date(b.starts_at).getTime(),
    );
    day.followups.sort(
      (a, b) => new Date(a.fires_at).getTime() - new Date(b.fires_at).getTime(),
    );
  }
  return windowDates.map((date) => byDate.get(date)!);
}
