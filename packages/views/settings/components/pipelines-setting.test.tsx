// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Pipeline } from "@multica/core/pipelines";
import { renderWithI18n } from "../../test/i18n";

// Parsing and stage states: packages/core/pipelines/pipeline-run.test.ts.

const state = vi.hoisted(() => ({ pipelines: [] as Pipeline[], save: vi.fn(), remove: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/workspace/queries", () => ({ agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "a1", name: "Planner" }, { id: "a2", name: "Builder" }] }) }));
vi.mock("@multica/core/pipelines", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/pipelines")>()),
  pipelinesOptions: () => ({ queryKey: ["pipelines"], queryFn: async () => state.pipelines }),
  pipelineSquadsOptions: () => ({ queryKey: ["squads"], queryFn: async () => [{ id: "q1", name: "Core squad" }] }),
  useSavePipeline: () => ({ mutate: state.save, isPending: false }),
  useDeletePipeline: () => ({ mutate: state.remove, isPending: false }),
}));

import { PipelinesSetting } from "./pipelines-setting";

function render(canManage = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PipelinesSetting canManage={canManage} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.pipelines = [];
  state.save.mockReset();
  state.remove.mockReset();
});

describe("PipelinesSetting", () => {
  it("creates a pipeline from the template with gates and executors", async () => {
    render();
    fireEvent.click(await screen.findByRole("button", { name: "Start from the triage → review template" }));
    fireEvent.change(screen.getByLabelText("Pipeline name"), { target: { value: "Delivery" } });
    fireEvent.change(screen.getByLabelText("Executor of stage 3"), { target: { value: "squad:q1" } });
    fireEvent.click(screen.getByRole("button", { name: "Save pipeline" }));
    expect(state.save).toHaveBeenCalledTimes(1);
    const call = state.save.mock.calls[0]?.[0] as { input: { name: string; stages: { name: string; executor_type: string; requires_human_gate: boolean }[] } };
    expect(call.input.name).toBe("Delivery");
    expect(call.input.stages.map((s) => s.name)).toEqual(["triage", "plan", "implement", "test", "review"]);
    expect(call.input.stages[2]?.executor_type).toBe("squad");
    expect(call.input.stages[2]?.requires_human_gate).toBe(true);
  });

  it("lists a pipeline with its chain, locks stages under open runs and deletes only when idle", async () => {
    state.pipelines = [{ id: "p1", name: "Delivery", stages: [{ id: "s1", position: 0, name: "plan", executor_type: "agent", executor_id: "a1", requires_human_gate: false }, { id: "s2", position: 1, name: "build", executor_type: "agent", executor_id: "zz", requires_human_gate: true }], open_runs: 1, created_at: "" }];
    render();
    expect(await screen.findByText("Delivery")).toBeTruthy();
    expect(screen.getByText(/Planner/)).toBeTruthy();
    expect(screen.getByText(/executor missing/)).toBeTruthy();
    expect((screen.getByLabelText("Delete pipeline Delivery") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    expect(screen.getByText(/stages cannot change/)).toBeTruthy();
    fireEvent.change(screen.getByLabelText("Pipeline name"), { target: { value: "Delivery v2" } });
    fireEvent.click(screen.getByRole("button", { name: "Save pipeline" }));
    expect(state.save).toHaveBeenCalledWith({ id: "p1", input: { name: "Delivery v2" } }, expect.anything());
  });
});
