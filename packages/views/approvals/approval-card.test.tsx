// @vitest-environment jsdom

import { type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enIssues from "../locales/en/issues.json";
import type { ApprovalItem } from "@multica/core/approvals";

// ApprovalCard: one card per source, the right controls for each, the gate
// details fold, the countdown, the "cannot decide" state. The feed shape
// and the countdown arithmetic are proven in packages/core/approvals/schemas.test.ts.

const mockRespond = vi.hoisted(() => vi.fn());
const mockDecideTransition = vi.hoisted(() => vi.fn());
const mockAnswerGoal = vi.hoisted(() => vi.fn());
const mockInvalidate = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ invalidateQueries: mockInvalidate }),
  queryOptions: <T,>(opts: T) => opts,
}));
vi.mock("@multica/core/issues/decisions", () => ({
  useRespondIssueDecision: () => ({ mutate: mockRespond, isPending: false }),
}));
vi.mock("@multica/core/issues/goal-loop", () => ({
  useAnswerIssueGoal: () => ({ mutate: mockAnswerGoal, isPending: false }),
}));
vi.mock("@multica/core/issue-transitions", () => ({
  useDecideIssueTransitionRequest: () => ({ mutate: mockDecideTransition, isPending: false }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => "acme",
  paths: { workspace: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }) },
}));
vi.mock("../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: ReactNode }) => <a href={href}>{children}</a>,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { ApprovalCard, PendingApprovalsBar } from "./approval-card";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };
afterEach(cleanup);
beforeEach(() => vi.clearAllMocks());

function renderUI(children: ReactNode) {
  return render(<I18nProvider locale="en" resources={TEST_RESOURCES}>{children}</I18nProvider>);
}

function item(over: Partial<ApprovalItem> = {}): ApprovalItem {
  return {
    id: "d1",
    source: "decision",
    kind: "gate",
    issue: { id: "i1", identifier: "ONE-7", title: "Clean the CRM", status: "in_progress" },
    task_id: "t1",
    asked_by: { type: "agent", id: "a1", name: "Ops bot" },
    question: "Blocked action · delete_customer",
    options: [{ id: "approve", label: "Approve", impact: "the run continues" }, { id: "deny", label: "Deny", impact: "refused" }],
    recommended_option_id: "",
    urgency: "high",
    created_at: "2026-09-09T10:00:00Z",
    expires_at: new Date(Date.now() + 20 * 60_000).toISOString(),
    sla_deadline_at: null,
    can_decide: true,
    cannot_decide_reason: "",
    decision: null,
    gate: { id: "g1", task_id: "t1", gate_type: "mcp_tool_call", summary: "delete_customer", details: { params: { id: "c-1" }, paths: ["crm/customers"], required_approvals: 2, approvers: ["u9"] }, status: "pending", created_at: "", expires_at: null, resolved_at: null },
    transition: null,
    goal_question: null,
    ...over,
  };
}

describe("ApprovalCard", () => {
  it("answers a gate card with an option and unfolds what the gate would do", async () => {
    const user = userEvent.setup();
    renderUI(<ApprovalCard approval={item()} wsId="ws" showIssue />);
    expect(screen.getByText(enIssues.approvals.kind_gate_tool)).toBeTruthy();
    expect(screen.getByText("ONE-7 · Clean the CRM")).toBeTruthy();
    expect(screen.getByTestId("approval-countdown").textContent).toMatch(/left/);
    expect(screen.getByText(/1\/2 approvals/)).toBeTruthy();

    await user.click(screen.getByText(enIssues.approvals.details_show));
    const details = screen.getByTestId("approval-gate-details");
    expect(details.textContent).toContain("mcp_tool_call");
    expect(details.textContent).toContain("crm/customers");
    expect(details.textContent).toContain('"id": "c-1"');

    await user.click(screen.getByRole("button", { name: /^Approve/ }));
    expect(mockRespond).toHaveBeenCalledWith({ issueId: "i1", decisionId: "d1", answer: { option_id: "approve" } }, expect.anything());
  });

  it("sends a free-text answer on a decision", async () => {
    const user = userEvent.setup();
    renderUI(<ApprovalCard approval={item({ kind: "decision", gate: null, question: "Which colour?" })} wsId="ws" />);
    await user.click(screen.getByText(enIssues.decisions.modify));
    await user.type(screen.getByLabelText(enIssues.decisions.modify_placeholder), "Blue, but darker");
    await user.click(screen.getByRole("button", { name: enIssues.decisions.send }));
    expect(mockRespond).toHaveBeenCalledWith({ issueId: "i1", decisionId: "d1", answer: { modified_text: "Blue, but darker" } }, expect.anything());
  });

  it("approves a held transition with a note", async () => {
    const user = userEvent.setup();
    renderUI(<ApprovalCard approval={item({ id: "r1", source: "transition", kind: "transition", gate: null, question: "Move ONE-7 from in_progress to done", transition: { request_id: "r1", from_status: "in_progress", to_status: "done", rule_id: null, approver_roles: ["owner", "admin"] } })} wsId="ws" />);
    await user.type(screen.getByPlaceholderText(enIssues.approvals.note_placeholder), "ship it");
    await user.click(screen.getByRole("button", { name: enIssues.approvals.approve }));
    expect(mockDecideTransition).toHaveBeenCalledWith({ requestId: "r1", decision: "approve", note: "ship it" }, expect.anything());
  });

  it("answers a goal question by choice or by text", async () => {
    const user = userEvent.setup();
    const { unmount } = renderUI(<ApprovalCard approval={item({ id: "g1", source: "goal_question", kind: "goal_question", gate: null, question: "Which region first?", options: [{ id: "0", label: "EU", impact: "" }, { id: "1", label: "US", impact: "" }], goal_question: { kind: "choice", prompt: "Which region first?", options: ["EU", "US"], run_id: "t1", asked_at: "" } })} wsId="ws" />);
    await user.click(screen.getByRole("button", { name: "US" }));
    expect(mockAnswerGoal).toHaveBeenCalledWith("US", expect.anything());
    unmount();
    renderUI(<ApprovalCard approval={item({ id: "g2", source: "goal_question", kind: "goal_question", gate: null, options: [], goal_question: { kind: "text", prompt: "Name?", options: [], run_id: "t1", asked_at: "" } })} wsId="ws" />);
    await user.type(screen.getByLabelText(enIssues.approvals.answer_placeholder), "Grace");
    await user.click(screen.getByRole("button", { name: enIssues.approvals.send }));
    expect(mockAnswerGoal).toHaveBeenCalledWith("Grace", expect.anything());
  });

  it("explains why the reader cannot decide instead of showing buttons", () => {
    renderUI(<ApprovalCard approval={item({ can_decide: false, cannot_decide_reason: "gate_approvers_policy" })} wsId="ws" />);
    expect(screen.getByTestId("approval-cannot-decide").textContent).toBe(enIssues.approvals.cannot_decide_policy);
    expect(screen.queryByRole("button", { name: /^Approve/ })).toBeNull();
  });

  it("marks an expired ask and offers nothing to click", () => {
    renderUI(<ApprovalCard approval={item({ expires_at: new Date(Date.now() - 60_000).toISOString() })} wsId="ws" />);
    expect(screen.getByTestId("approval-countdown").textContent).toContain(enIssues.approvals.expired);
    expect(screen.queryByRole("button", { name: /^Approve/ })).toBeNull();
  });
});

describe("PendingApprovalsBar", () => {
  it("counts the asks and scrolls to the one clicked", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    renderUI(<PendingApprovalsBar approvals={[item(), item({ id: "d2", question: "Blocked action · git push origin main" })]} onSelect={onSelect} />);
    expect(screen.getByTestId("pending-approvals-bar").textContent).toContain("2 awaiting a decision");
    await user.click(screen.getByRole("button", { name: "git push origin main" }));
    expect(onSelect).toHaveBeenCalledWith("d2");
  });
  it("renders nothing without asks", () => {
    renderUI(<PendingApprovalsBar approvals={[]} onSelect={() => {}} />);
    expect(screen.queryByTestId("pending-approvals-bar")).toBeNull();
  });
});
