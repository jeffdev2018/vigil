// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider, keepPreviousData } from "@tanstack/react-query";
import type { ReviewFlag, ReviewFlagList } from "@multica/core/review-flags";
import { renderWithI18n } from "../../test/i18n";

// Parsing, severity/state normalisation and the location label are covered
// canonically in packages/core/review-flags/schemas.test.ts.

const state = vi.hoisted(() => ({
  list: { flags: [], counts: { bug: 0, warning: 0, info: 0 } } as ReviewFlagList,
  setState: vi.fn(),
  lastFilter: "" as string,
  fails: false,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/review-flags", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/review-flags")>()),
  reviewFlagsOptions: (_wsId: string, _issueId: string, filter: string) => {
    state.lastFilter = filter;
    return {
      queryKey: ["review-flags", filter, state.fails],
      queryFn: async () => {
        if (state.fails) throw new Error("boom");
        return state.list;
      },
      // Mirrors the real options: switching the filter must not blank the
      // section while the new list loads.
      placeholderData: keepPreviousData,
    };
  },
  useSetReviewFlagState: () => ({ mutate: state.setState, isPending: false }),
}));

import { ReviewFlagsSection } from "./review-flags-section";

const flag = (over: Partial<ReviewFlag> = {}): ReviewFlag => ({
  id: "f1",
  issue_id: "i1",
  pr_source: "github",
  pr_id: "pr1",
  head_sha: "abc123",
  file_path: "src/checkout/total.py",
  line_start: 42,
  line_end: 48,
  side: "new",
  severity: "bug",
  confidence: 80,
  title: "the retry path swallows the error",
  body: "",
  author_agent_id: "a1",
  author_user_id: "",
  task_id: "t1",
  state: "open",
  resolved_by_type: "",
  resolved_by_id: "",
  resolved_at: "",
  created_at: "",
  updated_at: "",
  ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ReviewFlagsSection issueId="i1" />
    </QueryClientProvider>,
  );
}

async function open() {
  render();
  fireEvent.click(await screen.findByRole("button", { name: /Review flags/ }));
}

beforeEach(() => {
  state.list = { flags: [], counts: { bug: 0, warning: 0, info: 0 } };
  state.setState.mockReset();
  state.lastFilter = "";
  state.fails = false;
});

describe("ReviewFlagsSection", () => {
  it("renders nothing when the issue has no flags", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows the open counts in the header without opening the section", async () => {
    state.list = { flags: [flag()], counts: { bug: 2, warning: 1, info: 0 } };
    render();
    const counts = await screen.findByTestId("review-flags-counts");
    // Only the severities that have open flags get a dot.
    expect(counts.textContent).toBe("21");
    expect(screen.queryByTestId("review-flag")).toBeNull();
  });

  it("renders the rows in the order the server sent them", async () => {
    // The sort is the server's: severity, then confidence descending. A row
    // order derived here would diverge from the counts above it.
    state.list = {
      flags: [
        flag({ id: "a", severity: "bug", confidence: 90, title: "first", file_path: "b.go" }),
        flag({ id: "b", severity: "bug", confidence: 20, title: "second", file_path: "a.go" }),
        flag({ id: "c", severity: "warning", confidence: null, title: "third", file_path: "w.go" }),
        flag({ id: "d", severity: "info", confidence: 99, title: "fourth", file_path: "z.go" }),
      ],
      counts: { bug: 2, warning: 1, info: 1 },
    };
    await open();
    const rows = await screen.findAllByTestId("review-flag");
    expect(rows.map((r) => r.getAttribute("data-severity"))).toEqual(["bug", "bug", "warning", "info"]);
    expect(rows.map((r) => r.querySelector("span.text-body")?.textContent)).toEqual([
      "first", "second", "third", "fourth",
    ]);
    // A stated confidence gets a chip; "did not say" gets none.
    expect(rows[0]?.querySelector('[data-testid="review-flag-confidence"]')?.textContent).toBe("90% sure");
    expect(rows[2]?.querySelector('[data-testid="review-flag-confidence"]')).toBeNull();
    // The location keeps the range so it can be pasted into an editor.
    expect(rows[0]?.textContent).toContain("b.go:42-48");
  });

  it("renders an unknown severity as info rather than hiding it", async () => {
    state.list = { flags: [flag({ severity: "critical" })], counts: { bug: 0, warning: 0, info: 1 } };
    await open();
    const row = await screen.findByTestId("review-flag");
    expect(row.getAttribute("data-severity")).toBe("info");
  });

  it("marks a stale flag as code-changed and keeps it readable", async () => {
    // Zero open counts: the section stays alive because the stale flag is
    // still a record a reviewer may need to read.
    state.list = { flags: [flag({ state: "stale" })], counts: { bug: 0, warning: 0, info: 0 } };
    await open();
    const row = await screen.findByTestId("review-flag");
    expect(row.getAttribute("data-state")).toBe("stale");
    expect(screen.getByTestId("review-flag-stale").textContent).toBe("code changed");
    // Still shows what was found — a reviewer justifying a merge needs it.
    expect(row.textContent).toContain("the retry path swallows the error");
  });

  it("resolves an open flag through the row menu", async () => {
    state.list = { flags: [flag()], counts: { bug: 1, warning: 0, info: 0 } };
    await open();
    fireEvent.click(await screen.findByRole("button", { name: "Flag actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Resolve" }));
    expect(state.setState).toHaveBeenCalledWith({ flagId: "f1", state: "resolved" });
  });

  it("offers only reopen on a settled flag", async () => {
    state.list = { flags: [flag({ state: "resolved" })], counts: { bug: 0, warning: 0, info: 0 } };
    await open();
    fireEvent.click(await screen.findByRole("button", { name: "Flag actions" }));
    expect(screen.queryByRole("menuitem", { name: "Resolve" })).toBeNull();
    fireEvent.click(await screen.findByRole("menuitem", { name: "Reopen" }));
    expect(state.setState).toHaveBeenCalledWith({ flagId: "f1", state: "open" });
  });

  it("asks the server for the all filter when the reviewer switches to it", async () => {
    state.list = { flags: [flag()], counts: { bug: 1, warning: 0, info: 0 } };
    await open();
    expect(state.lastFilter).toBe("open");
    fireEvent.click(await screen.findByRole("button", { name: "All" }));
    expect(state.lastFilter).toBe("all");
    // The selected tab stays identifiable, which is what keeps it from
    // reading as a plain hover once the pointer lands on it.
    expect(screen.getByRole("button", { name: "All" }).getAttribute("data-active")).toBe("true");
  });

  it("offers a retry when the list could not be loaded", async () => {
    // A silent section here would read as "the review found nothing", which is
    // the one thing a failed load must not be mistaken for.
    state.fails = true;
    await open();
    expect(await screen.findByText("The review flags could not be loaded.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
  });
});
