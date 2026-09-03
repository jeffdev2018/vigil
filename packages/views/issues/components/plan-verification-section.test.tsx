// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssuePlanEnvelope, PlanVerification } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Helpers and schema fallbacks live in packages/core/issues/plan.test.ts;
// this checks the section's states.

const state = vi.hoisted(() => ({
  plan: { plan: null, versions: [] } as IssuePlanEnvelope,
  verifications: [] as PlanVerification[],
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/plan", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/plan")>()),
  issuePlanOptions: (wsId: string, issueId: string) => ({
    queryKey: ["issues", wsId, "plan", issueId],
    queryFn: async () => state.plan,
  }),
  planVerificationsOptions: (wsId: string, issueId: string) => ({
    queryKey: ["issues", wsId, "plan-verifications", issueId],
    queryFn: async () => state.verifications,
  }),
}));

import { PlanVerificationSection } from "./plan-verification-section";

const plan = (version: number, superseded = false) => ({
  id: `p${version}`, issue_id: "a", version, content: `Plan text v${version}`, steps: [],
  author_type: "agent", author_id: "ag", superseded_at: superseded ? "2026-09-03T00:00:00Z" : null,
  created_at: "2026-09-03T00:00:00Z",
});

const verification = (over: Partial<PlanVerification> = {}): PlanVerification => ({
  id: "v1", issue_id: "a", plan_id: "p2", plan_version: 2, task_id: "t", source_task_id: "s",
  state: "reported", findings: [], critical_count: 0, major_count: 0, minor_count: 0, outdated_count: 0,
  summary: null, reported_at: "2026-09-03T01:00:00Z", created_at: "2026-09-03T01:00:00Z", ...over,
});

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PlanVerificationSection issueId="a" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.plan = { plan: null, versions: [] };
  state.verifications = [];
});

describe("PlanVerificationSection", () => {
  it("renders nothing without a plan", async () => {
    const { container } = renderSection();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows the active plan, its versions and the no-verification hint", async () => {
    state.plan = { plan: plan(2), versions: [plan(2), plan(1, true)] };
    renderSection();
    expect(await screen.findByText("Plan text v2")).toBeTruthy();
    expect(screen.getByText("Plan v2")).toBeTruthy();
    expect(screen.getByLabelText("Plan version")).toBeTruthy();
    expect(screen.getByText("No verification run yet.")).toBeTruthy();
  });

  it("renders a critical report with findings sorted by severity, unknown kept", async () => {
    state.plan = { plan: plan(2), versions: [plan(2)] };
    state.verifications = [
      verification({
        critical_count: 1, minor_count: 1, summary: "Endpoint missing.",
        findings: [
          { severity: "minor", title: "Naming drift" },
          { severity: "weird", title: "Unknown severity" },
          { severity: "critical", title: "Endpoint missing", files: ["server/x.go"], plan_step_id: "s1" },
        ],
      }),
    ];
    renderSection();
    expect(await screen.findByText("1 critical")).toBeTruthy();
    expect(screen.getByText("Endpoint missing.")).toBeTruthy();
    const titles = screen.getAllByText(/Naming drift|Unknown severity|Endpoint missing$/).map((n) => n.textContent);
    expect(titles).toEqual(["Endpoint missing", "Naming drift", "Unknown severity"]);
    expect(screen.getByText("weird")).toBeTruthy();
  });

  it("shows a running badge and a failed hint", async () => {
    state.plan = { plan: plan(1), versions: [plan(1)] };
    state.verifications = [verification({ state: "running", reported_at: null })];
    const { unmount } = renderSection();
    expect(await screen.findByText("Verifying")).toBeTruthy();
    unmount();

    state.verifications = [verification({ state: "failed", reported_at: null })];
    renderSection();
    expect(await screen.findByText("Verification failed")).toBeTruthy();
    expect(screen.getByText(/Rerun the issue/)).toBeTruthy();
  });
});
