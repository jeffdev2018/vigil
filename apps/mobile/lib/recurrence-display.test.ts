// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  DEFAULT_RECURRENCE_DRAFT,
  cronError,
  cronSentence,
  cronToDraft,
  formatClock,
  ordinal,
  presetToCron,
  presetToMode,
  recurrenceSentence,
  recurrenceSetError,
  type RecurrenceDraft,
} from "@/lib/recurrence-display";

const draft = (over: Partial<RecurrenceDraft>): RecurrenceDraft => ({
  ...DEFAULT_RECURRENCE_DRAFT,
  ...over,
});

/**
 * Presets → cron. The four scheduled shapes are the ones the contract names
 * as suggested presets (recurrence-api-contract.md); `on_close` writes no
 * cron at all, which is what lets the server accept it without one.
 */
describe("presetToCron / presetToMode", () => {
  it.each([
    [draft({ preset: "daily", hour: 9, minute: 0 }), "0 9 * * *"],
    [draft({ preset: "weekdays", hour: 9, minute: 0 }), "0 9 * * 1-5"],
    [draft({ preset: "weekly", hour: 9, minute: 0, weekday: 1 }), "0 9 * * 1"],
    [draft({ preset: "weekly", hour: 18, minute: 30, weekday: 5 }), "30 18 * * 5"],
    [draft({ preset: "monthly", hour: 9, minute: 0, monthDay: 1 }), "0 9 1 * *"],
    [draft({ preset: "monthly", hour: 7, minute: 5, monthDay: 15 }), "5 7 15 * *"],
    [draft({ preset: "custom", customCron: "  */15 * * * *  " }), "*/15 * * * *"],
    [draft({ preset: "on_close" }), ""],
  ])("%o → %s", (input, expected) => {
    expect(presetToCron(input)).toBe(expected);
  });

  it("only on_close leaves the schedule mode", () => {
    expect(presetToMode("on_close")).toBe("on_close");
    for (const preset of ["daily", "weekdays", "weekly", "monthly", "custom"] as const) {
      expect(presetToMode(preset)).toBe("schedule");
    }
  });
});

/** Round-trip: what the sheet wrote is what "Edit" re-opens on. */
describe("cronToDraft", () => {
  it.each([
    ["0 9 * * *", "daily"],
    ["0 9 * * 1-5", "weekdays"],
    ["0 9 * * 1", "weekly"],
    ["0 9 1 * *", "monthly"],
    ["*/15 * * * *", "custom"],
    ["0 9 * 3 1", "custom"], // a month restriction has no preset
    ["0 9 1 * 1", "custom"], // both day-of-month and weekday pinned
    ["nonsense", "custom"],
    ["", "custom"],
  ])("%s → %s", (cron, preset) => {
    expect(cronToDraft(cron, "schedule").preset).toBe(preset);
  });

  it("carries the time and the weekday back", () => {
    expect(cronToDraft("30 18 * * 5", "schedule")).toMatchObject({
      preset: "weekly",
      hour: 18,
      minute: 30,
      weekday: 5,
    });
  });

  it("carries the day of month back", () => {
    expect(cronToDraft("5 7 15 * *", "schedule")).toMatchObject({
      preset: "monthly",
      hour: 7,
      minute: 5,
      monthDay: 15,
    });
  });

  it("keeps the raw expression in the custom box", () => {
    expect(cronToDraft("*/15 * * * *", "schedule").customCron).toBe("*/15 * * * *");
  });

  it("on_close wins over whatever cron rode along", () => {
    expect(cronToDraft("0 9 * * 1", "on_close").preset).toBe("on_close");
  });
});

describe("cronSentence", () => {
  it.each([
    ["0 9 * * *", "Every day at 09:00"],
    ["0 9 * * 1-5", "Every weekday at 09:00"],
    ["0 9 * * 1", "Every Monday at 09:00"],
    ["0 9 * * 0", "Every Sunday at 09:00"],
    ["30 18 * * 5", "Every Friday at 18:30"],
    ["0 9 1 * *", "On the 1st of each month at 09:00"],
    ["0 9 22 * *", "On the 22nd of each month at 09:00"],
    ["0 9 11 * *", "On the 11th of each month at 09:00"],
    // Nothing this recognises falls back to the expression itself rather
    // than to a sentence that would be wrong.
    ["*/15 * * * *", "*/15 * * * *"],
    ["0 9 * 3 1", "0 9 * 3 1"],
    ["@daily", "@daily"],
  ])("%s → %s", (cron, sentence) => {
    expect(cronSentence(cron)).toBe(sentence);
  });
});

