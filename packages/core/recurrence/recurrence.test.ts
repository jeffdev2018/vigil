// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import { ApiClient, ApiError } from "../api/client";
import { issueKeys } from "../issues/queries";
import {
  dailyCron,
  describeRecurrence,
  describeRecurrenceCron,
  formatRecurrenceClock,
  monthlyCron,
  recurrencePresetOf,
  weekdaysCron,
  weeklyCron,
} from "./presets";
import { invalidateIssueRecurrence, recurrenceSeriesIds } from "./mutations";
import { recurrenceKeys } from "./queries";
import { onIssueRecurrenceChanged } from "./ws-updaters";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(typeof body === "string" ? body : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const client = () => new ApiClient("https://api.example.test");

const validRule = {
  id: "r-1",
  issue_id: "i-src",
  cron_expression: "0 9 * * 1",
  timezone: "Europe/Paris",
  mode: "schedule",
  enabled: true,
  next_run_at: "2026-09-14T07:00:00Z",
  last_occurrence_id: "i-2",
  occurrence_count: 2,
  created_by_type: "member",
  created_by_id: "u-1",
  created_at: "2026-09-01T10:00:00Z",
  updated_at: "2026-09-01T10:00:00Z",
};

const validPayload = {
  recurrence: validRule,
  source: { id: "i-src", identifier: "ONE-12", title: "Weekly review" },
  occurrences: [
    {
      id: "i-2",
      identifier: "ONE-19",
      title: "Weekly review",
      status: "todo",
      created_at: "2026-09-07T07:00:00Z",
      due_date: "2026-09-11",
    },
    {
      id: "i-src",
      identifier: "ONE-12",
      title: "Weekly review",
      status: "done",
      created_at: "2026-09-01T10:00:00Z",
      due_date: null,
    },
  ],
  next_runs: ["2026-09-14T07:00:00Z", "2026-09-21T07:00:00Z", "2026-09-28T07:00:00Z"],
};

describe("getIssueRecurrence", () => {
  it("parses a well-formed payload", async () => {
    stubFetchJson(validPayload);
    const res = await client().getIssueRecurrence("i-2");
    expect(res?.recurrence.cron_expression).toBe("0 9 * * 1");
    expect(res?.source.identifier).toBe("ONE-12");
    expect(res?.occurrences).toHaveLength(2);
    expect(res?.next_runs).toHaveLength(3);
  });

  it("reads a 404 as 'this issue does not recur', not as a failure", async () => {
    stubFetchJson({ error: "this issue does not recur" }, 404);
    await expect(client().getIssueRecurrence("i-9")).resolves.toBeNull();
  });

  it("fills defaults for fields an older server omits", async () => {
    stubFetchJson({ recurrence: { id: "r-1" } });
    const res = await client().getIssueRecurrence("i-1");
    expect(res?.recurrence.mode).toBe("schedule");
    expect(res?.recurrence.timezone).toBe("UTC");
    expect(res?.recurrence.next_run_at).toBeNull();
    expect(res?.occurrences).toEqual([]);
    expect(res?.next_runs).toEqual([]);
    expect(res?.source).toEqual({ id: "", identifier: "", title: "" });
  });

  it("keeps a malformed series from costing the rule", async () => {
    stubFetchJson({ ...validPayload, occurrences: "nope", source: 42, next_runs: 7 });
    const res = await client().getIssueRecurrence("i-2");
    expect(res?.recurrence.cron_expression).toBe("0 9 * * 1");
    expect(res?.occurrences).toEqual([]);
    expect(res?.next_runs).toEqual([]);
  });

  it("degrades an unreadable rule to null rather than inventing an empty one", async () => {
    stubFetchJson({ recurrence: "not-an-object" });
    await expect(client().getIssueRecurrence("i-1")).resolves.toBeNull();
  });

  it("keeps a 500 as an ApiError", async () => {
    stubFetchJson({ error: "boom" }, 500);
    await expect(client().getIssueRecurrence("i-1")).rejects.toBeInstanceOf(ApiError);
  });
});

describe("setIssueRecurrence", () => {
  it("PUTs the rule and parses the answer", async () => {
    stubFetchJson(validPayload);
    const res = await client().setIssueRecurrence("i-1", {
      cron_expression: "0 9 * * 1",
      timezone: "Europe/Paris",
      mode: "schedule",
      enabled: true,
    });
    expect(res?.recurrence.id).toBe("r-1");
    const [, init] = (globalThis.fetch as unknown as { mock: { calls: [string, RequestInit][] } })
      .mock.calls[0]!;
    expect(init.method).toBe("PUT");
    expect(JSON.parse(String(init.body))).toEqual({
      cron_expression: "0 9 * * 1",
      timezone: "Europe/Paris",
      mode: "schedule",
      enabled: true,
    });
  });

  it("surfaces the members-only refusal as a 403", async () => {
    stubFetchJson({ error: "only a member sets a recurrence" }, 403);
    await expect(client().setIssueRecurrence("i-1", { mode: "on_close" })).rejects.toMatchObject({
      status: 403,
    });
  });

  it("surfaces a rejected cron as a 400", async () => {
    stubFetchJson({ error: "invalid cron_expression: nope" }, 400);
    await expect(
      client().setIssueRecurrence("i-1", { cron_expression: "nope" }),
    ).rejects.toMatchObject({ status: 400 });
  });

  it("degrades a malformed answer — the rule was saved server-side", async () => {
    stubFetchJson({ recurrence: 5 });
    await expect(client().setIssueRecurrence("i-1", { mode: "on_close" })).resolves.toBeNull();
  });
});

describe("clearIssueRecurrence", () => {
  it("surfaces a 404 so the UI can say the series is already stopped", async () => {
    stubFetchJson({ error: "this issue does not recur" }, 404);
    await expect(client().clearIssueRecurrence("i-1")).rejects.toMatchObject({ status: 404 });
  });
});

describe("preset crons", () => {
  it("builds the four shapes the radio can produce", () => {
    expect(dailyCron(9, 0)).toBe("0 9 * * *");
    expect(weekdaysCron(9, 30)).toBe("30 9 * * 1-5");
    expect(weeklyCron(1, 9, 0)).toBe("0 9 * * 1");
    expect(monthlyCron(1, 9, 0)).toBe("0 9 1 * *");
  });

  it("clamps a value the server would refuse", () => {
    expect(dailyCron(99, -4)).toBe("0 23 * * *");
    expect(monthlyCron(0, 9, 0)).toBe("0 9 1 * *");
    expect(weeklyCron(12, 9, 0)).toBe("0 9 * * 6");
  });

  it("pads the clock so a rule never reads '9:5'", () => {
    expect(formatRecurrenceClock(9, 5)).toBe("09:05");
  });
});

describe("describeRecurrenceCron", () => {
  it("reads back every preset it builds", () => {
    expect(describeRecurrenceCron(dailyCron(9, 0))).toEqual({ kind: "daily", hour: 9, minute: 0 });
    expect(describeRecurrenceCron(weekdaysCron(8, 15))).toEqual({
      kind: "weekdays",
      hour: 8,
      minute: 15,
    });
    expect(describeRecurrenceCron(weeklyCron(3, 18, 45))).toEqual({
      kind: "weekly",
      hour: 18,
      minute: 45,
      weekday: 3,
    });
    expect(describeRecurrenceCron(monthlyCron(28, 7, 0))).toEqual({
      kind: "monthly",
      hour: 7,
      minute: 0,
      day: 28,
    });
  });

  it("calls anything the radio cannot represent custom, verbatim", () => {
    for (const expr of [
      "0 9 * * 1,3",
      "0 9,18 * * *",
      "0 9 1 1 *",
      "0 9 1 * 1",
      "0 9 * *",
      "",
      "not a cron at all",
    ]) {
      expect(describeRecurrenceCron(expr)).toEqual({ kind: "custom", cron: expr.trim() });
    }
  });

  it("mode wins: an on_close rule has no cron to read", () => {
    expect(describeRecurrence("on_close", "")).toEqual({ kind: "on_close" });
    expect(describeRecurrence("on_close", "0 9 * * 1")).toEqual({ kind: "on_close" });
    expect(recurrencePresetOf("schedule", "0 9 * * 1")).toBe("weekly");
    expect(recurrencePresetOf("on_close", "")).toBe("on_close");
  });
});

describe("recurrenceSeriesIds", () => {
  it("names the source and every occurrence, once each", () => {
    expect(recurrenceSeriesIds(validPayload as never)).toEqual(["i-src", "i-2", "i-src"]);
  });

  it("answers nothing for an issue that does not recur", () => {
    expect(recurrenceSeriesIds(null)).toEqual([]);
  });
});

function spyClient() {
  const calls: unknown[][] = [];
  return {
    calls,
    qc: {
      invalidateQueries: (filters: { queryKey: unknown[] }) => {
        calls.push(filters.queryKey);
      },
    } as unknown as QueryClient,
  };
}

describe("invalidateIssueRecurrence", () => {
  it("refreshes this issue, every other member of the series, and the issues", () => {
    const { calls, qc } = spyClient();
    invalidateIssueRecurrence(qc, "ws-1", "i-2", ["i-src", "i-2", ""]);
    expect(calls).toEqual([
      recurrenceKeys.issue("ws-1", "i-2"),
      recurrenceKeys.issue("ws-1", "i-src"),
      issueKeys.all("ws-1"),
    ]);
  });
});

describe("onIssueRecurrenceChanged", () => {
  it("takes the workspace prefix: the rule belongs to the series, not the issue", () => {
    const { calls, qc } = spyClient();
    onIssueRecurrenceChanged(qc, "ws-1");
    expect(calls).toEqual([recurrenceKeys.all("ws-1"), issueKeys.all("ws-1")]);
  });
});
