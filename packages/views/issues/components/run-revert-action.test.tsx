// @vitest-environment jsdom

import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockApi = vi.hoisted(() => ({
  requestRunRevert: vi.fn(),
  getRunRevertRequest: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({ api: mockApi }));

const mockToast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast: mockToast }));

import { RunRevertAction } from "./run-revert-action";

// F09: reverting a conversation to one of its turns.
//
// What this pins is the affordance contract and the outcome reporting. The git
// side is in server/internal/daemon/execenv/local_worktree_test.go; the
// "may this run be reverted to" matrix is server-side, in
// internal/handler/worktree_revert_unit_test.go, and reaches the client as the
// single `revertable` boolean asserted here.

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: "2026-06-08T08:04:00Z",
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    ...overrides,
  };
}

function renderAction(task: AgentTask, laterRunCount = 0) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <RunRevertAction task={task} issueId="issue-1" laterRunCount={laterRunCount} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
});

afterEach(() => cleanup());

describe("RunRevertAction", () => {
  // Absent, not disabled: a disabled button promises an affordance this row can
  // never offer, and a click on it explains nothing.
  it("renders nothing for a run the server did not mark revertable", () => {
    renderAction(makeTask());
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("renders nothing when revertable is anything other than true", () => {
    // What a newer or malformed backend can produce. The affordance fails
    // closed, so only a literal `true` opens it.
    renderAction(makeTask({ revertable: "yes" as unknown as boolean }));
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("names how many runs disappear before doing anything", async () => {
    const user = userEvent.setup();
    renderAction(makeTask({ revertable: true, checkpoint_sha: "abc", turn_seq: 1 }), 2);

    await user.click(screen.getByRole("button", { name: "Revert the branch to this run" }));

    expect(
      screen.getByText(/2 later runs and their messages are removed/),
    ).toBeInTheDocument();
    // Confirming is a second, separate act: nothing is requested on open.
    expect(mockApi.requestRunRevert).not.toHaveBeenCalled();
  });

  it("says so when there is nothing after this turn", async () => {
    const user = userEvent.setup();
    renderAction(makeTask({ revertable: true, checkpoint_sha: "abc", turn_seq: 3 }), 0);

    await user.click(screen.getByRole("button", { name: "Revert the branch to this run" }));

    expect(screen.getByText(/No later runs are removed/)).toBeInTheDocument();
  });

  it("reports success once the daemon has moved the branch", async () => {
    const user = userEvent.setup();
    mockApi.requestRunRevert.mockResolvedValue({ request_id: "req-1", status: "pending" });
    mockApi.getRunRevertRequest.mockResolvedValue({ request_id: "req-1", status: "done" });

    renderAction(makeTask({ revertable: true, checkpoint_sha: "abc", turn_seq: 1 }), 1);

    await user.click(screen.getByRole("button", { name: "Revert the branch to this run" }));
    await user.click(screen.getByRole("button", { name: "Revert branch" }));

    await waitFor(() => expect(mockApi.requestRunRevert).toHaveBeenCalledWith("issue-1", "task-1"));
    await waitFor(() => expect(mockToast.success).toHaveBeenCalled());
  });

  // The daemon's refusal is the only thing that tells the user what to do
  // next — "the branch moved off that checkpoint" — so it is shown verbatim
  // rather than collapsed into a generic failure.
  it("shows the daemon's named cause when the revert is refused", async () => {
    const user = userEvent.setup();
    mockApi.requestRunRevert.mockResolvedValue({ request_id: "req-1", status: "pending" });
    mockApi.getRunRevertRequest.mockResolvedValue({
      request_id: "req-1",
      status: "failed",
      error: "revert refused: branch agent/j/x has moved off that run's checkpoint",
    });

    renderAction(makeTask({ revertable: true, checkpoint_sha: "abc", turn_seq: 1 }), 1);

    await user.click(screen.getByRole("button", { name: "Revert the branch to this run" }));
    await user.click(screen.getByRole("button", { name: "Revert branch" }));

    await waitFor(() =>
      expect(mockToast.error).toHaveBeenCalledWith(
        expect.stringContaining("moved off that run's checkpoint"),
      ),
    );
    expect(mockToast.success).not.toHaveBeenCalled();
  });

  it("surfaces a rejected request without leaving the row spinning", async () => {
    const user = userEvent.setup();
    mockApi.requestRunRevert.mockRejectedValue(new Error("a revert is already in progress for this run"));

    renderAction(makeTask({ revertable: true, checkpoint_sha: "abc", turn_seq: 1 }), 1);

    await user.click(screen.getByRole("button", { name: "Revert the branch to this run" }));
    await user.click(screen.getByRole("button", { name: "Revert branch" }));

    await waitFor(() =>
      expect(mockToast.error).toHaveBeenCalledWith("a revert is already in progress for this run"),
    );
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Revert the branch to this run" })).toBeEnabled(),
    );
    expect(mockApi.getRunRevertRequest).not.toHaveBeenCalled();
  });
});
