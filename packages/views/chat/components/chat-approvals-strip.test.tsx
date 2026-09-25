// @vitest-environment jsdom

import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import type { ApprovalItem } from "@multica/core/approvals";
import { renderWithI18n } from "../../test/i18n";

// The card's own answer flows, gate details, and countdown are proven in
// approval-card.test.tsx. This suite proves only what the strip adds: it
// shows nothing empty, counts and titles what it holds, and collapses.

vi.mock("@tanstack/react-query", () => ({ useQueryClient: () => ({ invalidateQueries: vi.fn() }) }));
vi.mock("@multica/core/issues/decisions", () => ({ useRespondIssueDecision: () => ({ mutate: vi.fn(), isPending: false }) }));
vi.mock("@multica/core/issues/goal-loop", () => ({ useAnswerIssueGoal: () => ({ mutate: vi.fn(), isPending: false }) }));
vi.mock("@multica/core/issue-transitions", () => ({ useDecideIssueTransitionRequest: () => ({ mutate: vi.fn(), isPending: false }) }));
vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => "acme",
  paths: { workspace: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }) },
}));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a> }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { ChatApprovalsStrip } from "./chat-approvals-strip";

function item(over: Partial<ApprovalItem> = {}): ApprovalItem {
  return {
    id: "d1",
    source: "decision",
    kind: "decision",
    issue: { id: "i1", identifier: "ONE-7", title: "Clean the CRM", status: "in_progress" },
    task_id: "t1",
    asked_by: { type: "agent", id: "a1", name: "Ops bot" },
    question: "Which environment?",
    options: [{ id: "staging", label: "Staging", impact: "" }, { id: "prod", label: "Prod", impact: "" }],
    recommended_option_id: "",
    urgency: "normal",
    created_at: "2026-09-09T10:00:00Z",
    expires_at: null,
    sla_deadline_at: null,
    can_decide: true,
    cannot_decide_reason: "",
    decision: null,
    gate: null,
    transition: null,
    goal_question: null,
    ...over,
  };
}

afterEach(cleanup);

describe("ChatApprovalsStrip", () => {
  it("renders nothing when the agent has no pending ask", () => {
    renderWithI18n(<ChatApprovalsStrip approvals={[]} wsId="ws" />);
    expect(screen.queryByTestId("chat-approvals-strip")).toBeNull();
  });

  it("titles itself with the count and shows a card per ask", () => {
    renderWithI18n(<ChatApprovalsStrip approvals={[item(), item({ id: "d2", question: "Which region?" })]} wsId="ws" />);
    expect(screen.getByText("2 pending approvals")).toBeTruthy();
    expect(screen.getAllByTestId("approval-card")).toHaveLength(2);
  });

  it("collapses and expands", () => {
    renderWithI18n(<ChatApprovalsStrip approvals={[item()]} wsId="ws" />);
    expect(screen.getByText("1 pending approval")).toBeTruthy();
    expect(screen.getByTestId("approval-card")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /1 pending approval/ }));
    expect(screen.queryByTestId("approval-card")).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /1 pending approval/ }));
    expect(screen.getByTestId("approval-card")).toBeTruthy();
  });
});
