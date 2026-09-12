// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueGoal } from "@multica/core/types";
import { legKeys, type WorkflowLegs } from "@multica/core/issues/legs";
import { renderWithI18n } from "../../test/i18n";

// Client parsing, mutation wiring and pure helpers: packages/core/issues/goal-loop.test.ts.

const state = vi.hoisted(() => ({
  goal: null as IssueGoal | null,
  setGoal: vi.fn(),
  pause: vi.fn(),
  resume: vi.fn(),
  answer: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/goal-loop", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/goal-loop")>()),
  issueGoalOptions: () => ({ queryKey: ["goal"], queryFn: async () => state.goal }),
  useSetIssueGoal: () => ({ mutate: state.setGoal, isPending: false }),
  usePauseIssueGoal: () => ({ mutate: state.pause, isPending: false }),
  useResumeIssueGoal: () => ({ mutate: state.resume, isPending: false }),
  useAnswerIssueGoal: () => ({ mutate: state.answer, isPending: false }),
}));

import { GoalSection } from "./goal-section";

const goal = (over: Partial<IssueGoal> = {}): IssueGoal => ({
  id: "g1", issue_id: "i1", goal: "Ship the CSV export", status: "active",
  continuation: 2, max_continuations: 8, no_progress: 0, last_outcome: "continued",
  evidence: ["ran unit tests", "checked lint", "opened the PR", "asked for review"],
  set_by_type: "member", updated_at: "2026-09-05T00:00:00Z", ...over,
});

function render(issue: { assignee_type: "agent" | "member" | null; assignee_id: string | null } = { assignee_type: "agent", assignee_id: "a1" }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return { qc, ...renderWithI18n(
    <QueryClientProvider client={qc}>
      <GoalSection issueId="i1" issue={issue} />
    </QueryClientProvider>,
  ) };
}

describe("GoalSection", () => {
  beforeEach(() => {
    state.goal = null;
    state.setGoal.mockReset();
    state.pause.mockReset();
    state.resume.mockReset();
    state.answer.mockReset();
  });

  it("renders nothing when there is no goal and no agent assignee", async () => {
    const { container } = render({ assignee_type: "member", assignee_id: "m1" });
    await new Promise((r) => setTimeout(r, 0));
    expect(container.querySelector('[data-testid="goal-section"]')).toBeNull();
  });

  it("offers a compact set-goal form when an agent is assigned but no goal exists, and saves it", async () => {
    render();
    await screen.findByTestId("goal-section");
    fireEvent.change(screen.getByLabelText("Goal"), { target: { value: "Ship the CSV export" } });
    fireEvent.change(screen.getByLabelText("Max continuations"), { target: { value: "5" } });
    fireEvent.click(screen.getByText("Save goal"));
    expect(state.setGoal).toHaveBeenCalledWith({ goal: "Ship the CSV export", max_continuations: 5 }, expect.anything());
  });

  it("shows the goal, status, progress, and collapses evidence beyond 3 with a working toggle", async () => {
    state.goal = goal();
    render();
    const section = await screen.findByTestId("goal-section");
    expect(section.getAttribute("data-status")).toBe("active");
    expect(screen.getByText("Ship the CSV export")).toBeTruthy();
    expect(screen.getByText("Active")).toBeTruthy();
    expect(screen.getByText("Continuation 2 / 8")).toBeTruthy();
    // Regression: evidence lists collapse to the first 3 entries with a
    // "show all" toggle that reveals the rest and re-collapses on a second click.
    expect(screen.getByText("ran unit tests")).toBeTruthy();
    expect(screen.getByText("checked lint")).toBeTruthy();
    expect(screen.getByText("opened the PR")).toBeTruthy();
    expect(screen.queryByText("asked for review")).toBeNull();
    fireEvent.click(screen.getByText("Show all (4)"));
    expect(screen.getByText("asked for review")).toBeTruthy();
    fireEvent.click(screen.getByText("Show less"));
    expect(screen.queryByText("asked for review")).toBeNull();
  });

  it("shows Pause for an active goal and calls the mutation", async () => {
    state.goal = goal({ status: "active" });
    render();
    await screen.findByTestId("goal-section");
    expect(screen.queryByText("Resume")).toBeNull();
    fireEvent.click(screen.getByText("Pause"));
    expect(state.pause).toHaveBeenCalledWith(undefined, expect.anything());
  });

  it("shows Resume for a paused goal and calls the mutation", async () => {
    state.goal = goal({ status: "paused" });
    render();
    await screen.findByTestId("goal-section");
    expect(screen.queryByText("Pause")).toBeNull();
    fireEvent.click(screen.getByText("Resume"));
    expect(state.resume).toHaveBeenCalledWith(undefined, expect.anything());
  });

  it("renders a choice question and sends the clicked option as the answer", async () => {
    state.goal = goal({
      status: "waiting_user",
      question: { kind: "choice", prompt: "Which environment?", options: ["staging", "prod"], run_id: "t1", asked_at: "2026-09-05T00:00:00Z" },
    });
    render();
    await screen.findByTestId("goal-question");
    expect(screen.getByText("Which environment?")).toBeTruthy();
    fireEvent.click(screen.getByText("staging"));
    expect(state.answer).toHaveBeenCalledWith("staging", expect.anything());
  });

  it("renders a text question and sends the typed answer", async () => {
    state.goal = goal({
      status: "waiting_user",
      question: { kind: "text", prompt: "What's the target date?", run_id: "t1", asked_at: "2026-09-05T00:00:00Z" },
    });
    render();
    await screen.findByTestId("goal-question");
    fireEvent.change(screen.getByLabelText("Answer"), { target: { value: "next Friday" } });
    fireEvent.click(screen.getByText("Answer"));
    expect(state.answer).toHaveBeenCalledWith("next Friday", expect.anything());
  });

  it("shows an already-answered question with who and when", async () => {
    state.goal = goal({
      status: "waiting_user",
      question: {
        kind: "text", prompt: "What's the target date?", run_id: "t1", asked_at: "2026-09-05T00:00:00Z",
        answer: "next Friday", answered_by: "u1", answered_at: "2026-09-05T01:00:00Z",
      },
    });
    render();
    expect(await screen.findByText(/next Friday/)).toBeTruthy();
    expect(screen.getByText(/Answered by u1/)).toBeTruthy();
  });

  it("reuses the leg totals as the chain cost line when chain_root_task_id is set", async () => {
    state.goal = goal({ chain_root_task_id: "root-1" });
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const workflow: WorkflowLegs = {
      root_task_id: "root-1",
      legs: [],
      totals: { legs: 3, cost_usd_ticks: 5_000_000_000, input_tokens: 100, output_tokens: 10, duration_seconds: 90 },
    };
    qc.setQueryData(legKeys.workflow("ws-1", "root-1"), workflow);
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <GoalSection issueId="i1" issue={{ assignee_type: "agent", assignee_id: "a1" }} />
      </QueryClientProvider>,
    );
    await screen.findByTestId("goal-section");
    expect(await screen.findByText("Workflow")).toBeTruthy();
    expect(screen.getByText(/3 runs/)).toBeTruthy();
  });
});
