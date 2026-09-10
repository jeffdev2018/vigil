// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AgendaCycle, AgendaIssue, AgendaMeeting, CalendarEventEntry } from "@multica/core/types";
import {
  buildAgendaDays,
  dayKeyInTimezone,
  enumerateLocalDays,
  formatEventTimeRange,
  groupEventsByDay,
  responseLabel,
  statusLabel,
} from "./calendar-display";

function event(overrides: Partial<CalendarEventEntry> = {}): CalendarEventEntry {
  return {
    id: "e1",
    title: "Sprint review",
    description: "",
    starts_at: "2026-09-10T14:00:00Z",
    ends_at: "2026-09-10T15:00:00Z",
    all_day: false,
    timezone: "UTC",
    location: "",
    issue_id: null,
    issue_identifier: "",
    project_id: null,
    status: "scheduled",
    created_by: { type: "member", id: "u1", name: "Ada" },
    source: "vigil",
    external_id: "",
    decision_id: null,
    participants: [],
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
    ...overrides,
  };
}

describe("dayKeyInTimezone", () => {
  it("keys by the event's own timezone, not UTC", () => {
    // 23:30 UTC on Sep 9 is already Sep 10 in Tokyo (UTC+9).
    const iso = "2026-09-09T23:30:00Z";
    expect(dayKeyInTimezone(iso, "UTC")).toBe("2026-09-09");
    expect(dayKeyInTimezone(iso, "Asia/Tokyo")).toBe("2026-09-10");
  });

  it("falls back to UTC on an invalid timezone instead of throwing", () => {
    expect(() => dayKeyInTimezone("2026-09-09T12:00:00Z", "Not/AZone")).not.toThrow();
  });

  it("returns a sentinel for an unparsable instant", () => {
    expect(dayKeyInTimezone("not-a-date", "UTC")).toBe("invalid-date");
  });
});

describe("groupEventsByDay", () => {
  it("groups by day-in-own-timezone and sorts groups and rows chronologically", () => {
    const a = event({ id: "a", starts_at: "2026-09-10T14:00:00Z" });
    const b = event({ id: "b", starts_at: "2026-09-09T09:00:00Z" });
    const c = event({ id: "c", starts_at: "2026-09-10T08:00:00Z" });
    const groups = groupEventsByDay([a, b, c]);
    expect(groups.map((g) => g.date)).toEqual(["2026-09-09", "2026-09-10"]);
    expect(groups[1]?.items.map((e) => e.id)).toEqual(["c", "a"]);
  });

  it("splits a day boundary correctly across two different event timezones", () => {
    const utcEvening = event({
      id: "utc",
      starts_at: "2026-09-09T23:00:00Z",
      timezone: "UTC",
    });
    const tokyoNextDay = event({
      id: "tokyo",
      starts_at: "2026-09-09T23:00:00Z",
      timezone: "Asia/Tokyo",
    });
    const groups = groupEventsByDay([utcEvening, tokyoNextDay]);
    expect(groups.map((g) => g.date)).toEqual(["2026-09-09", "2026-09-10"]);
  });
});

describe("formatEventTimeRange", () => {
  it("renders All day for an all-day event", () => {
    expect(formatEventTimeRange(event({ all_day: true }))).toBe("All day");
  });

  it("renders a start–end range in the event's timezone", () => {
    const e = event({
      starts_at: "2026-09-10T14:00:00Z",
      ends_at: "2026-09-10T15:30:00Z",
      timezone: "UTC",
    });
    expect(formatEventTimeRange(e)).toBe("2:00 PM – 3:30 PM");
  });
});

describe("responseLabel", () => {
  it.each([
    ["accepted", "Accepted"],
    ["declined", "Declined"],
    ["tentative", "Tentative"],
    ["pending", "Pending"],
  ] as const)("%s -> %s", (response, label) => {
    expect(responseLabel(response)).toBe(label);
  });
});

describe("statusLabel", () => {
  it("labels a proposed event as awaiting a decision", () => {
    expect(statusLabel("proposed")).toBe("Awaits a decision");
  });
  it("labels scheduled and cancelled", () => {
    expect(statusLabel("scheduled")).toBe("Scheduled");
    expect(statusLabel("cancelled")).toBe("Cancelled");
  });
});

describe("enumerateLocalDays", () => {
  it("produces N consecutive local calendar days starting at start", () => {
    expect(enumerateLocalDays(new Date(2026, 8, 28), 4)).toEqual([
      "2026-09-28",
      "2026-09-29",
      "2026-09-30",
      "2026-10-01",
    ]);
  });
});

describe("buildAgendaDays", () => {
  const windowDates = enumerateLocalDays(new Date(2026, 8, 9), 3); // Sep 9-11, 2026

  function issue(overrides: Partial<AgendaIssue> = {}): AgendaIssue {
    return {
      id: "i1",
      identifier: "ENG-1",
      title: "Ship it",
      status: "todo",
      due_date: "2026-09-10",
      assignee_type: null,
      assignee_id: null,
      ...overrides,
    };
  }
  function cycle(overrides: Partial<AgendaCycle> = {}): AgendaCycle {
    return {
      id: "c1",
      name: "Cycle 12",
      start_date: "2026-09-08",
      end_date: "2026-09-10",
      ...overrides,
    };
  }
  function meeting(overrides: Partial<AgendaMeeting> = {}): AgendaMeeting {
    return {
      id: "m1",
      title: "Standup",
      status: "done",
      started_at: "2026-09-11T09:00:00Z",
      ended_at: null,
      ...overrides,
    };
  }

  it("renders every window day even with nothing on it", () => {
    const days = buildAgendaDays(
      { events: [], issues_due: [], cycles: [], meetings: [] },
      windowDates,
      "UTC",
    );
    expect(days.map((d) => d.date)).toEqual(windowDates);
    expect(days.every((d) => d.events.length === 0)).toBe(true);
  });

  it("keys issues_due by their date-only due_date directly", () => {
    const days = buildAgendaDays(
      { events: [], issues_due: [issue({ due_date: "2026-09-10" })], cycles: [], meetings: [] },
      windowDates,
      "UTC",
    );
    expect(days.find((d) => d.date === "2026-09-10")?.issuesDue).toHaveLength(1);
  });

  it("bands a cycle across every day inside its inclusive start/end range", () => {
    const days = buildAgendaDays(
      { events: [], issues_due: [], cycles: [cycle({ start_date: "2026-09-08", end_date: "2026-09-10" })], meetings: [] },
      windowDates,
      "UTC",
    );
    // Sep 9, 10 are in the window (Sep 8 isn't); both should carry the band.
    expect(days.find((d) => d.date === "2026-09-09")?.cycles).toHaveLength(1);
    expect(days.find((d) => d.date === "2026-09-10")?.cycles).toHaveLength(1);
  });

  it("groups meetings by the device timezone", () => {
    const days = buildAgendaDays(
      { events: [], issues_due: [], cycles: [], meetings: [meeting({ started_at: "2026-09-11T09:00:00Z" })] },
      windowDates,
      "UTC",
    );
    expect(days.find((d) => d.date === "2026-09-11")?.meetings).toHaveLength(1);
  });
});
