// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Issue } from "@multica/core/types";
import { useIssueRoleViewStore } from "@multica/core/issues/role-view-store";
import { renderWithI18n } from "../../test/i18n";

// Store semantics: packages/core/issues/role-view-store.test.ts.

const state = vi.hoisted(() => ({ decisions: [] as unknown[], usage: null as null | Record<string, number>, audit: [] as unknown[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/decisions", () => ({
  issueDecisionsOptions: (wsId: string, issueId: string) => ({ queryKey: ["decisions", wsId, issueId], queryFn: async () => state.decisions }),
}));
vi.mock("@multica/core/issues/queries", () => ({
  issueUsageOptions: (issueId: string) => ({ queryKey: ["usage", issueId], queryFn: async () => state.usage }),
}));
vi.mock("@multica/core/workspace/audit", () => ({
  auditLogInfiniteOptions: (wsId: string, filter: unknown) => ({
    queryKey: ["audit", wsId, JSON.stringify(filter)],
    queryFn: async () => ({ entries: state.audit, next_cursor: "" }),
    initialPageParam: "",
    getNextPageParam: () => undefined,
  }),
}));
vi.mock("../../agents/components/agent-scorecard-section", () => ({ AgentScorecardSection: () => <div data-testid="scorecard" /> }));
vi.mock("./decision-cards-section", () => ({ DecisionCardsSection: () => <div data-testid="decisions" /> }));
vi.mock("./acceptance-criteria-section", () => ({ AcceptanceCriteriaSection: () => <div data-testid="criteria" /> }));
vi.mock("./plan-verification-section", () => ({ PlanVerificationSection: () => <div data-testid="plan" /> }));
vi.mock("./execution-log-section", () => ({ ExecutionLogSection: () => <div data-testid="log" /> }));
vi.mock("./merge-readiness-panel", () => ({ MergeReadinessPanel: () => <div data-testid="merge" /> }));

import { RoleView, RoleViewTabs } from "./role-view";

const issue = { id: "i1", identifier: "JEFF-1", assignee_type: "agent", assignee_id: "a1" } as unknown as Issue;

function render(node: React.ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={qc}>{node}</QueryClientProvider>);
}

beforeEach(() => {
  state.decisions = [];
  state.usage = null;
  state.audit = [];
  useIssueRoleViewStore.getState().setView("full");
});

describe("RoleViewTabs", () => {
  it("selects a preset in the store and marks it active", () => {
    render(<RoleViewTabs />);
    fireEvent.click(screen.getByRole("tab", { name: "CTO" }));
    expect(useIssueRoleViewStore.getState().view).toBe("cto");
    expect(screen.getByRole("tab", { name: "CTO" }).getAttribute("aria-selected")).toBe("true");
    expect(screen.getByRole("tab", { name: "Full" }).getAttribute("aria-selected")).toBe("false");
  });
});

describe("RoleView", () => {
  it("PM falls back to the run log when the issue has no decision card", async () => {
    render(<RoleView view="pm" issue={issue} />);
    expect(await screen.findByTestId("pm-empty")).toBeTruthy();
    expect(screen.getByTestId("log")).toBeTruthy();
    expect(screen.getByTestId("criteria")).toBeTruthy();
  });

  it("PM hides the fallback once a decision exists", async () => {
    state.decisions = [{ id: "d1" }];
    render(<RoleView view="pm" issue={issue} />);
    expect(await screen.findByTestId("decisions")).toBeTruthy();
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.queryByTestId("pm-empty")).toBeNull();
    expect(screen.queryByTestId("log")).toBeNull();
  });

  it("QA shows criteria, the verification report and merge readiness", () => {
    render(<RoleView view="qa" issue={issue} />);
    expect(screen.getByTestId("criteria")).toBeTruthy();
    expect(screen.getByTestId("plan")).toBeTruthy();
    expect(screen.getByTestId("merge")).toBeTruthy();
  });

  it("CTO shows cost, the agent scorecard and the issue's audit trail on one page", async () => {
    state.usage = { total_input_tokens: 1000, total_output_tokens: 500, cost_usd_ticks: 12_500_000_000 };
    state.audit = [{ id: "e1", occurred_at: "2026-09-04T00:00:00Z", action: "issue.status_changed", actor_type: "member" }];
    render(<RoleView view="cto" issue={issue} />);
    expect((await screen.findByText(/\$1\.25/)).textContent).toContain("1,500 tokens");
    expect(screen.getByTestId("scorecard")).toBeTruthy();
    expect((await screen.findAllByTestId("cto-audit-row"))[0]?.textContent).toContain("issue.status_changed");
  });

  it("CTO stays usable without an agent or audit entries", async () => {
    render(<RoleView view="cto" issue={{ ...issue, assignee_type: null, assignee_id: null } as unknown as Issue} />);
    expect(await screen.findByText("No agent assigned")).toBeTruthy();
    expect(await screen.findByText("Nothing recorded on this issue yet")).toBeTruthy();
  });
});
