// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue, OwnershipSuggestion } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Schema fallbacks: packages/core/issues/ownership.test.ts.

const state = vi.hoisted(() => ({
  suggestion: null as OwnershipSuggestion | null,
  mutate: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/mutations", () => ({ useUpdateIssue: () => ({ mutate: state.mutate, isPending: false }) }));
vi.mock("@multica/core/issues/ownership", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/ownership")>()),
  ownershipSuggestionOptions: (wsId: string, issueId: string) => ({
    queryKey: ["issues", wsId, "ownership-suggestion", issueId],
    queryFn: async () => state.suggestion,
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: (wsId: string) => ({ queryKey: ["members", wsId], queryFn: async () => [{ id: "m1", workspace_id: wsId, user_id: "u1", role: "member", created_at: "", name: "Ada", email: "ada@x", avatar_url: null }] }),
  agentListOptions: (wsId: string) => ({ queryKey: ["agents", wsId], queryFn: async () => [{ id: "a1", name: "Billing bot" }] }),
}));

import { OwnershipSuggestionSection } from "./ownership-suggestion-section";

type SectionIssue = Pick<Issue, "id" | "assignee_type" | "assignee_id">;

function renderSection(issue: SectionIssue = { id: "i1", assignee_type: null, assignee_id: null }) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <OwnershipSuggestionSection issue={issue} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.suggestion = null;
  state.mutate.mockReset();
});

describe("OwnershipSuggestionSection", () => {
  it("renders nothing without a matching rule", async () => {
    const { container } = renderSection();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("names the owner and agent, and assigns only on click", async () => {
    state.suggestion = { rule_id: "r1", owner_user_id: "u1", referent_agent_id: "a1", matched: "path:packages/core/billing/invoice.ts", pattern: "packages/core/billing/**" };
    renderSection();
    const section = await screen.findByTestId("ownership-suggestion");
    expect(section.textContent).toContain("packages/core/billing/invoice.ts");
    expect(await screen.findByRole("button", { name: "Assign to Ada" })).toBeTruthy();
    expect(state.mutate).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Assign to Ada" }));
    expect(state.mutate).toHaveBeenCalledWith({ id: "i1", assignee_type: "member", assignee_id: "u1" }, expect.anything());
    fireEvent.click(await screen.findByRole("button", { name: "Assign to Billing bot" }));
    expect(state.mutate).toHaveBeenLastCalledWith({ id: "i1", assignee_type: "agent", assignee_id: "a1" }, expect.anything());
  });

  it("hides once the issue is the owner's", async () => {
    state.suggestion = { rule_id: "r1", owner_user_id: "u1", referent_agent_id: null, matched: "label:l1", pattern: "label:l1" };
    const { container } = renderSection({ id: "i1", assignee_type: "member", assignee_id: "u1" });
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });
});
