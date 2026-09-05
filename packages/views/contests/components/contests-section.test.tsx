// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Contest } from "@multica/core/issues/contest";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ contests: [] as Contest[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/contest", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/contest")>()),
  issueContestsOptions: () => ({ queryKey: ["contests"], queryFn: async () => state.contests }),
  useConfirmContest: () => ({ mutate: vi.fn(), isPending: false }),
}));

import { ContestsSection } from "./contests-section";

const contest = (over: Partial<Contest> = {}): Contest => ({
  id: "c1", workspace_id: "ws-1", project_id: null, issue_id: "i1", target_type: "task_result", target_id: "t1", target_excerpt: "",
  author_agent_id: "a1", author_provider: "claude", challenger_kind: "agent", challenger_agent_id: "a2", challenger_provider: "codex", same_vendor: false,
  challenger_task_id: null, answer_task_id: null, round: 1, max_rounds: 1, objections: [], answers: [], nothing_to_contest: "",
  status: "confirmed", human_verdict: "dismissed", verdict_note: "", confirmed_by: null, confirmed_at: null, auto: false, created_by: null,
  created_at: "2026-09-01T10:00:00Z", updated_at: "2026-09-01T10:00:00Z", ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ContestsSection issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.contests = [];
});

describe("ContestsSection", () => {
  it("renders nothing without a contest", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("lists the contests newest first, one panel each", async () => {
    state.contests = [contest({ id: "old", created_at: "2026-09-01T10:00:00Z", target_type: "plan" }), contest({ id: "new", created_at: "2026-09-03T10:00:00Z" })];
    render();
    expect((await screen.findByTestId("contests-section")).textContent).toContain("2 contest(s)");
    const panels = screen.getAllByTestId("contest-panel");
    expect(panels).toHaveLength(2);
    expect(panels[0]?.textContent).toContain("Run result");
    expect(panels[1]?.textContent).toContain("Plan");
  });
});
