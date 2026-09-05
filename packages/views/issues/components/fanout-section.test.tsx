// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { FanoutBatch } from "@multica/core/issues/fanout";
import { renderWithI18n } from "../../test/i18n";

// Parsing and progress: packages/core/issues/fanout.test.ts.

const state = vi.hoisted(() => ({ batch: null as FanoutBatch | null, start: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => <a href={href} className={className}>{children}</a> }));
vi.mock("@multica/core/workspace/queries", () => ({ agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "lead", name: "Lead" }, { id: "a", name: "Alpha" }, { id: "b", name: "Beta" }] }) }));
vi.mock("@multica/core/issues/fanout", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/fanout")>()),
  issueFanoutOptions: () => ({ queryKey: ["fanout"], queryFn: async () => state.batch }),
  useStartFanout: () => ({ mutate: state.start, isPending: false }),
}));

import { FanoutSection } from "./fanout-section";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <FanoutSection issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.batch = null;
  state.start.mockReset();
});

describe("FanoutSection", () => {
  it("launches a fan-out with a leader and assigned sub-tasks", async () => {
    render();
    fireEvent.click(await screen.findByText("Fan out to specialists"));
    fireEvent.change(screen.getByLabelText("Leader agent"), { target: { value: "lead" } });
    fireEvent.change(screen.getByLabelText("Sub-task 1"), { target: { value: "Write the changelog" } });
    fireEvent.change(screen.getByLabelText("Assignee of sub-task 1"), { target: { value: "a" } });
    fireEvent.click(screen.getByRole("button", { name: "Add a sub-task" }));
    fireEvent.change(screen.getByLabelText("Sub-task 2"), { target: { value: "Tag the release" } });
    fireEvent.change(screen.getByLabelText("Assignee of sub-task 2"), { target: { value: "b" } });
    fireEvent.click(screen.getByRole("button", { name: "Launch fan-out" }));
    expect(state.start).toHaveBeenCalledWith({ leader_agent_id: "lead", sub_tasks: [{ description: "Write the changelog", assignee_id: "a" }, { description: "Tag the release", assignee_id: "b" }] }, expect.anything());
  });

  it("shows live member statuses, the barrier, and the partial-failure synthesis", async () => {
    state.batch = { id: "b1", parent_issue_id: "i1", leader_agent_id: "lead", status: "partial_failure", expected_count: 2, completed_count: 1, failed_count: 1, synthesis_task_id: "0123456789", created_at: "", completed_at: "2026-09-04T10:00:00Z", members: [
      { id: "m1", child_issue_id: "c1", task_id: "t1", task_status: "completed", assignee_agent_id: "a", description: "Write the changelog", outcome: "completed", settled_at: "" },
      { id: "m2", child_issue_id: "c2", task_id: "t2", task_status: "failed", assignee_agent_id: "b", description: "Tag the release", outcome: "failed", settled_at: "" },
    ] };
    render();
    expect(await screen.findByTestId("fanout-batch")).toBeTruthy();
    expect(screen.getAllByTestId("fanout-member").map((el) => el.getAttribute("data-outcome"))).toEqual(["completed", "failed"]);
    expect((screen.getByText("Write the changelog") as HTMLAnchorElement).getAttribute("href")).toBe("/acme/issues/c1");
    expect(screen.getByTestId("fanout-warning")).toBeTruthy();
    expect(screen.getByText(/Synthesis run 01234567/)).toBeTruthy();
    expect(screen.getByText("Fan out to specialists")).toBeTruthy();
  });
});
