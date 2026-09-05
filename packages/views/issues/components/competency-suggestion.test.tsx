// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AssigneeSuggestion } from "@multica/core/agents/competency";
import { renderWithI18n } from "../../test/i18n";

// Parsing and formatting: packages/core/agents/competency.test.ts.

const state = vi.hoisted(() => ({ suggestion: null as AssigneeSuggestion | null }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/agents/competency", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/competency")>()),
  assigneeSuggestionOptions: () => ({ queryKey: ["competency"], queryFn: async () => state.suggestion }),
}));

import { CompetencySuggestion } from "./competency-suggestion";

const row = (agent_id: string, agent_name: string, over: Partial<AssigneeSuggestion["candidates"][number]> = {}) => ({ agent_id, agent_name, domain_key: "path:server", success_count: 0, total_count: 0, duel_wins: 0, duel_losses: 0, sample_size: 0, score: 0, reliable: false, updated_at: "", ...over });

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
});
