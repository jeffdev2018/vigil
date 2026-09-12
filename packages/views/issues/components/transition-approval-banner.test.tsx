// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueTransitionRequest } from "@multica/core/issue-transitions";
import { renderWithI18n } from "../../test/i18n";

// Parsing and the pending/effective helpers:
// packages/core/issue-transitions/schemas.test.ts.

const state = vi.hoisted(() => ({
  requests: [] as IssueTransitionRequest[],
  role: "member" as string,
  decide: vi.fn(),
  cancel: vi.fn(),
}));

const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (sel: (s: unknown) => unknown) => sel({ user: { id: "u1" } }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [{ user_id: "u1", role: state.role }],
  }),
}));
vi.mock("@multica/core/issue-transitions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issue-transitions")>()),
  issueTransitionRequestsOptions: () => ({
    queryKey: ["reqs"],
    queryFn: async () => ({ requests: state.requests }),
  }),
  useDecideIssueTransitionRequest: () => ({ mutate: state.decide, isPending: false }),
  useCancelIssueTransitionRequest: () => ({ mutate: state.cancel, isPending: false }),
}));

import { TransitionApprovalBanner } from "./transition-approval-banner";

const request = (over: Partial<IssueTransitionRequest> = {}): IssueTransitionRequest => ({
  id: "q1",
  workspace_id: "ws-1",
  issue_id: "i1",
  from_status: "in_progress",
  to_status: "done",
  rule_id: "r1",
  requested_by_type: "member",
  requested_by_id: "u2",
  state: "pending",
  decided_by_type: null,
  decided_by_id: null,
  decided_at: null,
  note: null,
  created_at: "2026-09-04T10:00:00Z",
  ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <TransitionApprovalBanner issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.requests = [];
  state.role = "member";
  state.decide.mockReset();
  state.cancel.mockReset();
  toast.success.mockReset();
  toast.error.mockReset();
});

describe("TransitionApprovalBanner", () => {
  it("renders nothing when no request is pending", async () => {
    state.requests = [request({ state: "approved" })];
    render();
    await waitFor(() => expect(screen.queryByTestId("transition-approval-banner")).toBeNull());
  });

  it("names the requested status", async () => {
    state.requests = [request()];
    render();
    const banner = await screen.findByTestId("transition-approval-banner");
    expect(banner.textContent).toContain("done");
  });

  it("hides the decision controls from a plain member", async () => {
    // A button that 403s is worse than no button: the rule can widen the
    // approver set, and someone widened into it uses the inbox item instead.
    state.requests = [request()];
    state.role = "member";
    render();
    await screen.findByTestId("transition-approval-banner");
    expect(screen.queryByRole("button", { name: "Approve" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Reject" })).toBeNull();
  });

  it("lets an admin approve with a note", async () => {
    state.requests = [request()];
    state.role = "admin";
    render();
    await screen.findByTestId("transition-approval-banner");

    fireEvent.change(screen.getByPlaceholderText("Note (optional)"), { target: { value: "ship it" } });
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));

    expect(state.decide).toHaveBeenCalledWith(
      { requestId: "q1", decision: "approve", note: "ship it" },
      expect.anything(),
    );
  });

  it("lets an admin reject without a note", async () => {
    state.requests = [request()];
    state.role = "owner";
    render();
    await screen.findByTestId("transition-approval-banner");

    fireEvent.click(screen.getByRole("button", { name: "Reject" }));

    expect(state.decide).toHaveBeenCalledWith(
      { requestId: "q1", decision: "reject", note: undefined },
      expect.anything(),
    );
  });

  it("lets the requester cancel their own request", async () => {
    state.requests = [request({ requested_by_id: "u1" })];
    render();
    await screen.findByTestId("transition-approval-banner");

    fireEvent.click(screen.getByRole("button", { name: "Cancel my request" }));

    expect(state.cancel).toHaveBeenCalledWith("q1", expect.anything());
  });

  it("hides the cancel action from a member who did not file the request", async () => {
    state.requests = [request({ requested_by_id: "u2" })];
    render();
    await screen.findByTestId("transition-approval-banner");
    expect(screen.queryByRole("button", { name: "Cancel my request" })).toBeNull();
  });

  it("does not treat an agent request with a colliding id as the user's own", async () => {
    state.requests = [request({ requested_by_type: "agent", requested_by_id: "u1" })];
    render();
    await screen.findByTestId("transition-approval-banner");
    expect(screen.queryByRole("button", { name: "Cancel my request" })).toBeNull();
  });

  it("gives an admin who did not file the request the decision, not the cancel", async () => {
    state.requests = [request({ requested_by_id: "u2" })];
    state.role = "admin";
    render();
    await screen.findByTestId("transition-approval-banner");
    await screen.findByRole("button", { name: "Approve" });
    expect(screen.queryByRole("button", { name: "Cancel my request" })).toBeNull();
  });

  it("toasts when the cancel fails", async () => {
    state.requests = [request({ requested_by_id: "u1" })];
    state.cancel.mockImplementation((_id: string, opts: { onError: (e: Error) => void }) => opts.onError(new Error("boom")));
    render();
    await screen.findByTestId("transition-approval-banner");

    fireEvent.click(screen.getByRole("button", { name: "Cancel my request" }));

    expect(toast.error).toHaveBeenCalledWith("Could not cancel the request.");
  });
});
