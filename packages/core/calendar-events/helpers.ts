import type {
  AgendaFollowup,
  AgendaIssue,
  AgendaMeeting,
  CalendarAgenda,
  CalendarEventEntry,
  CalendarParticipantResponse,
} from "../types";

/**
 * Native calendar (OS plan, chantier 19) — pure helpers. No React, no
 * `new Date()` on a local clock in a layout path, so these are shareable and
 * unit-testable without a browser.
 */

/** One day's slice of the agenda, keyed by its "YYYY-MM-DD" in the asked timezone. */
export interface CalendarDayBucket {
  date: string;
  events: CalendarEventEntry[];
  issuesDue: AgendaIssue[];
  meetings: AgendaMeeting[];
  /** Scheduled agent wake-ups ("réveil programmé") firing that day. */
  followups: AgendaFollowup[];
}

/**
 * "YYYY-MM-DD" of an ISO instant in `tz`. `en-CA` formats as YYYY-MM-DD,
 * which is the one locale format that needs no reassembly. Falls back to the
 * instant's own UTC-day slice if `tz` is not a recognized IANA zone, so a
 * malformed preference degrades to UTC bucketing rather than throwing.
 */
export function dayKeyInTimezone(iso: string, tz: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  try {
    return new Intl.DateTimeFormat("en-CA", {
      timeZone: tz,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
    }).format(d);
  } catch {
    return d.toISOString().slice(0, 10);
  }
}

/**
 * Buckets the agenda's events, issues-due and meetings by calendar day in
 * `tz` — the shape the week/agenda list view renders. Cycles are omitted:
 * they span a range rather than living on one day, so a caller draws them as
 * a band across the days they cover instead of duplicating them per day.
 *
 * `issuesDue` uses its own `due_date` ("YYYY-MM-DD", already a calendar day
 * with no instant to convert) directly as the bucket key — a timezone
 * conversion would risk shifting a due date across midnight for no reason.
 */
export function groupAgendaByDay(
  agenda: CalendarAgenda,
  tz: string,
): Map<string, CalendarDayBucket> {
  const days = new Map<string, CalendarDayBucket>();
  const bucket = (date: string): CalendarDayBucket => {
    let b = days.get(date);
    if (!b) {
      b = { date, events: [], issuesDue: [], meetings: [], followups: [] };
      days.set(date, b);
    }
    return b;
  };
  for (const event of agenda.events) {
    const key = dayKeyInTimezone(event.starts_at, tz);
    if (key) bucket(key).events.push(event);
  }
  for (const issue of agenda.issues_due) {
    if (issue.due_date) bucket(issue.due_date).issuesDue.push(issue);
  }
  for (const meeting of agenda.meetings) {
    const key = dayKeyInTimezone(meeting.started_at, tz);
    if (key) bucket(key).meetings.push(meeting);
  }
  // `followups` is absent on a server that predates the wake-ups — read it as
  // empty rather than letting the whole grouping throw.
  for (const followup of agenda.followups ?? []) {
    const key = dayKeyInTimezone(followup.fires_at, tz);
    if (key) bucket(key).followups.push(followup);
  }
  return days;
}

/** True when two [start, end) instants overlap. Malformed instants never overlap. */
export function calendarEventsOverlap(
  a: { starts_at: string; ends_at: string },
  b: { starts_at: string; ends_at: string },
): boolean {
  const aStart = Date.parse(a.starts_at);
  const aEnd = Date.parse(a.ends_at);
  const bStart = Date.parse(b.starts_at);
  const bEnd = Date.parse(b.ends_at);
  if ([aStart, aEnd, bStart, bEnd].some(Number.isNaN)) return false;
  return aStart < bEnd && bStart < aEnd;
}

/**
 * Minutes to ADD to a UTC instant to get its wall-clock reading in `tz`
 * (e.g. +540 for Asia/Tokyo). Derived from `Intl` rather than a fixed table,
 * so it is correct on either side of a DST transition. Malformed instants or
 * an unrecognized zone answer 0 (treat as UTC) rather than throwing.
 */
function tzOffsetMinutes(utcInstant: Date, tz: string): number {
  if (Number.isNaN(utcInstant.getTime())) return 0;
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone: tz,
      hourCycle: "h23",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    }).formatToParts(utcInstant);
    const get = (type: string) => Number(parts.find((p) => p.type === type)?.value ?? "0");
    const asIfUTC = Date.UTC(
      get("year"),
      get("month") - 1,
      get("day"),
      get("hour") % 24,
      get("minute"),
      get("second"),
    );
    return Math.round((asIfUTC - utcInstant.getTime()) / 60_000);
  } catch {
    return 0;
  }
}

/**
 * Converts a "YYYY-MM-DDTHH:mm" wall-clock value (no offset, e.g. a
 * `<input type="datetime-local">`), read as being in `tz`, to a UTC ISO
 * instant the API accepts. Empty/unparseable input returns "".
 *
 * `<input type="datetime-local">` deliberately carries no timezone, so this
 * is the one place that decides what the typed digits mean — every other
 * caller works in real instants.
 */
export function zonedWallClockToUtcISO(wallClock: string, tz: string): string {
  if (!wallClock) return "";
  // Has seconds already ("...THH:mm:ss") or not ("...THH:mm") — either way,
  // appending "Z" reads the typed digits as if they were UTC, which is the
  // deliberate first step: it gives us an instant with the right NUMBERS, to
  // be corrected by the zone's offset below.
  const withSeconds = /T\d{2}:\d{2}$/.test(wallClock) ? `${wallClock}:00` : wallClock;
  const naiveUtc = new Date(`${withSeconds}Z`);
  if (Number.isNaN(naiveUtc.getTime())) return "";
  const offset = tzOffsetMinutes(naiveUtc, tz);
  return new Date(naiveUtc.getTime() - offset * 60_000).toISOString();
}

/**
 * The reverse of {@link zonedWallClockToUtcISO}: the "YYYY-MM-DDTHH:mm" a
 * `datetime-local` input should show for a UTC instant in `tz`.
 */
export function utcISOToZonedWallClock(iso: string, tz: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  try {
    const parts = new Intl.DateTimeFormat("en-CA", {
      timeZone: tz,
      hourCycle: "h23",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
    }).formatToParts(d);
    const get = (type: string) => parts.find((p) => p.type === type)?.value ?? "";
    return `${get("year")}-${get("month")}-${get("day")}T${get("hour")}:${get("minute")}`;
  } catch {
    return "";
  }
}

const KNOWN_RESPONSES = ["pending", "accepted", "declined", "tentative"] as const;
export type KnownCalendarResponse = (typeof KNOWN_RESPONSES)[number];

/**
 * Canonicalizes a participant's response for a display switch. A value this
 * client doesn't recognize (a future response type from a newer backend)
 * degrades to "pending" — the correct default (an unread invitation), not a
 * decision this client is unable to render one way or the other.
 */
export function canonicalCalendarResponse(
  response: CalendarParticipantResponse,
): KnownCalendarResponse {
  return (KNOWN_RESPONSES as readonly string[]).includes(response)
    ? (response as KnownCalendarResponse)
    : "pending";
}
