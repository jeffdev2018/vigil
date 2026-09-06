// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PrWalkthrough } from "@multica/core/pr-walkthrough";
import { renderWithI18n } from "../../test/i18n";

// Parsing, ordering and path truncation: packages/core/pr-walkthrough/schemas.test.ts.

const state = vi.hoisted(() => ({
  pullRequests: [{ id: "pr-1", number: 42, title: "Retry on 429" }] as unknown[],
  walkthrough: undefined as PrWalkthrough | undefined,
  refresh: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/github", () => ({
  issuePullRequestsOptions: () => ({ queryKey: ["prs"], queryFn: async () => ({ pull_requests: state.pullRequests }) }),
}));
vi.mock("@multica/core/pr-walkthrough", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/pr-walkthrough")>()),
  prWalkthroughOptions: () => ({ queryKey: ["wt"], queryFn: async () => state.walkthrough }),
  useRefreshPrWalkthrough: () => ({ mutate: state.refresh, isPending: false }),
}));

import { PrWalkthroughSection } from "./pr-walkthrough-section";

const walkthrough = (over: Partial<PrWalkthrough> = {}): PrWalkthrough => ({
  state: "ready",
  head_sha: "abc123",
  truncated: false,
  omitted_files: 0,
  generated_at: "2026-01-02T03:04:05Z",
  error: "",
  groups: [
    {
      title: "Regenerated client",
      kind: "generated",
      rationale: "Rebuilt from the schema.",
      files: [{ path: "api/generated.go", hunks: [{ old_start: 1, new_start: 1, lines: "@@ -1 +1 @@", explanation: "Regenerated.", moved_from: "" }] }],
    },
    {
      title: "Retry the fetch on a 429",
      kind: "core",
      rationale: "A throttled response is retried instead of failing.",
      files: [{ path: "api/client.go", hunks: [{ old_start: 12, new_start: 12, lines: "@@ -12,7 +12,9 @@", explanation: "Wraps the call.", moved_from: "" }] }],
    },
  ],
  ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PrWalkthroughSection issueId="issue-1" />
    </QueryClientProvider>,
  );
}

async function open() {
  render();
  fireEvent.click(await screen.findByRole("button", { name: /Walkthrough/i }));
}

beforeEach(() => {
  state.pullRequests = [{ id: "pr-1", number: 42, title: "Retry on 429" }];
  state.walkthrough = walkthrough();
  state.refresh.mockReset();
});

describe("PrWalkthroughSection", () => {
  it("stays collapsed until the reviewer opens it", async () => {
    render();
    const toggle = await screen.findByRole("button", { name: /Walkthrough/i });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText("Retry the fetch on a 429")).not.toBeInTheDocument();
  });

  it("renders groups in the fixed order with their hunk explanations", async () => {
    await open();
    await screen.findByText("Retry the fetch on a 429");

    // Acceptance 4: core before generated, whatever order the server sent.
    const titles = screen.getAllByRole("heading", { level: 4 }).map((h) => h.textContent);
    expect(titles).toEqual(["Retry the fetch on a 429", "Regenerated client"]);
    expect(screen.getByText("Wraps the call.")).toBeInTheDocument();
    expect(screen.getByText("A throttled response is retried instead of failing.")).toBeInTheDocument();
  });

  it("shows a skeleton while the run is out", async () => {
    state.walkthrough = walkthrough({ state: "pending", groups: [] });
    await open();
    await waitFor(() => expect(screen.getByLabelText("Writing the walkthrough…")).toBeInTheDocument());
    expect(screen.queryByRole("heading", { level: 4 })).not.toBeInTheDocument();
  });

  it("treats an unknown state from a newer server as pending", async () => {
    state.walkthrough = walkthrough({ state: "summarising", groups: [] });
    await open();
    await waitFor(() => expect(screen.getByLabelText("Writing the walkthrough…")).toBeInTheDocument());
  });

  it("shows the failure and lets the reviewer regenerate", async () => {
    state.walkthrough = walkthrough({ state: "failed", error: "the run ended without a readable block", groups: [] });
    await open();
    await screen.findByText("the run ended without a readable block");
    fireEvent.click(screen.getByRole("button", { name: "Regenerate" }));
    expect(state.refresh).toHaveBeenCalled();
  });

  it("says how much of an over-cap diff was left out, and still shows the rest", async () => {
    // Acceptance 3: truncation is a banner over a partial walkthrough, not an
    // error that replaces it.
    state.walkthrough = walkthrough({ truncated: true, omitted_files: 25 });
    await open();
    await screen.findByText(/25 files omitted/);
    expect(screen.getByText("Retry the fetch on a 429")).toBeInTheDocument();
  });

  it("says so when a ready walkthrough has no groups", async () => {
    state.walkthrough = walkthrough({ groups: [] });
    await open();
    await screen.findByText("No walkthrough for this revision yet.");
  });

  it("renders nothing when the issue has no linked pull request", async () => {
    state.pullRequests = [];
    const { container } = render();
    await waitFor(() => expect(container.textContent).toBe(""));
  });
});
