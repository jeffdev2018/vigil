// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WeeklyRetro } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Client parsing: packages/core/inbox/retro.test.ts.

const state = vi.hoisted(() => ({ data: null as WeeklyRetro | null, fail: false, regenerated: 0 }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/w/issues/${id}` }) }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children }: { href: string; children: React.ReactNode }) => <a href={href}>{children}</a> }));
vi.mock("@multica/core/api", () => ({
  api: {
    regenerateWeeklyRetro: vi.fn(async () => {
      state.regenerated++;
      return retro();
    }),
  },
}));
vi.mock("@multica/core/inbox/queries", () => ({
  weeklyRetroOptions: (wsId: string) => ({
    queryKey: ["inbox", wsId, "retro", "latest"],
    queryFn: async () => {
      if (state.fail) throw new Error("boom");
      return state.data;
    },
  }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { WeeklyRetroView } from "./weekly-retro-view";

const retro = (over: Partial<WeeklyRetro> = {}): WeeklyRetro => ({
  week_start: "2026-08-31", week_end: "2026-09-06", runs_total: 3, runs_by_status: { completed: 2, failed: 1 }, median_minutes: 12.4,
  failed: [{ run_id: "r1", issue_id: "i1", identifier: "JEFF-3", title: "Fix billing", status: "failed", agent_id: "a1", minutes: 5, error: "tests red" }],
  agents: [{ agent_id: "a1", name: "Builder", runs_total: 3, runs_failed: 1, runs_accepted: 2, runs_reopened: 0, runs_no_intervention: 2, cost_usd_ticks: 12_500_000_000 }],
  skill_proposals: [], narrative: "A steady week.", generated_at: "2026-09-04T00:00:00Z", ...over,
});

function renderView() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <WeeklyRetroView />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = null;
  state.fail = false;
  state.regenerated = 0;
});

describe("WeeklyRetroView", () => {
  it("offers to generate the first retro", async () => {
    renderView();
    expect(await screen.findByTestId("retro-empty")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Generate now" }));
    expect(await screen.findByTestId("weekly-retro")).toBeTruthy();
    expect(state.regenerated).toBe(1);
  });

  it("shows runs by outcome, failed runs with their reason, agents and the empty skills section", async () => {
    state.data = retro();
    renderView();
    const view = await screen.findByTestId("weekly-retro");
    expect(view.textContent).toContain("A steady week.");
    expect(screen.getAllByTestId("retro-status").map((e) => e.textContent)).toEqual(["completed · 2", "failed · 1"]);
    expect(screen.getByTestId("retro-failed").textContent).toContain("tests red");
    expect(screen.getByRole("link", { name: "JEFF-3" }).getAttribute("href")).toBe("/w/issues/i1");
    expect(screen.getByTestId("retro-agent").textContent).toContain("Builder");
    expect(screen.getByTestId("retro-agent").textContent).toContain("1.25");
    expect(view.textContent).toContain("Skill Miner");
  });

  it("shows an error with a retry", async () => {
    state.fail = true;
    renderView();
    expect(await screen.findByRole("button", { name: "Retry" })).toBeTruthy();
  });
});
