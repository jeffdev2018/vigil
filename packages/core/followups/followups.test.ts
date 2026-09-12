// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import { ApiClient, ApiError } from "../api/client";
import { EMPTY_ISSUE_FOLLOWUPS } from "../api/schemas";
import { runKeys } from "../runs/fleet-queries";
import { followupKeys } from "./queries";
import { followupQuickChoiceInstant, followupSecondsUntil } from "./quick-choices";
import {
  DEFAULT_FOLLOWUP_BUDGET,
  followupBudgetFromSettings,
  mergeFollowupBudget,
} from "./settings";
import { onFollowupChanged } from "./ws-updaters";

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

const validFollowup = {
  id: "f-1",
  issue_id: "i-1",
  agent_id: "a-1",
  agent_name: "Ada",
  fires_at: "2026-09-11T09:00:00Z",
  note: "Check whether staging is green",
  scheduled_by_type: "member",
  scheduled_by_id: "u-1",
  created_at: "2026-09-10T18:00:00Z",
};

describe("listIssueFollowups", () => {
  it("parses a well-formed response", async () => {
    stubFetchJson({
      followups: [validFollowup],
      budget: { max_per_agent_per_day: 20, max_per_workspace_per_day: 200 },
    });
    const res = await client().listIssueFollowups("i-1");
    expect(res.followups).toHaveLength(1);
    expect(res.followups[0]?.agent_name).toBe("Ada");
    expect(res.budget.max_per_agent_per_day).toBe(20);
  });

  it("fills defaults for fields an older server omits", async () => {
    stubFetchJson({ followups: [{ id: "f-2" }] });
    const res = await client().listIssueFollowups("i-1");
    expect(res.followups[0]?.note).toBe("");
    expect(res.followups[0]?.scheduled_by_type).toBe("member");
    expect(res.budget).toEqual({ max_per_agent_per_day: 0, max_per_workspace_per_day: 0 });
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ followups: "not-an-array", budget: 42 });
    const res = await client().listIssueFollowups("i-1");
    expect(res).toEqual(EMPTY_ISSUE_FOLLOWUPS);
  });

  it("keeps a malformed budget from costing the pending rows", async () => {
    stubFetchJson({ followups: [validFollowup], budget: "nope" });
    const res = await client().listIssueFollowups("i-1");
    expect(res.followups).toHaveLength(1);
    expect(res.budget.max_per_agent_per_day).toBe(0);
  });
});

describe("scheduleIssueFollowup", () => {
  it("parses the created follow-up", async () => {
    stubFetchJson({ followup: validFollowup }, 201);
    const created = await client().scheduleIssueFollowup("i-1", { when: "+60", note: "n" });
    expect(created.id).toBe("f-1");
  });

  it("degrades a malformed body instead of throwing — the wake-up was filed", async () => {
    stubFetchJson({ followup: { fires_at: 12 } }, 201);
    const created = await client().scheduleIssueFollowup("i-1", { when: "+60" });
    expect(created.id).toBe("");
  });

  it("surfaces the daily-budget refusal as a 429 ApiError", async () => {
    stubFetchJson({ error: "follow-up budget reached: at most 20 per agent per day" }, 429);
    await expect(client().scheduleIssueFollowup("i-1", { when: "+60" })).rejects.toMatchObject({
      status: 429,
    });
  });

  it("keeps a 500 as an ApiError", async () => {
    stubFetchJson({ error: "boom" }, 500);
    await expect(client().scheduleIssueFollowup("i-1", { when: "+60" })).rejects.toBeInstanceOf(
      ApiError,
    );
  });
});

describe("cancelIssueFollowup", () => {
  it("surfaces a 409 so the UI can say it already fired", async () => {
    stubFetchJson({ error: "this follow-up already fired or was cancelled" }, 409);
    await expect(client().cancelIssueFollowup("i-1", "f-1")).rejects.toMatchObject({ status: 409 });
  });
});

describe("followupKeys", () => {
  it("nests the issue entry under the workspace prefix", () => {
    expect(followupKeys.issue("ws-1", "i-1")).toEqual([...followupKeys.all("ws-1"), "i-1"]);
  });
});

