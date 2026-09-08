// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api/client";
import type { RunGroup, RunGroupAttempt } from "@multica/core/api/schemas";
import { renderWithI18n } from "../../test/i18n";

// Sorting, diff-stat parsing, the "one race at a time" rule and the error
// mapping are covered in packages/core/issues/run-group.test.ts. This file
// covers the wiring: what renders, and what the buttons call.

const state = vi.hoisted(() => ({
  groups: [] as RunGroup[],
  settle: vi.fn(),
  abandon: vi.fn(),
  start: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: state.toastError } }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "a1", name: "Alpha" }, { id: "a2", name: "Beta" }] }),
}));
vi.mock("@multica/core/issues/run-group", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/run-group")>()),
  runGroupsOptions: () => ({ queryKey: ["run-groups"], queryFn: async () => state.groups }),
  useStartRunGroup: () => ({ mutate: state.start, isPending: false }),
  useSettleRunGroup: () => ({ mutate: state.settle, isPending: false }),
  useAbandonRunGroup: () => ({ mutate: state.abandon, isPending: false }),
}));

import { RunGroupSection } from "./run-group-section";

const attempt = (over: Partial<RunGroupAttempt> = {}): RunGroupAttempt => ({
  task_id: "t1", agent_id: "a1", status: "completed", model: "", diff_stat: { files: 2, insertions: 10, deletions: 3 },
  diff_unified: "@@ -1 +1 @@\n-old\n+new", diff_truncated: false, created_at: "2026-01-01T00:00:00Z", completed_at: null, ...over,
});
const group = (over: Partial<RunGroup> = {}): RunGroup => ({
  id: "g1", issue_id: "i1", status: "running", attempt_count: 2, winner_task_id: null, created_by: null,
  created_at: "2026-01-01T00:00:00Z", settled_at: null,
  attempts: [attempt(), attempt({ task_id: "t2", agent_id: "a2", status: "running", model: "opus", diff_stat: null, diff_unified: null })],
  ...over,
});

function render(props: { canManage?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunGroupSection issueId="i1" {...props} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.groups = [];
  state.settle.mockReset();
  state.abandon.mockReset();
  state.start.mockReset();
  state.toastError.mockReset();
});

describe("RunGroupSection", () => {
  it("shows the empty state with the start button", async () => {
    render();
    expect(await screen.findByTestId("run-group-empty")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Start a race" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("lists attempts side by side with model, run status and diff, and disables start while a race runs", async () => {
    state.groups = [group()];
    render();
    const attempts = await screen.findAllByTestId("run-group-attempt");
    expect(attempts.map((el) => el.getAttribute("data-task-id"))).toEqual(["t1", "t2"]);
    expect(screen.getByText("Alpha")).toBeTruthy();
    expect(screen.getByText("opus")).toBeTruthy();
    expect(screen.getByText("agent default")).toBeTruthy();
    expect(screen.getByText("2 · +10 −3")).toBeTruthy();
    expect(screen.getByText("Completed")).toBeTruthy();
    expect(screen.getByTestId("run-group-diff").textContent).toContain("+new");
    expect((screen.getByRole("button", { name: "Start a race" }) as HTMLButtonElement).disabled).toBe(true);
  });

  it("says a diff was dropped for size rather than showing nothing", async () => {
    state.groups = [group({ attempts: [attempt({ diff_unified: null, diff_truncated: true })] })];
    render();
    expect((await screen.findByTestId("run-group-diff-truncated")).textContent).toContain("too large");
  });

  it("settles the race only after the confirmation is accepted", async () => {
    state.groups = [group()];
    render();
    fireEvent.click(await screen.findByRole("button", { name: "Keep Alpha" }));
    expect(state.settle).not.toHaveBeenCalled();
    expect(await screen.findByText("Keep Alpha's attempt?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Keep this attempt" }));
    expect(state.settle).toHaveBeenCalledWith({ groupId: "g1", winnerTaskId: "t1" }, expect.anything());
  });

  it("abandons the race after its own confirmation", async () => {
    state.groups = [group()];
    render();
    fireEvent.click(await screen.findByRole("button", { name: "Abandon the race" }));
    expect(await screen.findByText("Abandon this race?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Abandon" }));
    expect(state.abandon).toHaveBeenCalledWith({ groupId: "g1" }, expect.anything());
  });

  it("turns a 409 into its own sentence instead of the server's", async () => {
    state.groups = [group()];
    state.settle.mockImplementation((_vars, opts) => {
      opts.onError(new ApiError("this race is no longer running", 409, "Conflict", { code: "run_group_already_settled" }));
      opts.onSettled();
    });
    render();
    fireEvent.click(await screen.findByRole("button", { name: "Keep Alpha" }));
    fireEvent.click(await screen.findByRole("button", { name: "Keep this attempt" }));
    await waitFor(() => expect(state.toastError).toHaveBeenCalledWith("This race is no longer running."));
  });

  it("offers no action to a reader, and renders nothing when there is nothing to read", async () => {
    state.groups = [group({ status: "settled", winner_task_id: "t1" })];
    const { unmount } = render({ canManage: false });
    expect(await screen.findByTestId("run-group")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Start a race" })).toBeNull();
    expect(screen.queryByRole("button", { name: /Keep/ })).toBeNull();
    expect(screen.getByText("kept")).toBeTruthy();
    unmount();
    state.groups = [];
    const { container } = render({ canManage: false });
    await waitFor(() => expect(container.querySelector("[data-testid='run-group-section']")).toBeNull());
  });

  it("starts a race with two attempts and an optional model", async () => {
    render();
    fireEvent.click(await screen.findByRole("button", { name: "Start a race" }));
    const dialog = await screen.findByTestId("run-group-start-dialog");
    expect(dialog).toBeTruthy();
    expect((screen.getByRole("button", { name: "Start the race" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Attempt 1 — agent"), { target: { value: "a1" } });
    fireEvent.change(screen.getByLabelText("Attempt 2 — agent"), { target: { value: "a2" } });
    fireEvent.change(screen.getByLabelText("Attempt 2 — model"), { target: { value: " opus " } });
    fireEvent.change(screen.getByLabelText("Note"), { target: { value: " try both " } });
    fireEvent.click(screen.getByRole("button", { name: "Start the race" }));
    expect(state.start).toHaveBeenCalledWith(
      { attempts: [{ agent_id: "a1" }, { agent_id: "a2", model: "opus" }], note: "try both" },
      expect.anything(),
    );
  });
});
