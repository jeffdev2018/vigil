// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReviewCockpit as Cockpit } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Schema fallbacks and helpers: packages/core/issues/cockpit.test.ts.

const state = vi.hoisted(() => ({
  data: null as Cockpit | null,
  fail: false,
  mutate: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/w/issues/${id}`, issueReview: (id: string) => `/w/issues/${id}/review` }) }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a> }));
vi.mock("@multica/core/issues/mutations", () => ({ useUpdateIssue: () => ({ mutate: state.mutate, isPending: false }) }));
vi.mock("@multica/core/issues/cockpit", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/cockpit")>()),
  reviewCockpitOptions: (wsId: string, issueId: string, runId?: string) => ({
    queryKey: ["issues", wsId, "review-cockpit", issueId, runId ?? ""],
    queryFn: async () => {
      if (state.fail) throw new Error("boom");
      return state.data;
    },
  }),
}));

import { ReviewCockpit } from "./review-cockpit-page";

const cockpit = (over: Partial<Cockpit> = {}): Cockpit => ({
  issue: {
    id: "i1", workspace_id: "ws-1", number: 7, identifier: "T-7", title: "Export CSV", description: null, status: "in_review", priority: "medium",
    assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "u", parent_issue_id: null, project_id: null, position: 0,
    stage: null, start_date: null, due_date: null, metadata: {}, properties: {}, created_at: "2026-09-03T00:00:00Z", updated_at: "2026-09-03T00:00:00Z",
  },
  run: { id: "r1", status: "completed", agent_id: "a", created_at: "2026-09-03T00:00:00Z", started_at: null, completed_at: "2026-09-03T01:00:00Z", error: null },
  runs: [],
  merge_readiness: {
    prs: [{ id: "p", source: "vcs", number: 41, title: "Add export", html_url: "https://forge/pr/41", state: "open", mergeable: null, merge_state: null, checks: { total: 2, passed: 1, failed: 0, pending: 1 }, stale_snapshot: false, ready: false }],
    blockers: [{ kind: "checks_pending", label: "Checks still running", count: 1 }],
    unresolved_threads: 0, open_todos: 0, ready: false,
  },
  usage: { input_tokens: 1010, output_tokens: 201, cache_read_tokens: 0, cache_write_tokens: 0, cost_usd_ticks: 3_000_000_000, uncosted: true },
  open_questions: [{ id: "d", issue_id: "i1", asked_by_type: "agent", asked_by_id: "a", question: "Include archived?", options: [{ id: "y", label: "Yes" }, { id: "n", label: "No" }], urgency: "high", response: null, responded_at: null, created_at: "x" }],
  criteria: [{ id: "c1", text: "Button on the list", proof_state: "satisfied", proof_type: "url", proof_ref: "https://ci/1" }, { id: "c2", text: "Archived excluded", proof_state: "missing" }],
  plan_verification: null,
  self_review: null,
  failed_sections: [],
  ...over,
});

function renderCockpit() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ReviewCockpit issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = cockpit();
  state.fail = false;
  state.mutate.mockReset();
});

describe("ReviewCockpit", () => {
  it("renders every section from one payload and holds approval while checks run", async () => {
    renderCockpit();
    expect(await screen.findByText("Export CSV")).toBeTruthy();
    expect(screen.getByTestId("cockpit-prs").textContent).toContain("#41 Add export");
    expect(screen.getByTestId("cockpit-prs").textContent).toContain("1/2");
    expect(screen.getByTestId("cockpit-cost").textContent).toContain("$0.30");
    expect(screen.getByTestId("cockpit-questions").textContent).toContain("Include archived?");
    const criteria = screen.getByTestId("cockpit-criteria").querySelectorAll("li");
    expect(Array.from(criteria).map((li) => li.getAttribute("data-state"))).toEqual(["satisfied", "missing"]);
    const approve = screen.getByRole("button", { name: "Approve" }) as HTMLButtonElement;
    expect(approve.disabled).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Request changes" }));
    expect(state.mutate).toHaveBeenCalledWith({ id: "i1", status: "in_progress" }, expect.anything());
  });

  it("approves through the ordinary status move once checks are done, and names failed sections", async () => {
    state.data = cockpit({ merge_readiness: { prs: [], blockers: [], unresolved_threads: 0, open_todos: 0, ready: true }, failed_sections: ["usage"], usage: null });
    renderCockpit();
    expect(await screen.findByTestId("cockpit-failed")).toHaveTextContent("usage");
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(state.mutate).toHaveBeenCalledWith({ id: "i1", status: "done" }, expect.anything());
    expect(screen.getByTestId("cockpit-cost").textContent).toContain("No usage recorded");
  });

  it("offers a retry when the cockpit itself fails", async () => {
    state.fail = true;
    renderCockpit();
    expect(await screen.findByRole("button", { name: "Retry" })).toBeTruthy();
  });
});
