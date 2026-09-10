// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  FOLLOWUP_MAX_LEAD_MS,
  followupCancelError,
  followupNoteError,
  followupQuickChoices,
  followupRelativeTime,
  followupScheduleError,
  followupScheduledByLabel,
  followupWhenError,
  formatNextRuns,
  nextMondayAt9,
  toRfc3339,
} from "./followup-display";

/**
 * The quick choices are computed in the DEVICE's local calendar, so every
 * assertion here is written against local-time constructors rather than a
 * hardcoded UTC string — that is the property under test (a person in
 * Paris asking for "tomorrow 9:00" gets 09:00 Paris, not 09:00 UTC), and it
 * has to hold whatever timezone the machine running the suite is in.
 */
function localDate(
  y: number,
  m: number,
  d: number,
  h = 0,
  min = 0,
): Date {
  return new Date(y, m - 1, d, h, min, 0, 0);
}

describe("toRfc3339", () => {
  it("emits the device offset, and round-trips to the same instant", () => {
    const at = localDate(2026, 9, 11, 9, 0);
    const iso = toRfc3339(at);
    expect(iso).toMatch(/^2026-09-11T09:00:00(Z|[+-]\d{2}:\d{2})$/);
    expect(new Date(iso).getTime()).toBe(at.getTime());
  });

  it("zero-pads every field", () => {
    expect(toRfc3339(localDate(2026, 1, 2, 3, 4))).toMatch(
      /^2026-01-02T03:04:00/,
    );
  });
});

describe("followupQuickChoices", () => {
  const now = localDate(2026, 9, 10, 14, 30); // a Thursday afternoon

  it("offers exactly the three documented choices", () => {
    expect(followupQuickChoices(now).map((c) => c.id)).toEqual([
      "in_1h",
      "tomorrow_9",
      "next_monday_9",
    ]);
  });

  it("'in 1 hour' is one hour later, to the minute", () => {
    const [inOneHour] = followupQuickChoices(now);
    expect(inOneHour?.at.getTime()).toBe(now.getTime() + 3_600_000);
  });

  it("'tomorrow 9:00' is 09:00 local on the next calendar day", () => {
    const tomorrow = followupQuickChoices(now)[1]!.at;
    expect(tomorrow.getDate()).toBe(11);
    expect(tomorrow.getHours()).toBe(9);
    expect(tomorrow.getMinutes()).toBe(0);
  });

  it("crosses a month boundary rather than producing day 32", () => {
    const tomorrow = followupQuickChoices(localDate(2026, 9, 30, 23, 0))[1]!.at;
    expect(tomorrow.getMonth()).toBe(9); // October, 0-indexed
    expect(tomorrow.getDate()).toBe(1);
    expect(tomorrow.getHours()).toBe(9);
  });

  it("'next Monday' from a Thursday is the coming Monday at 09:00", () => {
    const monday = followupQuickChoices(now)[2]!.at;
    expect(monday.getDay()).toBe(1);
    expect(monday.getDate()).toBe(14);
    expect(monday.getHours()).toBe(9);
  });
});

describe("nextMondayAt9", () => {
  it("asked on a Monday, means the Monday after — never today", () => {
    const monday = localDate(2026, 9, 14, 8, 0); // Monday, before 9
    const next = nextMondayAt9(monday);
    expect(next.getDay()).toBe(1);
    expect(next.getDate()).toBe(21);
  });

  it("asked on a Sunday, means tomorrow", () => {
    const sunday = localDate(2026, 9, 13, 20, 0);
    expect(nextMondayAt9(sunday).getDate()).toBe(14);
  });
});

describe("followupWhenError", () => {
  const now = localDate(2026, 9, 10, 14, 30);

  it("accepts an instant inside the server's window", () => {
    expect(followupWhenError(new Date(now.getTime() + 3_600_000), now)).toBeNull();
  });

  it("refuses the past and anything under a minute", () => {
    expect(followupWhenError(new Date(now.getTime() - 1), now)).toMatch(/minute/);
    expect(followupWhenError(new Date(now.getTime() + 30_000), now)).toMatch(
      /minute/,
    );
  });

  it("refuses past 30 days", () => {
    expect(
      followupWhenError(new Date(now.getTime() + FOLLOWUP_MAX_LEAD_MS + 60_000), now),
    ).toMatch(/30 days/);
  });

  it("refuses an unparsable date", () => {
    expect(followupWhenError(new Date("nope"), now)).not.toBeNull();
  });
});

