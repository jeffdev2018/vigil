// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunCheckpointStatus } from "@multica/core/issues/checkpoint";
import { renderWithI18n } from "../../test/i18n";

// Parsing: packages/core/issues/checkpoint.test.ts.

const state = vi.hoisted(() => ({ run: null as RunCheckpointStatus | null }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/checkpoint", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/checkpoint")>()),
  issueRunCheckpointOptions: () => ({ queryKey: ["cp"], queryFn: async () => state.run }),
}));

import { RunInterruptedBanner } from "./run-interrupted-banner";

const run = (over: Partial<RunCheckpointStatus>): RunCheckpointStatus => ({
  task_id: "t", status: "running", failure_reason: "", last_checkpoint_seq: 12, checkpointed_at: "2026-09-04T10:00:00Z", attempts: 0, max_attempts: 3, resumed_from_task_id: null, exhausted: false, ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunInterruptedBanner issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.run = null;
});

describe("RunInterruptedBanner", () => {
  it("renders nothing while the run was never interrupted", async () => {
    state.run = run({});
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("announces an automatic resume from the checkpoint", async () => {
    state.run = run({ attempts: 1, resumed_from_task_id: "t0" });
    render();
    expect(await screen.findByText(/resumed automatically.*message 12.*1\/3/)).toBeTruthy();
    expect(screen.getByTestId("run-interrupted-banner").getAttribute("data-exhausted")).toBe("false");
  });

  it("marks the failure distinctly once the resume chain gave up", async () => {
    state.run = run({ attempts: 3, status: "failed", failure_reason: "checkpoint_resume_exhausted", exhausted: true });
    render();
    expect(await screen.findByText(/gave up after 3\/3/)).toBeTruthy();
    expect(screen.getByTestId("run-interrupted-banner").getAttribute("data-exhausted")).toBe("true");
  });
});