describe("recurrenceSentence", () => {
  it("appends the zone to a scheduled rule", () => {
    expect(
      recurrenceSentence({
        mode: "schedule",
        cron_expression: "0 9 * * 1",
        timezone: "Europe/Paris",
      }),
    ).toBe("Every Monday at 09:00 · Europe/Paris");
  });

  it("says what on_close means, and quotes no zone for it", () => {
    expect(
      recurrenceSentence({
        mode: "on_close",
        cron_expression: "",
        timezone: "Europe/Paris",
      }),
    ).toBe("When the current one is closed");
  });

  it("reads an unknown mode through its cron instead of dropping it", () => {
    expect(
      recurrenceSentence({
        mode: "some_future_mode",
        cron_expression: "0 9 * * *",
        timezone: "UTC",
      }),
    ).toBe("Every day at 09:00 · UTC");
  });

  it("degrades an unknown mode with no cron to a readable word", () => {
    expect(
      recurrenceSentence({ mode: "some_future_mode", cron_expression: "", timezone: "UTC" }),
    ).toBe("Recurring");
  });
});

/**
 * Pre-flight only — robfig server-side is the authority. The point of each
 * case is that an obvious typo costs no round-trip, and that nothing valid
 * is refused locally.
 */
describe("cronError", () => {
  it.each([
    "0 9 * * *",
    "0 9 * * 1-5",
    "*/15 * * * *",
    "0 0,12 * * *",
    "0 9 1,15 * *",
    "59 23 31 12 6",
    "0 9-17/2 * * 1-5",
    "@daily",
    "@every 1h30m",
  ])("accepts %s", (expr) => {
    expect(cronError(expr)).toBeNull();
  });

  it.each([
    ["", "Enter a cron expression."],
    ["   ", "Enter a cron expression."],
    ["0 9 * *", "A cron expression has 5 fields: minute hour day month weekday."],
    ["0 9 * * * *", "A cron expression has 5 fields: minute hour day month weekday."],
    ["90 9 * * *", '"90" is not a valid minute.'],
    ["0 25 * * *", '"25" is not a valid hour.'],
    ["0 9 0 * *", '"0" is not a valid day of month.'],
    ["0 9 * 13 *", '"13" is not a valid month.'],
    // robfig's 5-field parser tops weekday at 6; 7 is not an alias for Sunday.
    ["0 9 * * 7", '"7" is not a valid weekday.'],
    ["0 9 * * 5-1", '"5-1" is not a valid weekday.'],
    ["0 9 * * mon", '"mon" is not a valid weekday.'],
    ["*/0 * * * *", '"*/0" is not a valid minute.'],
    ["0 9 * * 1,,2", '"1,,2" is not a valid weekday.'],
  ])("refuses %s", (expr, message) => {
    expect(cronError(expr)).toBe(message);
  });
});

describe("formatClock / ordinal", () => {
  it("pads both halves", () => {
    expect(formatClock(9, 0)).toBe("09:00");
    expect(formatClock(18, 5)).toBe("18:05");
    expect(formatClock(0, 0)).toBe("00:00");
  });

  it.each([
    [1, "1st"],
    [2, "2nd"],
    [3, "3rd"],
    [4, "4th"],
    [11, "11th"],
    [12, "12th"],
    [13, "13th"],
    [21, "21st"],
    [22, "22nd"],
    [23, "23rd"],
    [31, "31st"],
  ])("%i → %s", (n, out) => {
    expect(ordinal(n)).toBe(out);
  });
});

describe("recurrenceSetError", () => {
  it("prints the server's 400 verbatim — it names which field it refused", () => {
    expect(
      recurrenceSetError({
        status: 400,
        body: { error: "invalid cron_expression: parse cron: expected 5 fields" },
      }),
    ).toBe("invalid cron_expression: parse cron: expected 5 fields");
  });

  it("explains a 403 when the server sent no sentence", () => {
    expect(recurrenceSetError({ status: 403, body: {} })).toBe(
      "Only a member of this workspace can set a recurrence.",
    );
  });

  it("prefers the server's own 403 wording", () => {
    expect(
      recurrenceSetError({ status: 403, body: { error: "only a member sets a recurrence" } }),
    ).toBe("only a member sets a recurrence");
  });

  it("falls back to the thrown message, then to a sentence", () => {
    expect(recurrenceSetError(new Error("Network request failed"))).toBe(
      "Network request failed",
    );
    expect(recurrenceSetError(null)).toBe(
      "Could not save the recurrence. Try again in a moment.",
    );
  });
});
