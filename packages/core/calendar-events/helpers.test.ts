// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  calendarEventsOverlap,
  canonicalCalendarResponse,
  dayKeyInTimezone,
  groupAgendaByDay,
  utcISOToZonedWallClock,
  zonedWallClockToUtcISO,
} from "./helpers";
import type { CalendarAgenda, CalendarEventEntry } from "../types";

function event(overrides: Partial<CalendarEventEntry> = {}): CalendarEventEntry {
  return {
    id: "e1",
    title: "Sync",
    description: "",
    starts_at: "2026-09-10T09:00:00Z",
    ends_at: "2026-09-10T09:30:00Z",
    all_day: false,
    timezone: "UTC",
    location: "",
    issue_id: null,
    project_id: null,
    status: "scheduled",
    created_by: { type: "member", id: "u1" },
    source: "vigil",
    decision_id: null,
    participants: [],
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
    ...overrides,
  };
}

describe("dayKeyInTimezone", () => {
  it("buckets an instant by the calendar day in the asked timezone", () => {
    // 23:30 UTC on the 9th is already the 10th in Tokyo (+9).
    expect(dayKeyInTimezone("2026-09-09T23:30:00Z", "Asia/Tokyo")).toBe("2026-09-10");
    expect(dayKeyInTimezone("2026-09-09T23:30:00Z", "UTC")).toBe("2026-09-09");
  });

  it("falls back to the UTC day slice for an unrecognized zone", () => {
    expect(dayKeyInTimezone("2026-09-09T23:30:00Z", "Not/AZone")).toBe("2026-09-09");
  });

  it("returns empty for an unparseable instant", () => {
    expect(dayKeyInTimezone("not-a-date", "UTC")).toBe("");
  });
});

describe("calendarEventsOverlap", () => {
  it("detects an overlap", () => {
    expect(
      calendarEventsOverlap(
        { starts_at: "2026-09-10T09:00:00Z", ends_at: "2026-09-10T10:00:00Z" },
        { starts_at: "2026-09-10T09:30:00Z", ends_at: "2026-09-10T11:00:00Z" },
      ),
    ).toBe(true);
  });

  it("treats back-to-back events as non-overlapping", () => {
    expect(
      calendarEventsOverlap(
        { starts_at: "2026-09-10T09:00:00Z", ends_at: "2026-09-10T10:00:00Z" },
        { starts_at: "2026-09-10T10:00:00Z", ends_at: "2026-09-10T11:00:00Z" },
      ),
    ).toBe(false);
  });

  it("is false for a malformed instant rather than throwing", () => {
    expect(
      calendarEventsOverlap(
        { starts_at: "nope", ends_at: "2026-09-10T10:00:00Z" },
        { starts_at: "2026-09-10T09:30:00Z", ends_at: "2026-09-10T11:00:00Z" },
      ),
    ).toBe(false);
  });
});

describe("canonicalCalendarResponse", () => {
  it("passes through a known response", () => {
    expect(canonicalCalendarResponse("accepted")).toBe("accepted");
    expect(canonicalCalendarResponse("declined")).toBe("declined");
    expect(canonicalCalendarResponse("tentative")).toBe("tentative");
  });

  it("degrades an unknown future response to pending", () => {
    expect(canonicalCalendarResponse("maybe-later")).toBe("pending");
  });
});

describe("zonedWallClockToUtcISO / utcISOToZonedWallClock", () => {
  it("round-trips a wall clock through a positive-offset zone", () => {
    const iso = zonedWallClockToUtcISO("2026-09-10T09:00", "Asia/Tokyo");
    // Tokyo is UTC+9 with no DST, so 09:00 JST is 00:00 UTC.
    expect(iso).toBe("2026-09-10T00:00:00.000Z");
    expect(utcISOToZonedWallClock(iso, "Asia/Tokyo")).toBe("2026-09-10T09:00");
  });

  it("round-trips through UTC itself", () => {
    const iso = zonedWallClockToUtcISO("2026-09-10T09:00", "UTC");
    expect(iso).toBe("2026-09-10T09:00:00.000Z");
    expect(utcISOToZonedWallClock(iso, "UTC")).toBe("2026-09-10T09:00");
  });

  it("returns empty string for empty or unparseable input", () => {
    expect(zonedWallClockToUtcISO("", "UTC")).toBe("");
    expect(utcISOToZonedWallClock("not-a-date", "UTC")).toBe("");
  });
});

describe("groupAgendaByDay", () => {
  const agenda: CalendarAgenda = {
    from: "2026-09-01T00:00:00Z",
    to: "2026-09-30T00:00:00Z",
    events: [event({ id: "e1", starts_at: "2026-09-09T23:30:00Z" })],
    issues_due: [
      { id: "i1", identifier: "MUL-1", title: "Ship it", status: "todo", due_date: "2026-09-10" },
    ],
    cycles: [{ id: "c1", name: "Cycle 1", start_date: "2026-09-01", end_date: "2026-09-14" }],
    meetings: [
      { id: "m1", title: "Standup", status: "done", started_at: "2026-09-09T23:45:00Z" },
    ],
    followups: [
      {
        id: "f1",
        issue_id: "i1",
        identifier: "MUL-1",
        issue_title: "Ship it",
        agent_id: "a1",
        agent_name: "Ada",
        fires_at: "2026-09-09T23:50:00Z",
        note: "Check staging",
      },
    ],
  };

  it("buckets events and meetings by their instant's day in tz, and issues by due_date directly", () => {
    const days = groupAgendaByDay(agenda, "Asia/Tokyo");
    expect(days.has("2026-09-10")).toBe(true);
    const day = days.get("2026-09-10")!;
    expect(day.events).toHaveLength(1);
    expect(day.meetings).toHaveLength(1);
    expect(day.issuesDue).toHaveLength(1);
    expect(day.followups).toHaveLength(1);
    // Cycles are not bucketed — they span a range, not one day.
    expect(days.has("2026-09-01")).toBe(false);
  });

  it("reads an agenda from a server that predates wake-ups as having none", () => {
    const { followups: _followups, ...older } = agenda;
    const days = groupAgendaByDay(older as CalendarAgenda, "Asia/Tokyo");
    expect(days.get("2026-09-10")?.followups).toEqual([]);
  });
});
