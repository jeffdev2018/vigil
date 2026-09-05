// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AssigneeSuggestion, IssueEstimate } from "@multica/core/agents/competency";
import { renderWithI18n } from "../../test/i18n";

// Parsing and formatting: packages/core/agents/competency.test.ts.

const state = vi.hoisted(() => ({
  suggestion: null as AssigneeSuggestion | null,
  estimate: null as IssueEstimate | null,
  holdEstimate: false,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/agents/competency", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/competency")>()),
  assigneeSuggestionOptions: () => ({ queryKey: ["competency"], queryFn: async () => state.suggestion }),
  issueEstimateOptions: (_ws: string, _issue: string, candidates: string[]) => ({
    queryKey: ["estimate", candidates.join(",")],
    queryFn: async () => (state.holdEstimate ? new Promise(() => {}) : state.estimate),
  }),
}));

import { CompetencySuggestion } from "./competency-suggestion";

const row = (agent_id: string, agent_name: string, over: Partial<AssigneeSuggestion["candidates"][number]> = {}) => ({ agent_id, agent_name, domain_key: "path:server", success_count: 0, total_count: 0, duel_wins: 0, duel_losses: 0, sample_size: 0, score: 0, reliable: false, updated_at: "", ...over });

const est = (agent_id: string, over: Partial<IssueEstimate["candidates"][number]> = {}) => ({ agent_id, agent_name: "", sample_size: 0, insufficient_history: true, median_cost_usd_ticks: null, cost_range_low_usd_ticks: null, cost_range_high_usd_ticks: null, median_duration_seconds: null, duration_range_low_seconds: null, duration_range_high_seconds: null, exceeds_budget: false, ...over });

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CompetencySuggestion issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.suggestion = null;
  state.estimate = null;
  state.holdEstimate = false;
});

describe("CompetencySuggestion", () => {
  it("renders nothing without data", async () => {
    state.suggestion = { domain_key: "path:server", min_sample: 5, candidates: [], ownership: null };
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows each candidate's rate and sample, flags small samples and counts duels apart", async () => {
    state.suggestion = { domain_key: "path:server", min_sample: 5, ownership: null, candidates: [
      row("a", "Alpha", { success_count: 11, total_count: 14, sample_size: 16, duel_wins: 2, score: 0.82, reliable: true }),
      row("b", "Beta", { success_count: 1, total_count: 1, sample_size: 1, score: 1, reliable: false }),
    ] };
    render();
    expect(await screen.findByTestId("competency-suggestion")).toBeTruthy();
    expect(screen.getByText("Domain server/")).toBeTruthy();
    expect(screen.getByText("Alpha: 82% (14 issues)")).toBeTruthy();
    expect(screen.getByText("2 won / 0 lost duels")).toBeTruthy();
    const rows = screen.getAllByTestId("competency-candidate");
    expect(rows.map((el) => el.getAttribute("data-reliable"))).toEqual(["true", "false"]);
    expect(rows[1]?.textContent).toContain("not enough data yet (1/5)");
  });

  // What-if estimate (K44): a second query, so the history must not wait on it.
  it("renders the candidates while the estimate is still loading", async () => {
    state.holdEstimate = true;
    state.suggestion = { domain_key: "path:server", min_sample: 5, ownership: null, candidates: [row("a", "Alpha", { total_count: 14, score: 0.82, reliable: true })] };
    render();
    expect(await screen.findByTestId("competency-candidate")).toBeTruthy();
    expect(screen.getByText("Alpha: 82% (14 issues)")).toBeTruthy();
    expect(screen.getByTestId("estimate-loading")).toBeTruthy();
    expect(screen.queryByTestId("estimate")).toBeNull();
  });

  it("shows a duration and cost range, an honest gap, and an over-budget warning", async () => {
    state.suggestion = { domain_key: "path:server", min_sample: 5, ownership: null, candidates: [
      row("a", "Alpha", { total_count: 14, score: 0.82, reliable: true }),
      row("b", "Beta", { total_count: 2, score: 0.5, reliable: false }),
    ] };
    state.estimate = { domain_key: "path:server", min_sample: 5, candidates: [
      est("a", { sample_size: 6, insufficient_history: false, median_cost_usd_ticks: 3_500_000_000, cost_range_low_usd_ticks: 3_000_000_000, cost_range_high_usd_ticks: 5_000_000_000, median_duration_seconds: 600, duration_range_low_seconds: 480, duration_range_high_seconds: 900, exceeds_budget: true }),
      est("b", { sample_size: 2 }),
    ] };
    render();
    expect(await screen.findByTestId("estimate")).toBeTruthy();
    expect(screen.getByTestId("estimate").textContent).toBe("8–15 min · $0.30–0.50");
    expect(screen.getByTestId("estimate-empty").textContent).toBe("not enough history to estimate");
    const badges = screen.getAllByTestId("estimate-over-budget");
    expect(badges).toHaveLength(1);
    expect(badges[0]?.textContent).toBe("over budget");
    // The warning belongs to the candidate whose median blows the budget.
    expect(screen.getAllByTestId("competency-candidate")[0]?.textContent).toContain("over budget");
  });
});
