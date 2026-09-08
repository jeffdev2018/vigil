// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRoiRow, DashboardAgentRoi } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Schema fallbacks and the trend maths: packages/core/dashboard/agent-roi.test.ts.

const state = vi.hoisted(() => ({ data: null as DashboardAgentRoi | null }));

vi.mock("@multica/core/dashboard/queries", () => ({
  dashboardAgentRoiOptions: (wsId: string, days: number, projectId: string | null, tz: string) => ({
    queryKey: ["dashboard", wsId, "roi-by-agent", days, projectId, tz],
    queryFn: async () => state.data,
  }),
  roiTrendPct: (cur: number | null, prev: number | null) =>
    cur === null || prev === null || prev === 0 ? null : ((cur - prev) / prev) * 100,
}));
vi.mock("@multica/ui/components/ui/number-flow", () => ({
  CurrencyNumberFlow: ({ value }: { value: number }) => <span>${value.toFixed(2)}</span>,
}));

import { AgentRoiCard } from "./agent-roi-card";

const row = (over: Partial<AgentRoiRow> = {}): AgentRoiRow => ({
  agent_id: "a1",
  agent_name: "Claude Code",
  provider: "anthropic",
  issues_closed: 0,
  prs_merged: 0,
  cost_usd_ticks: 0,
  uncosted_runs: 0,
  cost_per_issue_usd_ticks: null,
  cost_per_pr_usd_ticks: null,
  prev_cost_per_issue_usd_ticks: null,
  ...over,
});

function renderCard() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AgentRoiCard wsId="ws-1" days={30} projectId={null} tz="UTC" locales="en" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = null;
});

describe("AgentRoiCard", () => {
  it("headlines the two cheapest agents and trends each against the previous period", async () => {
    state.data = {
      days: 30,
      agents: [
        row({
          agent_id: "a1", agent_name: "Claude Code", issues_closed: 61, prs_merged: 40,
          cost_usd_ticks: 3_400_000_000_000, cost_per_issue_usd_ticks: 55_700_000_000,
          prev_cost_per_issue_usd_ticks: 111_400_000_000,
        }),
        row({
          agent_id: "a2", agent_name: "Codex", provider: "openai", issues_closed: 10, prs_merged: 5,
          cost_usd_ticks: 1_210_000_000_000, cost_per_issue_usd_ticks: 121_000_000_000,
          prev_cost_per_issue_usd_ticks: 96_800_000_000,
        }),
      ],
    };
    renderCard();
    const card = await screen.findByTestId("agent-roi");
    expect(screen.getByTestId("agent-roi-headline").textContent).toBe(
      "Claude Code closed 61 issues at $5.57/issue vs $12.10 for Codex",
    );
    expect(card.textContent).toContain("$5.57");
    expect(screen.getAllByTestId("agent-roi-trend").map((el) => el.textContent)).toEqual(["-50%", "+25%"]);
  });

  it("dashes the ratio of an agent that closed nothing instead of showing it as free", async () => {
    state.data = {
      days: 30,
      agents: [
        row({ agent_id: "a1", issues_closed: 2, cost_usd_ticks: 800, cost_per_issue_usd_ticks: 400 }),
        row({ agent_id: "a2", agent_name: "Burner", cost_usd_ticks: 1000, uncosted_runs: 1 }),
      ],
    };
    renderCard();
    const rows = await screen.findAllByTestId("agent-roi-row");
    expect(rows).toHaveLength(2);
    expect(rows[1]?.textContent).toContain("—");
    expect(rows[1]?.textContent).toContain("no issue closed");
    // One agent with a ratio: the single-agent sentence, not the comparison.
    expect(screen.getByTestId("agent-roi-headline").textContent).toBe("Claude Code closed 2 issues at $0.00/issue");
  });

  it("names the server's restricted bucket rather than its synthetic id", async () => {
    state.data = {
      days: 30,
      agents: [row({ agent_id: "__restricted_agents__", agent_name: "", provider: "", issues_closed: 4, cost_usd_ticks: 600, cost_per_issue_usd_ticks: 150 })],
    };
    renderCard();
    const card = await screen.findByTestId("agent-roi");
    expect(card.textContent).toContain("Other agents");
    expect(card.textContent).not.toContain("__restricted_agents__");
  });

  it("says when no agent was active instead of showing an empty table", async () => {
    state.data = { days: 30, agents: [] };
    renderCard();
    const card = await screen.findByTestId("agent-roi");
    expect(card.dataset.empty).toBe("true");
    expect(card.textContent).toContain("No agent activity in the last 30 days");
  });

  it("renders nothing when the response is not this shape", async () => {
    state.data = { days: 30 } as DashboardAgentRoi;
    const { container } = renderCard();
    expect(screen.queryByTestId("agent-roi")).toBeNull();
    expect(container.textContent).toBe("");
  });
});
