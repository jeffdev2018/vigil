// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { Contest } from "@multica/core/issues/contest";
import { renderWithI18n } from "../../test/i18n";

// Row pairing itself: packages/core/issues/contest.test.ts.

const state = vi.hoisted(() => ({ confirm: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/contest", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/contest")>()),
  useConfirmContest: () => ({ mutate: state.confirm, isPending: false }),
}));

import { ContestPanel } from "./contest-panel";

const contest = (over: Partial<Contest> = {}): Contest => ({
  id: "c1", workspace_id: "ws-1", project_id: null, issue_id: "i1", target_type: "task_result", target_id: "t1", target_excerpt: "",
  author_agent_id: "a1", author_provider: "claude", challenger_kind: "agent", challenger_agent_id: "a2", challenger_provider: "codex", same_vendor: false,
  challenger_task_id: "ct", answer_task_id: null, round: 1, max_rounds: 2,
  objections: [
    { n: 1, severity: "high", kind: "missing", claim: "No retry on 503", evidence: "client.ts:40", expected_proof: "a test hitting 503" },
    { n: 2, severity: "low", kind: "risky", claim: "Unbounded cache", evidence: "", expected_proof: "" },
  ],
  answers: [{ n: 1, verdict: "fix", note: "Added backoff", proof: "client.test.ts" }],
  nothing_to_contest: "", status: "answered", human_verdict: null, verdict_note: "", confirmed_by: null, confirmed_at: null, auto: false, created_by: "u1",
  created_at: "2026-09-01T10:00:00Z", updated_at: "2026-09-01T10:00:00Z", ...over,
});

beforeEach(() => state.confirm.mockReset());

describe("ContestPanel", () => {
  it("pairs each objection with the author's answer side by side", () => {
    renderWithI18n(<ContestPanel contest={contest()} />);
    const objections = screen.getAllByTestId("contest-objection");
    const answers = screen.getAllByTestId("contest-answer");
    expect(objections).toHaveLength(2);
    expect(answers).toHaveLength(2);
    expect(objections[0]?.textContent).toContain("No retry on 503");
    expect(objections[0]?.textContent).toContain("high");
    expect(objections[0]?.textContent).toContain("a test hitting 503");
    expect(answers[0]?.textContent).toContain("fix");
    expect(answers[0]?.textContent).toContain("Added backoff");
    expect(answers[1]?.textContent).toContain("no answer");
    expect(screen.getByText("challenged by codex")).toBeTruthy();
    expect(screen.getByText("round 1/2")).toBeTruthy();
    expect(screen.getByTestId("contest-status").textContent).toBe("answered");
  });

  it("explains a missing answer while the author is still answering, or when there is no author", () => {
    renderWithI18n(<ContestPanel contest={contest({ status: "answering", answers: [] })} />);
    expect(screen.getAllByText("waiting for the author")).toHaveLength(2);
    expect(screen.queryByTestId("contest-verdict-form")).toBeNull();
    renderWithI18n(<ContestPanel contest={contest({ id: "c2", author_agent_id: null, challenger_kind: "llm", same_vendor: true, answers: [], status: "objections_ready" })} />);
    expect(screen.getAllByText("no author to answer")).toHaveLength(2);
    expect(screen.getByText("service model")).toBeTruthy();
    expect(screen.getByText("same vendor, another model")).toBeTruthy();
  });

  it("sends the human verdict with its note", () => {
    renderWithI18n(<ContestPanel contest={contest()} />);
    fireEvent.change(screen.getByPlaceholderText("Optional note"), { target: { value: "backoff is enough" } });
    fireEvent.click(screen.getByRole("button", { name: "Mixed" }));
    expect(state.confirm).toHaveBeenCalledWith({ id: "c1", verdict: "mixed", note: "backoff is enough" }, expect.anything());
  });

  it("shows the verdict once confirmed, and the nothing-to-contest text", () => {
    renderWithI18n(<ContestPanel contest={contest({ status: "confirmed", human_verdict: "upheld", verdict_note: "ship the fix", confirmed_at: "2026-09-02T08:00:00Z" })} />);
    expect(screen.queryByTestId("contest-verdict-form")).toBeNull();
    const verdict = screen.getByTestId("contest-verdict");
    expect(verdict.textContent).toContain("Upheld");
    expect(verdict.textContent).toContain("ship the fix");
    expect(verdict.textContent).toContain("Verdict given");
    renderWithI18n(<ContestPanel contest={contest({ id: "c3", status: "confirmed", human_verdict: "dismissed", objections: [], answers: [], nothing_to_contest: "" })} />);
    expect(screen.getByTestId("contest-nothing").textContent).toBe("Nothing to contest");
  });
});
