// @vitest-environment jsdom

import { cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api/client";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockApi = vi.hoisted(() => ({
  listTasksByIssue: vi.fn().mockResolvedValue([]),
  getRunDiff: vi.fn(),
  promoteRun: vi.fn(),
  discardRun: vi.fn(),
}));

// The error-code helper stays real — the 409 mapping is part of what is
// pinned here; only the transport is stubbed.
vi.mock("@multica/core/api", async () => ({
  ...(await vi.importActual<Record<string, unknown>>("@multica/core/api")),
  api: mockApi,
}));

const mockToast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast: mockToast }));

import { WorktreeRunBlock } from "./worktree-run-block";

// JEF-255: a finished worktree run's branch — its diff, and the promote /
// discard close-out. The affordance matrix itself is pinned in
// packages/core/issues/run-actions.test.ts; here is the row's contract:
// end states, pending states, lazy diff, and what a 409 looks like.

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-09-01T08:00:00Z",
    completed_at: "2026-09-01T08:04:00Z",
    result: null,
    error: null,
    created_at: "2026-09-01T08:00:00Z",
    branch_name: "agent/jef-255/task-1",
    ...overrides,
  };
}

function renderBlock(task: AgentTask) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <WorktreeRunBlock task={task} issueId="issue-1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockApi.listTasksByIssue.mockResolvedValue([]);
});

afterEach(() => cleanup());

describe("WorktreeRunBlock", () => {
  it("renders nothing for a run without a branch or one still in flight", () => {
    const { container: noBranch } = renderBlock(makeTask({ branch_name: undefined }));
    expect(noBranch).toBeEmptyDOMElement();
    const { container: running } = renderBlock(makeTask({ status: "running", completed_at: null }));
    expect(running).toBeEmptyDOMElement();
  });

  it("shows the branch and both actions on a finished run", () => {
    renderBlock(makeTask());
    expect(screen.getByText("agent/jef-255/task-1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Promote" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Discard" })).toBeEnabled();
    // The diff stays lazy: nothing is fetched before the block is expanded.
    expect(mockApi.getRunDiff).not.toHaveBeenCalled();
  });

  it("fetches the diff only on expand and renders stat and patch", async () => {
    const user = userEvent.setup();
    mockApi.getRunDiff.mockResolvedValue({
      diff_stat: { files: 2, insertions: 10, deletions: 3 },
      diff_unified: "@@ -1 +1 @@\n-old\n+new",
      diff_truncated: false,
    });
    renderBlock(makeTask());

    await user.click(screen.getByRole("button", { name: "Show diff" }));

    await screen.findByTestId("run-diff");
    expect(mockApi.getRunDiff).toHaveBeenCalledWith("task-1");
    expect(screen.getByTestId("run-diff-stat").textContent).toBe("2 · +10 −3");
    expect(screen.getByTestId("run-diff").textContent).toContain("+new");
  });

  it("reads the diff endpoint's 404 as 'no diff recorded', not as an error", async () => {
    const user = userEvent.setup();
    mockApi.getRunDiff.mockRejectedValue(new ApiError("nope", 404, "Not Found", { code: "run_diff_not_found" }));
    renderBlock(makeTask());

    await user.click(screen.getByRole("button", { name: "Show diff" }));

    expect(await screen.findByText("No diff recorded.")).toBeInTheDocument();
  });

  it("asks before promoting, then posts the request", async () => {
    const user = userEvent.setup();
    mockApi.promoteRun.mockResolvedValue({ request_id: "req-1", status: "pending" });
    renderBlock(makeTask());

    await user.click(screen.getByRole("button", { name: "Promote" }));
    expect(screen.getByText(/open a pull request/)).toBeInTheDocument();
    expect(mockApi.promoteRun).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Push and open PR" }));
    await waitFor(() => expect(mockApi.promoteRun).toHaveBeenCalledWith("issue-1", "task-1"));
  });

  it("warns that discard deletes the branch before posting it", async () => {
    const user = userEvent.setup();
    mockApi.discardRun.mockResolvedValue({ request_id: "req-2", status: "pending" });
    renderBlock(makeTask());

    await user.click(screen.getByRole("button", { name: "Discard" }));
    expect(screen.getByText(/are deleted. This cannot be undone/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Delete branch" }));
    await waitFor(() => expect(mockApi.discardRun).toHaveBeenCalledWith("issue-1", "task-1"));
  });

  it("pins the row as 'Promoting…' while the daemon executes", () => {
    renderBlock(makeTask({ pending_branch_action: "promote" }));
    const promote = screen.getByRole("button", { name: /Promoting…/ });
    expect(promote).toBeDisabled();
    expect(screen.getByRole("button", { name: "Discard" })).toBeDisabled();
  });

  it("links to the pull request once promoted, or says 'Promoted' without one", () => {
    renderBlock(makeTask({ promoted_at: "2026-09-01T09:00:00Z", promote_pr_url: "https://example.test/pr/1" }));
    const link = screen.getByTestId("run-promoted");
    expect(link.tagName).toBe("A");
    expect(link).toHaveAttribute("href", "https://example.test/pr/1");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", expect.stringContaining("noopener"));
    expect(screen.queryByRole("button", { name: "Promote" })).not.toBeInTheDocument();

    cleanup();
    renderBlock(makeTask({ promoted_at: "2026-09-01T09:00:00Z", promote_pr_url: "" }));
    expect(screen.getByTestId("run-promoted").textContent).toBe("Promoted");
  });

  it("collapses a discarded run to a muted marker — no diff, no actions", () => {
    renderBlock(makeTask({ discarded_at: "2026-09-01T09:00:00Z" }));
    expect(screen.getByTestId("run-discarded")).toHaveTextContent("Discarded");
    expect(screen.queryByRole("button", { name: "Promote" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /diff/i })).not.toBeInTheDocument();
  });

  it("shows a 409 as an inline note, not a toast", async () => {
    const user = userEvent.setup();
    mockApi.promoteRun.mockRejectedValue(new ApiError("nope", 409, "Conflict", { code: "run_not_promotable" }));
    renderBlock(makeTask());

    await user.click(screen.getByRole("button", { name: "Promote" }));
    await user.click(screen.getByRole("button", { name: "Push and open PR" }));

    expect(await screen.findByTestId("run-branch-action-error")).toHaveTextContent(/can no longer be promoted/);
    expect(mockToast.error).not.toHaveBeenCalled();
  });
});
