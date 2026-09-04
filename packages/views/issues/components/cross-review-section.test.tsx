// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CrossReview } from "@multica/core/issues/cross-review";
import { renderWithI18n } from "../../test/i18n";

// Parsing and state derivation: packages/core/issues/cross-review.test.ts.

const state = vi.hoisted(() => ({ reviews: [] as CrossReview[], retry: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/cross-review", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/cross-review")>()),
  issueCrossReviewsOptions: () => ({ queryKey: ["cross-reviews"], queryFn: async () => state.reviews }),
  useRetryCrossReview: () => ({ mutate: state.retry, isPending: false }),
}));

import { CrossReviewSection } from "./cross-review-section";

const review = (over: Partial<CrossReview> = {}): CrossReview => ({ task_id: "t1", review_of_task_id: "a1", reviewer_agent_id: "r", reviewer_name: "Codex reviewer", reviewer_provider: "codex", status: "completed", report: null, created_at: "", completed_at: null, ...over });

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CrossReviewSection issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.reviews = [];
  state.retry.mockReset();
});

describe("CrossReviewSection", () => {
  it("renders nothing without a review", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows a discreet badge while the review runs, naming the provider", async () => {
    state.reviews = [review({ status: "running" })];
    render();
    expect((await screen.findByTestId("cross-review")).getAttribute("data-state")).toBe("in_progress");
    expect(screen.getByTestId("cross-review-badge").textContent).toBe("review in progress");
    expect(screen.getByText("by Codex reviewer on codex")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Retry the review" })).toBeNull();
  });

  it("shows the structured report with its verdict once done", async () => {
    state.reviews = [review({ report: { verdict: "request_changes", risks: ["retry path untested"], questions: ["why not reuse the helper?"], suggestions: [], summary: "" } }), review({ task_id: "t0", status: "failed" })];
    render();
    expect((await screen.findByTestId("cross-review")).getAttribute("data-state")).toBe("done");
    expect(screen.getByText("changes requested")).toBeTruthy();
    expect(screen.getByTestId("cross-review-risks").textContent).toContain("retry path untested");
    expect(screen.getByTestId("cross-review-questions").textContent).toContain("why not reuse the helper?");
    expect(screen.queryByTestId("cross-review-suggestions")).toBeNull();
    expect(screen.getByText("1 earlier attempt(s)")).toBeTruthy();
  });

  it("offers a retry when the latest review failed", async () => {
    state.reviews = [review({ status: "failed" })];
    render();
    expect((await screen.findByTestId("cross-review")).getAttribute("data-state")).toBe("failed");
    fireEvent.click(screen.getByRole("button", { name: "Retry the review" }));
    expect(state.retry).toHaveBeenCalled();
  });
});
