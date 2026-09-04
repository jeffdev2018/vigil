// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentScorecard } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Schema fallbacks and the rate helper: packages/core/agents/scorecard.test.ts.

const state = vi.hoisted(() => ({ data: null as AgentScorecard | null }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/agents/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/queries")>()),
  agentScorecardOptions: (wsId: string, agentId: string, days: number) => ({ queryKey: ["scorecard", wsId, agentId, days], queryFn: async () => state.data }),
}));

import { AgentScorecardSection } from "./agent-scorecard-section";

const totals = (over: Partial<AgentScorecard["totals"]> = {}) => ({
  runs_total: 0, runs_failed: 0, runs_cancelled: 0, runs_accepted: 0, runs_reopened: 0, runs_no_intervention: 0, cost_usd_ticks_total: 0, low_sample: true, ...over,
});

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AgentScorecardSection agentId="a1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = null;
});

describe("AgentScorecardSection", () => {
  it("says there is no data yet instead of showing zero rates", async () => {
    state.data = { agent_id: "a1", days: 30, totals: totals(), previous: totals(), series: [] };
    renderSection();
    const s = await screen.findByTestId("agent-scorecard");
    expect(s.dataset.empty).toBe("true");
    expect(s.textContent).toContain("Not enough runs yet");
  });

  it("shows each rate with its move against the previous period and flags a small sample", async () => {
    state.data = {
      agent_id: "a1", days: 30,
      totals: totals({ runs_total: 8, runs_accepted: 6, runs_failed: 1, runs_reopened: 0, runs_no_intervention: 4, cost_usd_ticks_total: 16_000_000_000, low_sample: true }),
      previous: totals({ runs_total: 10, runs_accepted: 5, runs_failed: 3, runs_no_intervention: 5, low_sample: false }),
      series: [],
    };
    renderSection();
    const s = await screen.findByTestId("agent-scorecard");
    expect(s.textContent).toContain("8 runs, read with care");
    const metrics = screen.getAllByTestId("scorecard-metric").map((el) => el.textContent);
    expect(metrics).toEqual(["75%+25", "13%-17", "0%", "50%"]);
    expect(screen.getByTestId("scorecard-cost").textContent).toBe("$0.20");
  });
});
