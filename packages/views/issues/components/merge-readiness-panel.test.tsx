// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { MergeReadiness } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Schema fallbacks and the "never ready by default" rule are covered in
// packages/core/github/merge-readiness.test.ts; this checks the rendering.

const state = vi.hoisted(() => ({
  data: { prs: [], blockers: [], unresolved_threads: 0, open_todos: 0, ready: false } as MergeReadiness,
}));

vi.mock("@multica/core/github/queries", () => ({
  issueMergeReadinessOptions: (issueId: string) => ({
    queryKey: ["github", "merge-readiness", issueId],
    queryFn: async () => state.data,
    enabled: !!issueId,
  }),
}));

import { MergeReadinessPanel } from "./merge-readiness-panel";

function renderPanel() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <MergeReadinessPanel issueId="issue-1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = { prs: [], blockers: [], unresolved_threads: 0, open_todos: 0, ready: false };
});

describe("MergeReadinessPanel", () => {
  it("shows the green chip only when the server is ready with no blockers", async () => {
    state.data = { ...state.data, ready: true };
    renderPanel();
    expect(await screen.findByText("Ready to merge")).toBeTruthy();
    expect(screen.getByTestId("merge-readiness").querySelector("[data-ready]")?.getAttribute("data-ready")).toBe("true");
  });

  it("lists translated blockers with their counts and folds past five", async () => {
    state.data = {
      prs: [],
      blockers: [
        { kind: "no_pr", label: "No open pull request" },
        { kind: "unresolved_threads", label: "x", count: 2 },
        { kind: "open_todos", label: "x", count: 3 },
        { kind: "blocking_issue", label: "x", issue_identifier: "MUL-9" },
        { kind: "checks_failing", label: "x", pr_number: 42 },
        { kind: "merge_conflict", label: "x", pr_number: 42 },
      ],
      unresolved_threads: 2,
      open_todos: 3,
      ready: false,
    };
    renderPanel();
    expect(await screen.findByText("Not ready to merge")).toBeTruthy();
    expect(screen.getByText("2 unresolved review thread(s)")).toBeTruthy();
    expect(screen.getByText("3 open todo(s) in comments")).toBeTruthy();
    expect(screen.getByText("Blocked by MUL-9")).toBeTruthy();
    expect(screen.getByText("Failing checks on #42")).toBeTruthy();
    expect(screen.queryByText("Merge conflict on #42")).toBeNull();
    fireEvent.click(screen.getByText("+1 more"));
    expect(screen.getByText("Merge conflict on #42")).toBeTruthy();
  });

  it("keeps an unknown blocker kind visible and the chip not ready", async () => {
    state.data = { ...state.data, ready: true, blockers: [{ kind: "policy_hold", label: "Held by release policy" }] };
    renderPanel();
    expect(await screen.findByText("Not ready to merge")).toBeTruthy();
    expect(screen.getByText("Held by release policy")).toBeTruthy();
  });

  it("flags stale snapshot data instead of a false green", async () => {
    state.data = {
      prs: [{ id: "p", source: "github", number: 1, title: "t", html_url: "u", state: "open", mergeable: null,
        merge_state: null, checks: { total: 0, passed: 0, failed: 0, pending: 0 }, stale_snapshot: true, ready: false }],
      blockers: [{ kind: "stale_snapshot", label: "x", pr_number: 1 }],
      unresolved_threads: 0, open_todos: 0, ready: false,
    };
    renderPanel();
    expect(await screen.findByText(/data may be stale/)).toBeTruthy();
    expect(screen.getByText("Stale data on #1")).toBeTruthy();
  });
});
