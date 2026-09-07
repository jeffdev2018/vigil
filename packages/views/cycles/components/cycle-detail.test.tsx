// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import type { Cycle, CycleBurndown } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// The capacity math itself is pinned in packages/core/cycles/queries.test.ts;
// this suite covers what the DETAIL has to get right — two separate bars, an
// honest overflow, and a burndown that states what it cannot know.

const state = vi.hoisted(() => ({
  cycle: null as Cycle | null,
  burndown: null as CycleBurndown | null,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("@multica/core/properties/queries", () => ({ propertyListOptions: () => ({ queryKey: ["properties"] }) }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
// The issue surface pulls in the whole query/table stack; the detail's job is
// to hand it the cycle scope, which is asserted through this stub.
vi.mock("../../issues/surface/issue-surface", () => ({
  IssueSurface: ({ scope }: { scope: { type: string; cycleId?: string; projectId?: string } }) => (
    <div data-testid="issue-surface" data-scope={scope.type} data-cycle={scope.cycleId} data-project={scope.projectId} />
  ),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) => {
    const key = o.queryKey?.[2];
    if (key === "detail") return { data: state.cycle, isPending: false };
    if (key === "burndown") return { data: state.burndown, isPending: false };
    return { data: [], isPending: false };
  },
}));
vi.mock("@multica/core/cycles", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/cycles")>();
  return {
    ...actual,
    cycleDetailOptions: (_ws: string, id: string) => ({ queryKey: ["cycles", "ws-1", "detail", id] }),
    cycleBurndownOptions: (_ws: string, id: string) => ({ queryKey: ["cycles", "ws-1", "burndown", id] }),
    useCloseCycle: () => ({ isPending: false, mutate: vi.fn() }),
    useCreateCycle: () => ({ isPending: false, mutate: vi.fn() }),
    useUpdateCycle: () => ({ isPending: false, mutate: vi.fn() }),
  };
});

import { CycleDetail } from "./cycle-detail";

const navigationAdapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/cycles/c1",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (p: string) => p,
};

const renderDetail = () =>
  renderWithI18n(
    <NavigationProvider value={navigationAdapter}>
      <CycleDetail cycleId="c1" />
    </NavigationProvider>,
  );

const cycle = (over: Partial<Cycle> = {}): Cycle => ({
  id: "c1", workspace_id: "ws-1", project_id: "p1", name: "Sprint 13", description: "",
  start_date: "2026-03-02", end_date: "2026-03-13", rollover: true, closed_at: null,
  status: "active", late: false, load_unit: "issues", load_property_id: null,
  issue_count: 6, done_count: 2,
  capacity: {
    human: { capacity: 5, load: 3 },
    agent: { capacity: 2, load: 1 },
    unassigned_load: 0,
  },
  created_at: "", updated_at: "", ...over,
});

const burndown = (over: Partial<CycleBurndown> = {}): CycleBurndown => ({
  days: [
    { date: "2026-03-02", remaining_count: 6, remaining_load: 6, ideal_count: 6, ideal_load: 6, human_load: 3, agent_load: 3 },
    { date: "2026-03-03", remaining_count: null, remaining_load: null, ideal_count: 0, ideal_load: 0, human_load: null, agent_load: null },
  ],
  capacity: { human: 5, agent: 2 },
  load_unit: "issues",
  load_property_id: null,
  approximate_before: null,
  ...over,
});

beforeEach(() => {
  state.cycle = cycle();
  state.burndown = burndown();
});

describe("CycleDetail", () => {
  it("draws human and agent capacity as two separate bars", () => {
    renderDetail();
    const bars = screen.getAllByTestId("capacity-bar");
    expect(bars.map((b) => b.getAttribute("data-side"))).toEqual(["People", "Agents"]);
    expect(bars[0]?.textContent).toContain("3 of 5");
    expect(bars[1]?.textContent).toContain("1 of 2");
  });

  it("says how far over capacity a side is instead of clipping the bar", () => {
    state.cycle = cycle({
      capacity: {
        human: { capacity: 4, load: 9 },
        agent: { capacity: 2, load: 1 },
        unassigned_load: 0,
      },
    });
    renderDetail();
    const human = screen.getAllByTestId("capacity-bar")[0]!;
    expect(human.textContent).toContain("5 over capacity");
    // The track still exists — it is full, not missing.
    expect(within(human).getByRole("progressbar")).toHaveAttribute("aria-valuenow", "100");
  });

  it("shows the load only, with no bar, when no capacity was declared", () => {
    state.cycle = cycle({
      capacity: {
        human: { capacity: null, load: 4 },
        agent: { capacity: null, load: 0 },
        unassigned_load: 0,
      },
    });
    renderDetail();
    const human = screen.getAllByTestId("capacity-bar")[0]!;
    expect(human.textContent).toContain("no capacity declared");
    expect(within(human).queryByRole("progressbar")).toBeNull();
  });

  it("says the load unit is issues when the cycle has no load property", () => {
    renderDetail();
    expect(
      screen.getAllByText(/Load is counted in issues/).length,
    ).toBeGreaterThan(0);
  });

  it("names the load property's unit when one is set", () => {
    state.cycle = cycle({ load_unit: "property", load_property_id: "prop-1" });
    state.burndown = burndown({ load_unit: "property", load_property_id: "prop-1" });
    renderDetail();
    expect(
      screen.getAllByText(/number property/).length,
    ).toBeGreaterThan(0);
  });

  it("warns that days before the first snapshot are a flat fill", () => {
    state.burndown = burndown({ approximate_before: "2026-03-05" });
    renderDetail();
    expect(screen.getByTestId("burndown-approximate").textContent).toContain("2026-03-05");
  });

  it("says the burndown has no history rather than drawing a flat zero", () => {
    state.burndown = burndown({
      days: [
        { date: "2026-03-02", remaining_count: null, remaining_load: null, ideal_count: 6, ideal_load: 6, human_load: null, agent_load: null },
      ],
    });
    renderDetail();
    expect(screen.getByText(/only the ideal line is drawn/)).toBeInTheDocument();
  });

  it("scopes the issue surface to the cycle while keeping its project", () => {
    renderDetail();
    const surface = screen.getByTestId("issue-surface");
    expect(surface).toHaveAttribute("data-scope", "cycle");
    expect(surface).toHaveAttribute("data-cycle", "c1");
    expect(surface).toHaveAttribute("data-project", "p1");
  });

  it("offers no close action on an already closed cycle", () => {
    state.cycle = cycle({ status: "closed", closed_at: "2026-03-14T00:00:00Z" });
    renderDetail();
    expect(screen.queryByRole("button", { name: "Close cycle" })).toBeNull();
  });

  it("reports a cycle that no longer exists instead of rendering an empty shell", () => {
    state.cycle = null;
    renderDetail();
    expect(screen.getByText("This cycle no longer exists.")).toBeInTheDocument();
  });
});
