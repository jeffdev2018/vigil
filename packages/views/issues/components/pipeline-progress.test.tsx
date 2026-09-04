// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Pipeline, PipelineRun } from "@multica/core/pipelines";
import { renderWithI18n } from "../../test/i18n";

// Parsing and stage states: packages/core/pipelines/pipeline-run.test.ts.

const state = vi.hoisted(() => ({ run: null as PipelineRun | null, pipelines: [] as Pipeline[], start: vi.fn(), advance: vi.fn(), cancel: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/pipelines", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/pipelines")>()),
  issuePipelineRunOptions: () => ({ queryKey: ["run"], queryFn: async () => state.run }),
  pipelinesOptions: () => ({ queryKey: ["pipelines"], queryFn: async () => state.pipelines }),
  useStartPipelineRun: () => ({ mutate: state.start, isPending: false }),
  useAdvancePipelineRun: () => ({ mutate: state.advance, isPending: false }),
  useCancelPipelineRun: () => ({ mutate: state.cancel, isPending: false }),
}));

import { PipelineProgress } from "./pipeline-progress";

const stages = [
  { id: "s1", position: 0, name: "plan", executor_type: "agent" as const, executor_id: "a", requires_human_gate: false },
  { id: "s2", position: 1, name: "implement", executor_type: "agent" as const, executor_id: "b", requires_human_gate: true },
  { id: "s3", position: 2, name: "review", executor_type: "squad" as const, executor_id: "q", requires_human_gate: false },
];
const run = (over: Partial<PipelineRun>): PipelineRun => ({ id: "r1", pipeline_id: "p1", pipeline_name: "Delivery", issue_id: "i1", status: "active", current_stage_id: "s2", current_index: 1, gate_decision_id: null, last_error: null, stages, started_at: "", completed_at: null, ...over });

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PipelineProgress issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.run = null;
  state.pipelines = [];
  state.start.mockReset();
  state.advance.mockReset();
  state.cancel.mockReset();
});

describe("PipelineProgress", () => {
  it("renders nothing without a pipeline, and a picker to start one when some exist", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
    state.pipelines = [{ id: "p1", name: "Delivery", stages, open_runs: 0, created_at: "" }];
    render();
    fireEvent.change(await screen.findByLabelText("Choose a pipeline"), { target: { value: "p1" } });
    fireEvent.click(screen.getByRole("button", { name: "Start pipeline" }));
    expect(state.start).toHaveBeenCalledWith("p1", expect.anything());
  });

  it("shows done, current and to-do stages, then the gate waiting with approve and cancel", async () => {
    state.run = run({});
    render();
    const items = await screen.findAllByTestId("pipeline-stage");
    expect(items.map((el) => el.getAttribute("data-state"))).toEqual(["done", "current", "todo"]);
    state.run = run({ status: "paused", gate_decision_id: "d1" });
    render();
    expect(await screen.findByText("Waiting for the gate")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Approve and advance" }));
    expect(state.advance).toHaveBeenCalledWith("r1", expect.anything());
    fireEvent.click(screen.getAllByRole("button", { name: "Stop pipeline" })[0] as HTMLButtonElement);
    expect(state.cancel).toHaveBeenCalledWith("r1", expect.anything());
  });

  it("shows an executor error explicitly with a retry", async () => {
    state.run = run({ status: "paused", last_error: "its agent no longer exists; reassign the stage" });
    render();
    expect(await screen.findByTestId("pipeline-error")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Retry stage" })).toBeTruthy();
  });
});
