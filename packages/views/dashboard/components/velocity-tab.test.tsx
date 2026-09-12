// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { DashboardVelocityWeekly } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  data: null as DashboardVelocityWeekly | null,
  fail: false,
}));

vi.mock("@multica/core/dashboard", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/dashboard")>();
  return {
    ...actual,
    dashboardVelocityWeeklyOptions: (wsId: string, days: number, projectId: string | null) => ({
      queryKey: ["dashboard", wsId, "velocity-weekly", days, projectId],
      queryFn: async () => {
        if (state.fail) throw new Error("network down");
        return state.data;
      },
    }),
  };
});
vi.mock("@multica/ui/components/ui/number-flow", () => ({
  NumberFlow: ({ value }: { value: number }) => <span>{value}</span>,
}));

import { VelocityTab } from "./velocity-tab";

const cycleTime = (over: Partial<DashboardVelocityWeekly["cycle_time"]> = {}) => ({
  member_median_days: null,
  agent_median_days: null,
  prev_member_median_days: null,
  prev_agent_median_days: null,
  member_count: 0,
  agent_count: 0,
  ...over,
});

function renderTab() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <VelocityTab wsId="ws-1" days={30} projectId={null} locales="en" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = null;
  state.fail = false;
});

describe("VelocityTab", () => {
  it("renders the KPIs, trends and both charts with data", async () => {
    state.data = {
      throughput: [
        { week_start: "2026-09-07", member_count: 3, agent_count: 5 },
      ],
      cycle_time: cycleTime({
        member_median_days: 2.5,
        prev_member_median_days: 5,
        agent_median_days: 1.25,
        prev_agent_median_days: 1,
        member_count: 3,
        agent_count: 5,
      }),
      cost_per_closed_issue: [
        {
          week_start: "2026-09-07",
          issue_count: 5,
          total_cost_usd_ticks: 20_000_000_000,
          mean_cost_usd_ticks: 4_000_000_000,
        },
      ],
    };
    renderTab();
    const tab = await screen.findByTestId("velocity-tab");
    expect(tab.dataset.empty).toBeUndefined();
    expect(tab.textContent).toContain("Member cycle time · 30D");
    expect(tab.textContent).toContain("Agent cycle time · 30D");
    expect(tab.textContent).toContain("2.5");
    expect(tab.textContent).toContain("1.3");
    expect(tab.textContent).toContain("Issues closed · 30D");
    // Total throughput KPI: 3 members + 5 agents.
    expect(tab.textContent).toContain("3 by members · 5 by agents");
    const trends = screen.getAllByTestId("velocity-trend").map((el) => el.textContent);
    expect(trends).toEqual(["-50%", "+25%"]);
    expect(screen.getByTestId("velocity-throughput")).toBeInTheDocument();
    expect(screen.getByTestId("velocity-cost")).toBeInTheDocument();
  });

  it("shows an empty state instead of zeros when nothing closed", async () => {
    state.data = {
      throughput: [],
      cycle_time: cycleTime(),
      cost_per_closed_issue: [],
    };
    renderTab();
    const tab = await screen.findByTestId("velocity-tab");
    expect(tab.dataset.empty).toBe("true");
    expect(tab.textContent).toContain("No velocity yet");
  });

  it("renders a dash rather than 0.0d when a median is null", async () => {
    state.data = {
      throughput: [{ week_start: "2026-09-07", member_count: 2, agent_count: 0 }],
      cycle_time: cycleTime({ member_median_days: 1.5, member_count: 2 }),
      cost_per_closed_issue: [],
    };
    renderTab();
    const tab = await screen.findByTestId("velocity-tab");
    expect(tab.textContent).toContain("1.5");
    expect(tab.textContent).toContain("—");
    expect(tab.textContent).not.toContain("0.0");
    expect(tab.textContent).toContain("Nothing closed in this period");
  });

  it("shows a retry-able error instead of nothing when the fetch fails", async () => {
    state.fail = true;
    renderTab();
    expect(await screen.findByTestId("velocity-error")).toBeInTheDocument();
    expect(screen.queryByTestId("velocity-tab")).toBeNull();
  });
});
