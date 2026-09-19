import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";

// The pre-launch line shown wherever a user starts an agent run (create with an
// agent, assign on create, @mention, chat). Canonical suite for its copy; the
// surfaces only check that it is wired in.

const state = vi.hoisted(() => ({
  estimate: { data: undefined as unknown, isPending: false },
  agents: [] as Array<Record<string, unknown>>,
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: <T,>(options: T) => options,
  useQuery: ({ queryKey }: { queryKey: string[] }) => {
    if (queryKey[0] === "agent-cost-estimate") return state.estimate;
    if (queryKey[0] === "agents") return { data: state.agents };
    if (queryKey[0] === "runtimes") return { data: [{ id: "rt-1", name: "Claude (mac)", custom_name: "Studio", provider: "claude" }] };
    return { data: undefined };
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/workspace/queries", () => ({ agentListOptions: () => ({ queryKey: ["agents"] }) }));
vi.mock("@multica/core/runtimes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes")>()),
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
}));

import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../locales/en/agents.json";
import { AgentRunDetails } from "./agent-run-details";

function renderDetails() {
  return render(
    <I18nProvider locale="en" resources={{ en: { agents: enAgents } }}>
      <AgentRunDetails agentId="agent-1" />
    </I18nProvider>,
  );
}

describe("AgentRunDetails", () => {
  beforeEach(() => {
    state.agents = [{ id: "agent-1", name: "Analyst", runtime_id: "rt-1" }];
    state.estimate = { data: undefined, isPending: false };
  });

  it("names the runtime and the agent's recent average cost per run", () => {
    state.estimate = { data: { agent_id: "agent-1", sample_runs: 4, avg_cost_usd_ticks: 25_000_000_000 }, isPending: false };
    renderDetails();
    expect(screen.getByTestId("agent-run-details")).toHaveTextContent(
      "Runs on Studio (Claude) · ≈ $2.50 per run (average of its last 4 priced runs)",
    );
  });

  it("says the cost is unknown rather than inventing a zero", () => {
    state.estimate = { data: { agent_id: "agent-1", sample_runs: 0, avg_cost_usd_ticks: null }, isPending: false };
    renderDetails();
    const line = screen.getByTestId("agent-run-details");
    expect(line).toHaveTextContent("cost unknown");
    expect(line).not.toHaveTextContent("$0");
  });

  it("flags auto-routing and a runtime the viewer cannot see", () => {
    state.agents = [{ id: "agent-1", name: "Analyst", runtime_id: "rt-hidden", runtime_routing: "auto" }];
    state.estimate = { data: undefined, isPending: true };
    renderDetails();
    expect(screen.getByTestId("agent-run-details")).toHaveTextContent(
      "Runs on a runtime you cannot see or another runtime picked automatically · estimating cost…",
    );
  });
});
