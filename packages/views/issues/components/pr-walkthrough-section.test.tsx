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
  // Anchored discussions (F07) shown under the hunk they point at.
  threads: [] as unknown[],
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/github", () => ({
  issuePullRequestsOptions: () => ({ queryKey: ["prs"], queryFn: async () => ({ pull_requests: state.pullRequests }) }),
}));
vi.mock("@multica/core/pr-walkthrough", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/pr-walkthrough")>()),
  prWalkthroughOptions: () => ({ queryKey: ["wt"], queryFn: async () => state.walkthrough }),
  useRefreshPrWalkthrough: () => ({ mutate: state.refresh, isPending: false }),
  anchoredThreadsOptions: () => ({ queryKey: ["anchored"], queryFn: async () => ({ threads: state.threads }) }),
}));

// The thread itself has its own suite (diff-anchor-thread.test.tsx); what
// matters here is WHICH hunk it lands under.
vi.mock("./diff-anchor-thread", () => ({
  DiffAnchorThread: ({ thread }: { thread: { root: { id: string } } }) => (
    <div data-testid="inline-thread">{thread.root.id}</div>
  ),
  AnchorAskButton: ({ label, onAsk }: { label: string; onAsk: () => void }) => (
    <button type="button" aria-label={label} onClick={onAsk} />
  ),
  AnchorComposer: ({ location }: { location: string }) => (
    <div data-testid="anchor-composer">{location}</div>
  ),
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
  state.threads = [];
});

// A hunk body with real content, so line numbers and the ask affordance have
// something to attach to.
// The two sides deliberately start at different numbers: a hunk whose old and
// new lines share their numbering could not tell a side mistake from a match.
const DIFF_HUNK = {
  old_start: 12,
  new_start: 40,
  lines: [" ctx", "-old line", "+new line", " tail"].join("\n"),
  explanation: "Wraps the call.",
  moved_from: "",
};

function withDiff(threads: unknown[] = []): void {
  state.walkthrough = walkthrough({
    groups: [
      {
        title: "Retry the fetch on a 429",
        kind: "core",
        rationale: "A throttled response is retried instead of failing.",
        files: [{ path: "api/client.go", hunks: [DIFF_HUNK] }],
      },
    ],
  });
  state.threads = threads;
}

const anchoredThread = (over: Record<string, unknown> = {}) => ({
  root: { id: "root-1", content: "why?" },
  replies: [],
  anchor: {
    kind: "diff_line",
    pr_source: "github",
    pr_id: "pr-1",
    head_sha: "abc123",
    file_path: "api/client.go",
    line_start: 41,
    line_end: 41,
    side: "new",
    review_flag_id: null,
  },
  anchor_stale: false,
  ...over,
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

  // Diff-anchored threads (F07 / JEF-21).
  it("numbers each diff line on the side it belongs to", async () => {
    withDiff();
    await open();
    await screen.findByText(/ctx/);
    const rows = screen.getAllByRole("row");
    // old / new / (ask) / text — a deletion has no new number and vice versa.
    const cells = rows.map((r) => Array.from(r.querySelectorAll("td")).map((c) => c.textContent));
    expect(cells).toEqual([
      ["12", "40", "", " ctx"],
      ["13", "", "", "-old line"],
      ["", "41", "", "+new line"],
      ["14", "42", "", " tail"],
    ]);
  });

  it("opens the composer for the line the reviewer asked about", async () => {
    withDiff();
    await open();
    await screen.findByText(/new line/);
    // The added line is row 3; its ask affordance anchors to new-side line 13.
    fireEvent.click(screen.getAllByRole("button", { name: "Ask about this line" })[2]!);
    expect(screen.getByTestId("anchor-composer").textContent).toContain("api/client.go:41");
  });

  it("renders a thread under the hunk its anchor points into", async () => {
    withDiff([anchoredThread()]);
    await open();
    expect((await screen.findAllByTestId("inline-thread"))[0]!.textContent).toBe("root-1");
  });

  it("leaves a thread anchored outside this hunk to the timeline alone", async () => {
    withDiff([
      // Right file, a line this hunk does not cover.
      anchoredThread({ anchor: { ...anchoredThread().anchor, line_start: 900, line_end: 900 } }),
      // Right line number, wrong file.
      anchoredThread({ root: { id: "root-2" }, anchor: { ...anchoredThread().anchor, file_path: "other.go" } }),
      // Right line number, other side of the diff.
      // Line 41 exists on the new side only; asking for it on the old side
      // must not drag the thread into this hunk.
      anchoredThread({ root: { id: "root-3" }, anchor: { ...anchoredThread().anchor, side: "old" } }),
    ]);
    await open();
    await screen.findByText(/new line/);
    expect(screen.queryByTestId("inline-thread")).toBeNull();
  });

  it("renders nothing when the issue has no linked pull request", async () => {
    state.pullRequests = [];
    const { container } = render();
    await waitFor(() => expect(container.textContent).toBe(""));
  });
});
