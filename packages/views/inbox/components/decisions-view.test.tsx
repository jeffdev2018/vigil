// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { InboxDecision, InboxDecisions } from "@multica/core/inbox/queries";
import { renderWithI18n } from "../../test/i18n";

// Parsing: packages/core/inbox/decisions.test.ts. Card semantics (answer flows,
// countdown, gate details): approval-card.test.tsx. This suite proves the view
// hands the server's risk-ordered, capped list to the shared ApprovalCard —
// adapted from the decisions endpoint's own shape, one adapter per source
// (JEF-244) — and keeps that ordering.

const state = vi.hoisted(() => ({
  data: { decisions: [], total: 0 } as InboxDecisions,
  include: undefined as string[] | undefined,
  respond: vi.fn(),
  decideTransition: vi.fn(),
  answerGoal: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => "acme",
  paths: { workspace: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }) },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => <a href={href} className={className}>{children}</a> }));
vi.mock("@multica/core/inbox/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/inbox/queries")>()),
  inboxDecisionsOptions: (_wsId: string, include?: string[]) => {
    state.include = include;
    return { queryKey: ["inbox-decisions"], queryFn: async () => state.data };
  },
}));
vi.mock("@multica/core/issues/decisions", () => ({ useRespondIssueDecision: () => ({ mutate: state.respond, isPending: false }) }));
vi.mock("@multica/core/issues/goal-loop", () => ({ useAnswerIssueGoal: () => ({ mutate: state.answerGoal, isPending: false }) }));
vi.mock("@multica/core/issue-transitions", () => ({ useDecideIssueTransitionRequest: () => ({ mutate: state.decideTransition, isPending: false }) }));

import { DecisionsView } from "./decisions-view";

const entry = (id: string, over: Record<string, unknown> = {}): InboxDecision => ({
  inbox_item_id: "ib-" + id, issue_id: "i-" + id, issue_identifier: "ACME-" + id, issue_title: "Issue " + id, risk_score: 80,
  source: "decision", decision: null, transition: null, goal_question: null, ...over,
} as InboxDecision);

const card = (id: string, over: Record<string, unknown> = {}): InboxDecision =>
  entry(id, {
    decision: { id, issue_id: "i-" + id, asked_by_type: "agent", asked_by_id: "ag", question: "Drop the legacy table " + id + "?", options: [{ id: "drop", label: "Drop it" }, { id: "keep", label: "Keep it" }], recommended_option_id: "keep", urgency: "high", response: null, responded_at: null, created_at: "2026-09-04T00:00:00Z", ...over },
  });

const transitionCard = (id: string): InboxDecision =>
  entry(id, {
    source: "transition",
    transition: { request_id: "tr-" + id, from_status: "Backlog", to_status: "In Progress", rule_id: null, approver_roles: ["owner"] },
  });

const goalQuestionCard = (id: string, over: Record<string, unknown> = {}): InboxDecision =>
  entry(id, {
    source: "goal_question",
    goal_question: { kind: "choice", prompt: "Which environment " + id + "?", options: ["staging", "prod"], run_id: "run-" + id, asked_at: "2026-09-04T00:00:00Z", ...over },
  });

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DecisionsView />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = { decisions: [], total: 0 };
  state.include = undefined;
  state.respond.mockReset();
  state.decideTransition.mockReset();
  state.answerGoal.mockReset();
});

describe("DecisionsView", () => {
  it("celebrates inbox zero", async () => {
    render();
    expect(await screen.findByTestId("inbox-decisions-empty")).toBeTruthy();
  });

  it("asks the server for transitions and goal questions too (JEF-244)", async () => {
    render();
    await screen.findByTestId("inbox-decisions-empty");
    expect(state.include).toEqual(["transitions", "goal_questions"]);
  });

  it("shows the cards the server sent, in the server's order, and answers in one click through the shared card", async () => {
    state.data = { decisions: ["1", "2", "3", "4", "5"].map((id) => card(id)), total: 8 };
    render();
    const cards = await screen.findAllByTestId("approval-card");
    expect(cards).toHaveLength(5);
    expect(screen.getByTestId("inbox-decisions-more").textContent).toBe("3 more waiting after these");
    expect(screen.getByText("ACME-1 · Issue 1").getAttribute("href")).toBe("/acme/issues/i-1");
    fireEvent.click(screen.getAllByRole("button", { name: /^Keep it/ })[0]!);
    expect(state.respond).toHaveBeenCalledWith({ issueId: "i-1", decisionId: "1", answer: { option_id: "keep" } }, expect.anything());
  });

  it("lets the human answer with free text from the list", async () => {
    state.data = { decisions: [card("9")], total: 1 };
    render();
    fireEvent.click(await screen.findByText("Answer with something else…"));
    fireEvent.change(screen.getByLabelText("Describe what to do instead"), { target: { value: "Archive it" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(state.respond).toHaveBeenCalledWith({ issueId: "i-9", decisionId: "9", answer: { modified_text: "Archive it" } }, expect.anything());
  });

  it("renders a held status transition and settles it through the transition hook", async () => {
    state.data = { decisions: [transitionCard("1"), card("2")], total: 2 };
    render();
    const cards = await screen.findAllByTestId("approval-card");
    expect(cards[0]!.getAttribute("data-source")).toBe("transition");
    expect(screen.getByText("Move ACME-1 from Backlog to In Progress")).toBeTruthy();
    expect(screen.getByText("Backlog → In Progress")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(state.decideTransition).toHaveBeenCalledWith({ requestId: "tr-1", decision: "approve", note: undefined }, expect.anything());
    expect(state.respond).not.toHaveBeenCalled();
  });

  it("renders a goal-loop question and answers it through the goal hook", async () => {
    state.data = { decisions: [goalQuestionCard("1")], total: 1 };
    render();
    const cardEl = (await screen.findAllByTestId("approval-card"))[0]!;
    expect(cardEl.getAttribute("data-source")).toBe("goal_question");
    expect(screen.getByText("Which environment 1?")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "staging" }));
    expect(state.answerGoal).toHaveBeenCalledWith("staging", expect.anything());
    expect(state.respond).not.toHaveBeenCalled();
  });

  it("answers a text goal question with free text", async () => {
    state.data = { decisions: [goalQuestionCard("1", { kind: "text", options: [] })], total: 1 };
    render();
    fireEvent.change(await screen.findByLabelText("Your answer"), { target: { value: "Ship it Friday" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(state.answerGoal).toHaveBeenCalledWith("Ship it Friday", expect.anything());
  });
});
