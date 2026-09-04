// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunLimitEvent } from "@multica/core/budgets/run-limits";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ events: [] as RunLimitEvent[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/budgets/run-limits", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/budgets/run-limits")>()),
  issueRunLimitEventsOptions: () => ({ queryKey: ["rle"], queryFn: async () => state.events }),
}));

import { RunLimitBadge } from "./run-limit-badge";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunLimitBadge issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.events = [];
});

describe("RunLimitBadge", () => {
  it("renders nothing without an event", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows the latest run's warning, then its stop", async () => {
    state.events = [{ task_id: "t2", gate: "cost", level: "stopped", observed: 12000000000, limit: 10000000000, policy_id: "p", created_at: "" }, { task_id: "t2", gate: "cost", level: "warn", observed: 6000000000, limit: 10000000000, policy_id: "p", created_at: "" }, { task_id: "t1", gate: "turns", level: "warn", observed: 8, limit: 10, policy_id: "p", created_at: "" }];
    render();
    expect(await screen.findByText("Stopped by a run limit")).toBeTruthy();
    expect(screen.getByText(/Cost \$1\.20 \/ \$1\.00 · stopped/)).toBeTruthy();
    expect(screen.queryByText(/Turns/)).toBeNull();
    expect(screen.getByTestId("run-limit-badge").getAttribute("data-stopped")).toBe("true");
  });
});
