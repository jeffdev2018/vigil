// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentCompetency } from "@multica/core/agents/competency";
import { renderWithI18n } from "../../test/i18n";

// Parsing and formatting: packages/core/agents/competency.test.ts.

const state = vi.hoisted(() => ({ competency: null as AgentCompetency | null }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/agents/competency", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/competency")>()),
  agentCompetencyOptions: () => ({ queryKey: ["competency"], queryFn: async () => state.competency }),
}));

import { AgentCompetencySection } from "./agent-competency-section";

const row = (domain_key: string, over: Partial<AgentCompetency["rows"][number]> = {}) => ({ agent_id: "a", agent_name: "Alpha", domain_key, success_count: 0, total_count: 0, duel_wins: 0, duel_losses: 0, sample_size: 0, score: 0, reliable: false, updated_at: "", ...over });

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AgentCompetencySection agentId="a" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.competency = null;
});

describe("AgentCompetencySection", () => {
  it("says there is no data instead of a zero score", async () => {
    state.competency = { agent_id: "a", min_sample: 5, rows: [] };
    render();
    expect((await screen.findByTestId("agent-competency")).getAttribute("data-empty")).toBe("true");
    expect(screen.queryByText(/0%/)).toBeNull();
  });

  it("lists domains with a rate when reliable and a low-sample note otherwise, duels apart", async () => {
    state.competency = { agent_id: "a", min_sample: 5, rows: [
      row("label:backend", { success_count: 11, total_count: 14, sample_size: 15, duel_wins: 1, score: 0.8, reliable: true }),
      row("path:packages", { success_count: 2, total_count: 2, sample_size: 2, score: 1, reliable: false }),
    ] };
    render();
    expect(await screen.findByTestId("agent-competency")).toBeTruthy();
    const rows = screen.getAllByTestId("agent-competency-row");
    expect(rows[0]?.textContent).toContain("backend");
    expect(rows[0]?.textContent).toContain("80% success");
    expect(rows[0]?.textContent).toContain("1 won / 0 lost duels");
    expect(rows[1]?.textContent).toContain("packages/");
    expect(rows[1]?.textContent).toContain("not enough data (2/5)");
    expect(rows[1]?.textContent).not.toContain("100%");
  });
});
