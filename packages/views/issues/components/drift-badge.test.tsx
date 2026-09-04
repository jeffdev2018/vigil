// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ runs: [] as Partial<AgentTask>[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/handoff", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/handoff")>()),
  issueRunsOptions: () => ({ queryKey: ["runs"], queryFn: async () => state.runs }),
}));

import { DriftBadge } from "./drift-badge";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DriftBadge issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.runs = [];
});

describe("DriftBadge", () => {
  it("renders nothing while no run drifted", async () => {
    state.runs = [{ id: "t1", status: "failed", failure_reason: "agent_error" }];
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("names the exact reason of the stop", async () => {
    state.runs = [{ id: "0123456789", status: "failed", failure_reason: "drift_detected", drift_reason: "file_reread_loop", error: "Run stopped for drift: a.go read 8 times without a write in between" }];
    render();
    expect(await screen.findByText("Stopped for drift")).toBeTruthy();
    expect(screen.getByText(/re-reading the same file/)).toBeTruthy();
    expect(screen.getByText(/a\.go read 8 times/)).toBeTruthy();
    expect(screen.getByTestId("drift-badge").getAttribute("data-reason")).toBe("file_reread_loop");
  });
});
