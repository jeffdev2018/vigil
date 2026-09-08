// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// The tone/message matrix is canonical in
// packages/core/agents/routing-check.test.ts; this covers the wiring only.

const state = vi.hoisted(() => ({ problems: [] as Array<{ code: string; message: string; fatal: boolean }> }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/agents/routing-check", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/routing-check")>()),
  agentRoutingCheckOptions: () => ({
    queryKey: ["routing-check", state.problems.length],
    queryFn: async () => ({ agent_id: "a1", ok: state.problems.length === 0, fatal: state.problems.some((p) => p.fatal), problems: state.problems }),
  }),
}));

import { AgentRoutingCheck } from "./agent-routing-check";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AgentRoutingCheck agentId="a1" />
    </QueryClientProvider>,
  );
}

describe("AgentRoutingCheck", () => {
  it("reassures when nothing blocks the agent", async () => {
    state.problems = [];
    render();
    const line = await screen.findByTestId("agent-routing-check");
    expect(line.dataset.tone).toBe("ok");
    expect(line.textContent).toContain("would run");
  });

  it("shows the fatal problem's own message so the reader can act on it", async () => {
    state.problems = [{ code: "agent_archived", message: "Ada is archived: it cannot take new work until it is restored.", fatal: true }];
    render();
    const line = await screen.findByTestId("agent-routing-check");
    expect(line.dataset.tone).toBe("error");
    expect(line.textContent).toContain("Ada is archived");
  });

  it("marks a wait as a warning rather than an error", async () => {
    state.problems = [{ code: "runtime_offline_no_fallback", message: "the run waits until it comes back", fatal: false }];
    render();
    const line = await screen.findByTestId("agent-routing-check");
    expect(line.dataset.tone).toBe("warning");
  });
});
