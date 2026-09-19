// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import {
  dashboardVelocityWeeklyOptions,
  formatCycleTimeTrend,
  mergeThroughputWeeks,
} from "./queries";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

// 2026-09-12 is a Saturday, so the current week starts Monday 2026-09-07.
const NOW = new Date(Date.UTC(2026, 8, 12, 12, 0, 0));

describe("mergeThroughputWeeks", () => {
  it("fills the missing weeks with zeroes between the oldest week and today, ascending", () => {
    const merged = mergeThroughputWeeks(
      [
        { week_start: "2026-09-07", member_count: 2, agent_count: 5 },
        { week_start: "2026-08-24", member_count: 1, agent_count: 3 },
      ],
      30,
      NOW,
    );
    expect(merged).toEqual([
      { week_start: "2026-08-24", member_count: 1, agent_count: 3 },
      { week_start: "2026-08-31", member_count: 0, agent_count: 0 },
      { week_start: "2026-09-07", member_count: 2, agent_count: 5 },
    ]);
  });

  it("sums duplicate week_start rows and re-aligns off-Monday dates to the week's Monday", () => {
    const merged = mergeThroughputWeeks(
      [
        { week_start: "2026-09-09", member_count: 1, agent_count: 1 },
        { week_start: "2026-09-07", member_count: 2, agent_count: 3 },
      ],
      7,
      NOW,
    );
    expect(merged).toEqual([
      { week_start: "2026-09-07", member_count: 3, agent_count: 4 },
    ]);
  });

  it("emits an all-zero axis covering the days window when the server returned nothing", () => {
    const merged = mergeThroughputWeeks([], 30, NOW);
    expect(merged).toEqual([
      { week_start: "2026-08-10", member_count: 0, agent_count: 0 },
      { week_start: "2026-08-17", member_count: 0, agent_count: 0 },
      { week_start: "2026-08-24", member_count: 0, agent_count: 0 },
      { week_start: "2026-08-31", member_count: 0, agent_count: 0 },
      { week_start: "2026-09-07", member_count: 0, agent_count: 0 },
    ]);
    expect(mergeThroughputWeeks([], 7, NOW)).toEqual([
      { week_start: "2026-09-07", member_count: 0, agent_count: 0 },
    ]);
  });

  it("ignores unparseable week_start values", () => {
    const merged = mergeThroughputWeeks(
      [{ week_start: "not-a-date", member_count: 9, agent_count: 9 }],
      7,
      NOW,
    );
    expect(merged).toEqual([
      { week_start: "2026-09-07", member_count: 0, agent_count: 0 },
    ]);
  });
});

describe("formatCycleTimeTrend", () => {
  it("returns the percentage delta, negative when delivery got faster", () => {
    expect(formatCycleTimeTrend(3.5, 4.1)).toBeCloseTo(-14.63, 2);
    expect(formatCycleTimeTrend(0.9, 0.8)).toBeCloseTo(12.5, 2);
  });

  it("is null-safe: no median or a zero previous period means no trend", () => {
    expect(formatCycleTimeTrend(null, 4.1)).toBeNull();
    expect(formatCycleTimeTrend(3.5, null)).toBeNull();
    expect(formatCycleTimeTrend(3.5, 0)).toBeNull();
  });
});

describe("getDashboardVelocityWeekly", () => {
  it("passes the filters and keeps malformed medians null", async () => {
    stubFetchJson({
      throughput: [{ week_start: "2026-08-31", member_count: 4, agent_count: 11 }],
      cycle_time: {
        member_median_days: 3.5,
        agent_median_days: "x",
        prev_member_median_days: 4.1,
        prev_agent_median_days: 0.9,
        member_count: 12,
        agent_count: 40,
      },
      cost_per_closed_issue: [
        { week_start: "2026-08-31", issue_count: 5, total_cost_usd_ticks: 1234567890, mean_cost_usd_ticks: 246913578 },
      ],
    });
    const out = await new ApiClient("https://api.example.test").getDashboardVelocityWeekly({ days: 7, projectId: "p1" });
    expect((globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[0]).toContain("velocity/weekly?days=7&project_id=p1");
    expect(out.throughput).toHaveLength(1);
    expect(out.cycle_time.member_median_days).toBe(3.5);
    expect(out.cycle_time.agent_median_days).toBeNull();
    expect(out.cost_per_closed_issue[0]?.mean_cost_usd_ticks).toBe(246913578);
  });

  it("falls back to an empty response on garbage", async () => {
    stubFetchJson([1, 2]);
    const out = await new ApiClient("https://api.example.test").getDashboardVelocityWeekly({ days: 30 });
    expect(out).toEqual({
      throughput: [],
      cycle_time: {
        member_median_days: null,
        agent_median_days: null,
        prev_member_median_days: null,
        prev_agent_median_days: null,
        member_count: 0,
        agent_count: 0,
      },
      cost_per_closed_issue: [],
    });
  });
});

describe("dashboardVelocityWeeklyOptions", () => {
  it("gates on a resolved workspace, like every other dashboard card", () => {
    expect(dashboardVelocityWeeklyOptions("", 30, null).enabled).toBe(false);
    expect(dashboardVelocityWeeklyOptions("ws-1", 30, null).enabled).toBe(true);
  });

  it("keys on workspace, range, and project", () => {
    expect(dashboardVelocityWeeklyOptions("ws-1", 7, "p1").queryKey).toEqual([
      "dashboard",
      "ws-1",
      "velocity-weekly",
      7,
      "p1",
    ]);
  });
});
