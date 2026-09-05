// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentTask } from "@multica/core/types";
import type { RunControlState } from "@multica/core/issues/run-control";
import { renderWithI18n } from "../../test/i18n";

// Client parsing: packages/core/issues/run-control.test.ts.

const state = vi.hoisted(() => ({ run: null as RunControlState | null, pause: vi.fn(), steer: vi.fn(), resume: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/run-control", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/run-control")>()),
  issueRunStateOptions: () => ({ queryKey: ["rs"], queryFn: async () => state.run }),
  usePauseRun: () => ({ mutate: state.pause, isPending: false }),
  useSteerRun: () => ({ mutate: state.steer, isPending: false }),
  useResumeRun: () => ({ mutate: state.resume, isPending: false }),
}));

import { RunControls } from "./run-controls";

function render(task: Partial<AgentTask>) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunControls issueId="i1" task={{ id: "t1", status: "running", ...task } as AgentTask} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.run = null;
  state.pause.mockReset();
  state.steer.mockReset();
  state.resume.mockReset();
});

describe("RunControls", () => {
  it("pauses a running run and shows the pending state", async () => {
    render({ status: "running" });
    fireEvent.click(screen.getByTestId("pause-run"));
    expect(state.pause).toHaveBeenCalled();
    render({ status: "running", pause_requested_at: "2026-09-04T10:00:00Z" });
    expect(screen.getByTestId("pause-pending")).toBeTruthy();
  });

  it("steers a paused run and keeps Resume disabled without an instruction", async () => {
    state.run = { task_id: "t1", status: "paused", pause_pending: false, instructions: [], resumed_by_task_id: null };
    render({ status: "paused" });
    expect(await screen.findByTestId("paused-run-controls")).toBeTruthy();
    expect((screen.getByRole("button", { name: /Resume/ }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Instruction"), { target: { value: "Use the helper" } });
    fireEvent.click(screen.getByRole("button", { name: "Send instruction" }));
    expect(state.steer).toHaveBeenCalledWith("Use the helper", expect.anything());
  });

  it("resumes once an instruction exists", async () => {
    state.run = { task_id: "t1", status: "paused", pause_pending: false, instructions: ["Use the helper"], resumed_by_task_id: null };
    render({ status: "paused" });
    const resume = (await screen.findByRole("button", { name: /Resume/ })) as HTMLButtonElement;
    await waitFor(() => expect(resume.disabled).toBe(false));
    expect(screen.getByText("Use the helper")).toBeTruthy();
    fireEvent.click(resume);
    expect(state.resume).toHaveBeenCalled();
  });

  it("renders nothing for a finished or already resumed run", async () => {
    const { container } = render({ status: "completed" });
    expect(container.innerHTML).toBe("");
    const second = render({ status: "paused", resumed_by_task_id: "t2" });
    expect(second.container.innerHTML).toBe("");
  });
});