describe("followupNoteError", () => {
  it("passes an empty note (the server defaults it)", () => {
    expect(followupNoteError("")).toBeNull();
  });

  it("refuses past 500 characters", () => {
    expect(followupNoteError("x".repeat(500))).toBeNull();
    expect(followupNoteError("x".repeat(501))).toMatch(/500/);
  });
});

describe("followupRelativeTime", () => {
  const now = localDate(2026, 9, 10, 12, 0);

  it("reads in minutes, hours, then days", () => {
    expect(followupRelativeTime(toRfc3339(localDate(2026, 9, 10, 12, 30)), now)).toBe(
      "in 30 min",
    );
    expect(followupRelativeTime(toRfc3339(localDate(2026, 9, 10, 15, 0)), now)).toBe(
      "in 3 h",
    );
    expect(followupRelativeTime(toRfc3339(localDate(2026, 9, 11, 12, 0)), now)).toBe(
      "in 1 day",
    );
    expect(followupRelativeTime(toRfc3339(localDate(2026, 9, 14, 12, 0)), now)).toBe(
      "in 4 days",
    );
  });

  it("says 'due now' rather than a negative count", () => {
    expect(followupRelativeTime(toRfc3339(localDate(2026, 9, 10, 11, 0)), now)).toBe(
      "due now",
    );
  });

  it("renders nothing for an unparsable fires_at", () => {
    expect(followupRelativeTime("", now)).toBe("");
  });
});

describe("followupScheduledByLabel", () => {
  it("names the member when we resolved them", () => {
    expect(
      followupScheduledByLabel(
        { scheduled_by_type: "member", agent_name: "Ada" },
        "Jeff",
      ),
    ).toBe("Scheduled by Jeff");
  });

  it("falls back when the member list has not loaded", () => {
    expect(
      followupScheduledByLabel(
        { scheduled_by_type: "member", agent_name: "Ada" },
        null,
      ),
    ).toBe("Scheduled by a teammate");
  });

  it("names the agent for a run-scheduled follow-up", () => {
    expect(
      followupScheduledByLabel(
        { scheduled_by_type: "agent", agent_name: "Ada" },
        null,
      ),
    ).toBe("Scheduled by Ada");
  });

  it("stays readable for an unknown future actor type", () => {
    expect(
      followupScheduledByLabel(
        { scheduled_by_type: "webhook", agent_name: "Ada" },
        null,
      ),
    ).toBe("Scheduled");
  });
});

describe("followupScheduleError", () => {
  it("shows the 429 budget sentence verbatim", () => {
    expect(
      followupScheduleError({
        status: 429,
        body: { error: "follow-up budget reached: at most 20 per agent per day" },
      }),
    ).toBe("follow-up budget reached: at most 20 per agent per day");
  });

  it("still says something useful when a 429 carries no body", () => {
    expect(followupScheduleError({ status: 429 })).toMatch(/used up/);
  });

  it("prefers the server sentence on a 400", () => {
    expect(
      followupScheduleError({
        status: 400,
        body: { error: "agent_id is required when the issue is not assigned to an agent" },
      }),
    ).toMatch(/agent_id is required/);
  });

  it("falls back to a plain message for a transport failure", () => {
    expect(followupScheduleError(new Error("Network request failed"))).toBe(
      "Network request failed",
    );
    expect(followupScheduleError(null)).toMatch(/Could not schedule/);
  });
});

describe("followupCancelError", () => {
  it("explains a 409 as already fired", () => {
    const [title] = followupCancelError({ status: 409 });
    expect(title).toBe("Already done");
  });

  it("explains a 404 as gone", () => {
    const [title] = followupCancelError({ status: 404 });
    expect(title).toBe("Not found");
  });

  it("falls through to the generic pair", () => {
    const [title, body] = followupCancelError(new Error("boom"));
    expect(title).toBe("Could not cancel");
    expect(body).toBe("boom");
  });
});

describe("formatNextRuns", () => {
  it("drops entries the server sent unparsable", () => {
    expect(formatNextRuns(["2026-09-14T07:00:00Z", "not-a-date"])).toHaveLength(1);
  });
});
