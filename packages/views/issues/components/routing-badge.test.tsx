// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueRouting } from "@multica/core/issues/routing";
import { renderWithI18n } from "../../test/i18n";

// Parsing: packages/core/issues/routing.test.ts.

const state = vi.hoisted(() => ({ routing: { decision: null, task_id: null } as IssueRouting }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/routing", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/routing")>()),
  issueRoutingOptions: () => ({ queryKey: ["routing"], queryFn: async () => state.routing }),
}));

import { RoutingBadge } from "./routing-badge";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RoutingBadge issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.routing = { decision: null, task_id: null };
});

describe("RoutingBadge", () => {
  it("renders nothing without a decision", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows the default decision neutrally and an escalation distinctly with its reason", async () => {
    state.routing = { decision: { risk_level: "low", matched_paths: ["apps/web/a.ts"], target_pool_name: "cheap", escalated: false, decided_at: "" }, task_id: "t1" };
    render();
    expect(await screen.findByText("Low risk")).toBeTruthy();
    expect(screen.getByText("Pool cheap")).toBeTruthy();
    expect(screen.getByTestId("routing-badge").getAttribute("data-escalated")).toBe("false");
    state.routing = { decision: { risk_level: "high", matched_paths: [], target_pool_name: "capable", escalated: true, escalation_reason: "2 consecutive failed runs on the normal pool", decided_at: "" }, task_id: "t2" };
    render();
    expect(await screen.findByText("Escalated")).toBeTruthy();
    expect(screen.getByText(/2 consecutive failed runs/)).toBeTruthy();
  });
});
