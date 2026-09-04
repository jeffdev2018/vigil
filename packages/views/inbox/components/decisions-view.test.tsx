// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { InboxDecisions } from "@multica/core/inbox/queries";
import { renderWithI18n } from "../../test/i18n";

// Parsing: packages/core/inbox/decisions.test.ts. Card semantics: decision-cards-section.test.tsx (K01).

const state = vi.hoisted(() => ({ data: { decisions: [], total: 0 } as InboxDecisions, respond: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => <a href={href} className={className}>{children}</a> }));
vi.mock("@multica/core/inbox/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/inbox/queries")>()),
  inboxDecisionsOptions: () => ({ queryKey: ["inbox-decisions"], queryFn: async () => state.data }),
}));
vi.mock("@multica/core/issues/decisions", () => ({ useRespondIssueDecision: () => ({ mutate: state.respond, isPending: false }) }));

import { DecisionsView } from "./decisions-view";

const card = (id: string, over: Record<string, unknown> = {}) => ({
  inbox_item_id: "ib-" + id, issue_id: "i-" + id, issue_identifier: "ACME-" + id, issue_title: "Issue " + id, risk_score: 80,
  decision: { id, issue_id: "i-" + id, asked_by_type: "agent", asked_by_id: "ag", question: "Drop the legacy table " + id + "?", options: [{ id: "drop", label: "Drop it" }, { id: "keep", label: "Keep it" }], recommended_option_id: "keep", urgency: "high", response: null, responded_at: null, created_at: "2026-09-04T00:00:00Z", ...over },
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DecisionsView />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = { decisions: [], total: 0 };
  state.respond.mockReset();
});

describe("DecisionsView", () => {
  it("celebrates inbox zero", async () => {
    render();
    expect(await screen.findByTestId("inbox-decisions-empty")).toBeTruthy();
  });

  it("shows at most the five cards the server sent, the remainder count, and answers in one click", async () => {
    state.data = { decisions: ["1", "2", "3", "4", "5"].map((id) => card(id)), total: 8 };
    render();
    expect(await screen.findAllByTestId("inbox-decision")).toHaveLength(5);
    expect(screen.getByTestId("inbox-decisions-more").textContent).toBe("3 more waiting after these");
    expect(screen.getByText("ACME-1").getAttribute("href")).toBe("/acme/issues/i-1");
    fireEvent.click(screen.getAllByRole("button", { name: /Keep it · recommended/ })[0]!);
    expect(state.respond).toHaveBeenCalledWith({ issueId: "i-1", decisionId: "1", answer: { option_id: "keep" } }, expect.anything());
  });

  it("lets the human answer with free text from the list", async () => {
    state.data = { decisions: [card("9")], total: 1 };
    render();
    fireEvent.click(await screen.findByText("Answer with something else…"));
    fireEvent.change(screen.getByLabelText("Describe what to do instead"), { target: { value: "Archive it" } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    expect(state.respond).toHaveBeenCalledWith({ issueId: "i-9", decisionId: "9", answer: { modified_text: "Archive it" } }, expect.anything());
  });
});
