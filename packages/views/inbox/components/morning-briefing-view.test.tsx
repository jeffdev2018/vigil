// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { MorningBriefing } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Schema fallbacks: packages/core/inbox/briefing.test.ts.

const state = vi.hoisted(() => ({ data: null as MorningBriefing | null, mutate: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/w/issues/${id}`, issueReview: (id: string) => `/w/issues/${id}/review` }) }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a> }));
vi.mock("@multica/core/issues/mutations", () => ({ useUpdateIssue: () => ({ mutate: state.mutate, isPending: false }) }));
vi.mock("@multica/core/inbox/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/inbox/queries")>()),
  morningBriefingOptions: (wsId: string) => ({ queryKey: ["inbox", wsId, "briefing"], queryFn: async () => state.data }),
}));

import { MorningBriefingView } from "./morning-briefing-view";

function renderView() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <MorningBriefingView />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = null;
  state.mutate.mockReset();
});

describe("MorningBriefingView", () => {
  it("hides empty sections and says when there is nothing", async () => {
    state.data = { date: "2026-09-04", merged: [], awaiting_review: [], blocked: [], sent_at: null };
    renderView();
    expect(await screen.findByTestId("briefing-empty")).toBeTruthy();
    expect(screen.queryByTestId("briefing-merged")).toBeNull();
    expect(screen.queryByTestId("briefing-blocked")).toBeNull();
  });

  it("shows the filled sections, links the review row to the cockpit and approves through the status move", async () => {
    state.data = {
      date: "2026-09-04",
      merged: [{ issue_id: "a", identifier: "T-1", title: "Done overnight", status: "done" }],
      awaiting_review: [{ issue_id: "b", identifier: "T-2", title: "Needs eyes", status: "in_review" }],
      blocked: [{ issue_id: "c", identifier: "T-3", title: "Stuck", status: "blocked", reason: "tests failed on CI", pending_decisions: 1 }],
      sent_at: "2026-09-04T06:00:00Z",
    };
    renderView();
    expect(await screen.findByTestId("briefing-merged")).toHaveTextContent("Done overnight");
    expect(screen.getByTestId("briefing-blocked")).toHaveTextContent("tests failed on CI");
    expect(screen.getByTestId("briefing-blocked")).toHaveTextContent("1 decision waiting");
    const review = screen.getByTestId("briefing-review");
    expect(review.querySelector("a")?.getAttribute("href")).toBe("/w/issues/b/review");
    fireEvent.click(screen.getByRole("button", { name: "Approve" }));
    expect(state.mutate).toHaveBeenCalledWith({ id: "b", status: "done" }, expect.anything());
  });
});
