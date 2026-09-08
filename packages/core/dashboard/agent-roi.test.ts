// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { roiTrendPct } from "./queries";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("roiTrendPct", () => {
  it("reads negative when the agent got cheaper", () => {
    expect(roiTrendPct(400, 800)).toBe(-50);
    expect(roiTrendPct(1000, 800)).toBe(25);
  });

  it("has no trend without both sides", () => {
    expect(roiTrendPct(null, 800)).toBeNull();
    expect(roiTrendPct(400, null)).toBeNull();
    // A previous period of zero has no percentage to change from.
    expect(roiTrendPct(400, 0)).toBeNull();
  });
});

describe("getDashboardAgentRoi", () => {
  it("passes the filters and keeps a malformed ratio null instead of zero", async () => {
    stubFetchJson({
      days: 7,
      agents: [
        { agent_id: "a1", agent_name: "Claude Code", provider: "anthropic", issues_closed: 2, prs_merged: 1, cost_usd_ticks: 800, uncosted_runs: 0, cost_per_issue_usd_ticks: 400, cost_per_pr_usd_ticks: 800, prev_cost_per_issue_usd_ticks: "x" },
      ],
    });
    const out = await new ApiClient("https://api.example.test").getDashboardAgentRoi({ days: 7, project_id: "p1", tz: "Asia/Tokyo" });
    expect((globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[0]).toContain("roi-by-agent?days=7&project_id=p1&tz=Asia%2FTokyo");
    expect(out.agents[0]?.cost_per_issue_usd_ticks).toBe(400);
    expect(out.agents[0]?.prev_cost_per_issue_usd_ticks).toBeNull();
  });

  it("falls back to an empty response on garbage", async () => {
    stubFetchJson([1, 2]);
    const out = await new ApiClient("https://api.example.test").getDashboardAgentRoi({ days: 30 });
    expect(out).toEqual({ days: 30, agents: [] });
  });
});