describe("onFollowupChanged", () => {
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

  it("refreshes the issue's follow-ups and the runs fleet", () => {
    const { calls, qc } = spyClient();
    onFollowupChanged(qc, "ws-1", "i-1");
    expect(calls).toEqual([followupKeys.issue("ws-1", "i-1"), runKeys.all("ws-1")]);
  });

  it("falls back to the workspace prefix when the payload names no issue", () => {
    const { calls, qc } = spyClient();
    onFollowupChanged(qc, "ws-1");
    expect(calls).toEqual([followupKeys.all("ws-1"), runKeys.all("ws-1")]);
  });
});

describe("followupQuickChoiceInstant", () => {
  // Wall-clock choices are local by design (see quick-choices.ts). The
  // assertions read the local getters back so they hold in any TZ the suite
  // runs under.
  it("puts 'in 1 h' exactly an hour out", () => {
    const now = new Date("2026-09-10T18:00:00Z");
    expect(followupQuickChoiceInstant("in_1h", now)).toBe("2026-09-10T19:00:00.000Z");
  });

  it("puts 'tomorrow 9:00' on the next local day at 09:00 local", () => {
    const now = new Date("2026-09-10T18:00:00Z");
    const at = new Date(followupQuickChoiceInstant("tomorrow_9", now));
    expect(at.getHours()).toBe(9);
    expect(at.getMinutes()).toBe(0);
    expect(at.getDate()).toBe(new Date(now.getTime() + 24 * 3600 * 1000).getDate());
  });

  it("puts 'next Monday 9:00' on a Monday, always ahead of now", () => {
    for (const iso of ["2026-09-07T12:00:00Z", "2026-09-10T12:00:00Z", "2026-09-13T12:00:00Z"]) {
      const now = new Date(iso);
      const at = new Date(followupQuickChoiceInstant("next_monday_9", now));
      expect(at.getDay()).toBe(1);
      expect(at.getHours()).toBe(9);
      expect(at.getTime()).toBeGreaterThan(now.getTime());
    }
  });
});

describe("followupSecondsUntil", () => {
  it("counts forward and never below zero", () => {
    const now = Date.parse("2026-09-10T18:00:00Z");
    expect(followupSecondsUntil("2026-09-10T18:30:00Z", now)).toBe(1800);
    expect(followupSecondsUntil("2026-09-10T17:00:00Z", now)).toBe(0);
  });

  it("answers null for an unparseable instant", () => {
    expect(followupSecondsUntil("soon")).toBeNull();
  });
});

describe("followupBudgetFromSettings", () => {
  it("reads the stored caps", () => {
    expect(
      followupBudgetFromSettings({ followups: { max_per_agent_per_day: 5, max_per_workspace_per_day: 50 } }),
    ).toEqual({ max_per_agent_per_day: 5, max_per_workspace_per_day: 50 });
  });

  it("falls back to the server's defaults when the key is absent or malformed", () => {
    for (const settings of [null, {}, { followups: "nope" }, { followups: { max_per_agent_per_day: "x" } }]) {
      expect(followupBudgetFromSettings(settings)).toEqual(DEFAULT_FOLLOWUP_BUDGET);
    }
  });

  it("clamps a stored value the API would refuse", () => {
    expect(
      followupBudgetFromSettings({ followups: { max_per_agent_per_day: 0, max_per_workspace_per_day: 9999 } }),
    ).toEqual({ max_per_agent_per_day: 1, max_per_workspace_per_day: 1000 });
  });
});

describe("mergeFollowupBudget", () => {
  it("keeps every other setting and clamps what it writes", () => {
    const merged = mergeFollowupBudget(
      { doctrine: { require_review: true }, followups: { max_per_agent_per_day: 20 } },
      { max_per_agent_per_day: 1500, max_per_workspace_per_day: 0 },
    );
    expect(merged).toEqual({
      doctrine: { require_review: true },
      followups: { max_per_agent_per_day: 1000, max_per_workspace_per_day: 1 },
    });
  });
});
