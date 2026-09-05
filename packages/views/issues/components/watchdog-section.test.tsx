// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Watchdog, WatchdogVerdict } from "@multica/core/issues/watchdog";
import { renderWithI18n } from "../../test/i18n";

// Client parsing and outcome naming: packages/core/issues/watchdog.test.ts.

const state = vi.hoisted(() => ({
  watchdog: null as Watchdog | null,
  verdicts: [] as WatchdogVerdict[],
  save: vi.fn(),
  scan: vi.fn(),
  review: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "a1", name: "Builder" }, { id: "a2", name: "Auditor" }] }),
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [{ id: "m1", user_id: "u1", role: "owner", user: { id: "u1", name: "Jeff", email: "j@x" } }] }),
}));
vi.mock("@multica/core/issues/watchdog", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/watchdog")>()),
  issueWatchdogOptions: () => ({ queryKey: ["wd"], queryFn: async () => state.watchdog }),
  issueWatchdogVerdictsOptions: () => ({ queryKey: ["wd-verdicts"], queryFn: async () => state.verdicts }),
  useSetIssueWatchdog: () => ({ mutate: state.save, isPending: false }),
  useDeleteIssueWatchdog: () => ({ mutate: vi.fn(), isPending: false }),
  useScanIssueWatchdogNow: () => ({ mutate: state.scan, isPending: false }),
  useReviewWatchdogVerdict: () => ({ mutate: state.review, isPending: false }),
}));

import { WatchdogSection } from "./watchdog-section";

const watchdog = (over: Partial<Watchdog> = {}): Watchdog => ({
  id: "w1", issue_id: "i1", agent_id: "a2", agent_name: "Auditor", owner_id: "u1", instructions: "", rest_minutes: 30, enabled: true,
  last_scan_task_id: "t1", last_scanned_at: "2026-09-05T00:00:00Z", motion_streak: 1, created_at: "2026-09-05T00:00:00Z", ...over,
});
const verdict = (over: Partial<WatchdogVerdict> = {}): WatchdogVerdict => ({
  id: "v1", watchdog_id: "w1", issue_id: "i1", task_id: "t1", verdict: "motion", summary: "Done without proof.", decision_id: null, human_review: "pending",
  findings: [{ issue: "JEF-12", issue_id: "c1", action: "reopen", reason: "no proof", missing_criterion: "c1" }], dropped: [{ issue: "JEF-99", issue_id: "", action: "reopen", reason: "", missing_criterion: "" }],
  applied: { reopened: 1, asked_proof: 0 }, contract_revision: 1, created_at: "2026-09-05T00:00:00Z", ...over,
});

function render(canManage = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <WatchdogSection issueId="i1" canManage={canManage} />
    </QueryClientProvider>,
  );
}

describe("WatchdogSection", () => {
  beforeEach(() => {
    state.watchdog = null;
    state.verdicts = [];
    state.save.mockReset();
    state.scan.mockReset();
    state.review.mockReset();
  });

  it("offers the form when no watchdog exists and saves the chosen agent and owner", async () => {
    render();
    await screen.findByTestId("watchdog-form");
    fireEvent.change(screen.getByLabelText("Watchdog agent"), { target: { value: "a2" } });
    fireEvent.change(screen.getByLabelText("Rest (minutes)"), { target: { value: "45" } });
    fireEvent.click(screen.getByText("Save watchdog"));
    expect(state.save).toHaveBeenCalledWith({ agent_id: "a2", owner_id: undefined, instructions: "", rest_minutes: 45, enabled: true }, expect.anything());
  });

  it("hides everything for viewers when nothing is configured", async () => {
    const { container } = render(false);
    await new Promise((r) => setTimeout(r, 0));
    expect(container.querySelector('[data-testid="watchdog"]')).toBeNull();
  });

  it("shows the verdicts with what they did, the ignored findings, and lets the owner review", async () => {
    state.watchdog = watchdog();
    state.verdicts = [verdict(), verdict({ id: "v2", verdict: "escalate", applied: { escalated: true }, findings: [], dropped: [], human_review: "overturned" })];
    render();
    await screen.findByTestId("watchdog");
    const rows = await screen.findAllByTestId("watchdog-verdict");
    expect(rows).toHaveLength(2);
    expect(rows[0]?.getAttribute("data-outcome")).toBe("reopened");
    expect(screen.getByText(/missing criterion: c1/)).toBeTruthy();
    expect(screen.getByText("1 finding(s) outside the tree were ignored")).toBeTruthy();
    expect(screen.getByText("overturned by a human")).toBeTruthy();
    expect(screen.getByText(/1 relaunch\(es\) in a row/)).toBeTruthy();
    fireEvent.click(screen.getAllByText("Confirm")[0]!);
    expect(state.review).toHaveBeenCalledWith({ verdictId: "v1", confirmed: true }, expect.anything());
    fireEvent.click(screen.getByText("Scan now"));
    expect(state.scan).toHaveBeenCalled();
  });
});
