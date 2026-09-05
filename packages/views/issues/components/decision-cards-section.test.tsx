// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueDecision } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Helpers and schema fallbacks: packages/core/issues/decisions.test.ts.

const state = vi.hoisted(() => ({
  decisions: [] as IssueDecision[],
  respond: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/decisions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/decisions")>()),
  issueDecisionsOptions: (wsId: string, issueId: string) => ({
    queryKey: ["issues", wsId, "decisions", issueId],
    queryFn: async () => state.decisions,
  }),
  useRespondIssueDecision: () => ({ mutate: state.respond, isPending: false }),
}));

import { DecisionCardsSection } from "./decision-cards-section";

const card = (over: Partial<IssueDecision> = {}): IssueDecision => ({
  id: "d1", issue_id: "a", asked_by_type: "agent", asked_by_id: "ag", question: "Drop the legacy table?",
  options: [{ id: "drop", label: "Drop it", impact: "irreversible" }, { id: "keep", label: "Keep it" }],
  recommended_option_id: "keep", urgency: "high", response: null, responded_at: null,
  created_at: "2026-09-03T00:00:00Z", ...over,
});

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DecisionCardsSection issueId="a" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.decisions = [];
  state.respond.mockReset();
});

describe("DecisionCardsSection", () => {
  it("renders nothing without a card", async () => {
    const { container } = renderSection();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows a pending card with one button per option and answers with the clicked option", async () => {
    state.decisions = [card()];
    renderSection();
    expect(await screen.findByText("Drop the legacy table?")).toBeTruthy();
    expect(screen.getByText(/Keep it/).textContent).toContain("recommended");
    fireEvent.click(screen.getByRole("button", { name: /Drop it/ }));
    expect(state.respond).toHaveBeenCalledWith(
      { issueId: "a", decisionId: "d1", answer: { option_id: "drop" } },
      expect.anything(),
    );
  });

  it("lets the human answer with free text", async () => {
    state.decisions = [card()];
    renderSection();
    fireEvent.click(await screen.findByText("Answer with something else…"));
    fireEvent.change(screen.getByLabelText("Describe what to do instead"), { target: { value: "Archive it" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(state.respond).toHaveBeenCalledWith(
      { issueId: "a", decisionId: "d1", answer: { modified_text: "Archive it" } },
      expect.anything(),
    );
  });

  it("shows an answered card with its recorded answer and no buttons", async () => {
    state.decisions = [card({ response: { option_id: "keep" }, responded_at: "2026-09-03T01:00:00Z", resume_task_id: "t9" })];
    renderSection();
    expect(await screen.findByText("Keep it")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Drop it/ })).toBeNull();
    expect(screen.getByText(/agent resumed/)).toBeTruthy();
    expect(screen.getByTestId("decision-card").getAttribute("data-pending")).toBe("false");
  });

  it("keeps an interview's questions together with its progress (K13)", async () => {
    state.decisions = [
      card({ id: "q2", question: "Which format?", interview_group_id: "g1", interview_position: 2 }),
      card({ id: "q1", question: "Include archived?", interview_group_id: "g1", interview_position: 1, response: { option_id: "keep" }, responded_at: "x" }),
      card({ id: "single", question: "Drop the table?" }),
    ];
    renderSection();
    const group = await screen.findByTestId("decision-interview");
    expect(group.dataset.answered).toBe("1");
    expect(group.textContent).toContain("Requirement interview");
    const questions = Array.from(group.querySelectorAll("[data-testid='decision-card']")).map((c) => c.getAttribute("data-pending"));
    expect(questions).toEqual(["false", "true"]);
    expect(screen.getByText("Drop the table?").closest("[data-testid='decision-interview']")).toBeNull();
  });

  it("shows the escalation state on an overdue card (K35)", async () => {
    state.decisions = [card({ escalation_level: 2, escalated_at: "2026-09-03T02:00:00Z", sla_deadline_at: "2026-09-03T01:00:00Z" })];
    renderSection();
    const sla = await screen.findByTestId("decision-sla");
    expect(sla.dataset.sla).toBe("escalated_leads");
    expect(sla.textContent).toContain("escalated to the workspace leads");
  });
});
