// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import type { Cycle, CycleActorCapacity, CycleBurndown, CycleVelocity } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// The capacity math itself is pinned in packages/core/cycles/queries.test.ts;
// this suite covers what the DETAIL has to get right — two separate bars, an
// honest overflow, and a burndown that states what it cannot know. The
// JEF-246 blocks mock the frozen server contract: per-actor capacities saved
// as one full-replace PUT, and a velocity payload with actors, other work,
// and history.

const state = vi.hoisted(() => ({
  cycle: null as Cycle | null,
  burndown: null as CycleBurndown | null,
  capacities: [] as CycleActorCapacity[],
  velocity: null as CycleVelocity | null,
  members: [] as { user_id: string; name: string }[],
  agents: [] as { id: string; name: string; archived_at: string | null }[],
  putMutate: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("@multica/core/properties/queries", () => ({ propertyListOptions: () => ({ queryKey: ["properties"] }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["workspaces", "ws-1", "members"] }),
  agentListOptions: () => ({ queryKey: ["workspaces", "ws-1", "agents"] }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ cycleDetail: (id: string) => `/acme/cycles/${id}` }),
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
// The issue surface pulls in the whole query/table stack; the detail's job is
// to hand it the cycle scope, which is asserted through this stub. It also
// renders `renderEmpty`'s own output (with a stub controller) so the empty
// state copy cycle-detail.tsx supplies is directly testable here — see
// "shows the cycle-specific empty state" below.
vi.mock("../../issues/surface/issue-surface", () => ({
  IssueSurface: ({
    scope,
    renderEmpty,
  }: {
    scope: { type: string; cycleId?: string; projectId?: string };
    renderEmpty?: (ctx: { controller: { openCreateIssue: () => void }; issues: never[] }) => React.ReactNode;
  }) => (
    <div data-testid="issue-surface" data-scope={scope.type} data-cycle={scope.cycleId} data-project={scope.projectId}>
      {renderEmpty?.({ controller: { openCreateIssue: () => {} }, issues: [] })}
    </div>
  ),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) => {
    const key = o.queryKey?.[2];
    if (key === "detail") return { data: state.cycle, isPending: false };
    if (key === "burndown") return { data: state.burndown, isPending: false };
    if (key === "capacities") return { data: state.capacities, isPending: false };
    if (key === "velocity") return { data: state.velocity, isPending: false };
    if (key === "members") return { data: state.members, isPending: false };
    if (key === "agents") return { data: state.agents, isPending: false };
    return { data: [], isPending: false };
  },
}));
vi.mock("@multica/core/cycles", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/cycles")>();
  return {
    ...actual,
    cycleDetailOptions: (_ws: string, id: string) => ({ queryKey: ["cycles", "ws-1", "detail", id] }),
    cycleBurndownOptions: (_ws: string, id: string) => ({ queryKey: ["cycles", "ws-1", "burndown", id] }),
    cycleCapacitiesOptions: (_ws: string, id: string) => ({ queryKey: ["cycles", "ws-1", "capacities", id] }),
    cycleVelocityOptions: (_ws: string, id: string) => ({ queryKey: ["cycles", "ws-1", "velocity", id] }),
    useCloseCycle: () => ({ isPending: false, mutate: vi.fn() }),
    useCreateCycle: () => ({ isPending: false, mutate: vi.fn() }),
    useUpdateCycle: () => ({ isPending: false, mutate: vi.fn() }),
    usePutCycleCapacities: () => ({ isPending: false, mutate: state.putMutate }),
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
  state.capacities = [];
  state.velocity = velocity();
  state.members = [];
  state.agents = [];
  state.putMutate = vi.fn();
});

const velocity = (over: Partial<CycleVelocity> = {}): CycleVelocity => ({
  cycle_id: "c1",
  actors: [],
  other_done_points: 0,
  history: [],
  ...over,
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

  // Regression: cycle-detail.tsx mounted <IssueSurface> with no renderEmpty,
  // so a cycle with zero issues fell through to IssueSurface's own default
  // copy — the "projects" namespace's "No issues linked ... assign existing
  // ones to this project", which names the wrong container inside a cycle.
  it("shows the cycle-specific empty state, not the projects-namespace default", () => {
    renderDetail();
    expect(screen.getByText("No issues in this cycle")).toBeInTheDocument();
    expect(screen.getByText("Create a new issue or assign existing ones to this cycle.")).toBeInTheDocument();
    expect(screen.queryByText("No issues linked")).toBeNull();
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

describe("CycleDetail · capacity by actor", () => {
  it("shows the empty state when no per-actor capacity is declared", () => {
    renderDetail();
    expect(screen.getByTestId("actor-capacity-empty")).toHaveTextContent(
      "No per-actor capacity declared yet.",
    );
  });

  it("saves edited points through the full-replace PUT", () => {
    state.capacities = [
      { actor_type: "member", actor_id: "u-1", name: "Ada", points: 20 },
      { actor_type: "agent", actor_id: "a-1", name: "Mika", points: 40 },
    ];
    renderDetail();
    const rows = screen.getAllByTestId("actor-capacity-row");
    expect(rows).toHaveLength(2);
    expect(rows[0]).toHaveTextContent("Ada");
    fireEvent.change(within(rows[0]!).getByLabelText("Points"), { target: { value: "25" } });
    fireEvent.click(screen.getByRole("button", { name: "Save capacities" }));
    expect(state.putMutate).toHaveBeenCalledWith(
      [
        { actor_type: "member", actor_id: "u-1", points: 25 },
        { actor_type: "agent", actor_id: "a-1", points: 40 },
      ],
      expect.objectContaining({ onError: expect.any(Function) }),
    );
  });

  it("drops a removed row from the PUT body", () => {
    state.capacities = [
      { actor_type: "member", actor_id: "u-1", name: "Ada", points: 20 },
      { actor_type: "agent", actor_id: "a-1", name: "Mika", points: 40 },
    ];
    renderDetail();
    const rows = screen.getAllByTestId("actor-capacity-row");
    fireEvent.click(within(rows[1]!).getByRole("button", { name: "Remove actor" }));
    fireEvent.click(screen.getByRole("button", { name: "Save capacities" }));
    expect(state.putMutate).toHaveBeenCalledWith(
      [{ actor_type: "member", actor_id: "u-1", points: 20 }],
      expect.anything(),
    );
  });

  it("blocks saving while a row has no actor or no valid points", () => {
    state.capacities = [{ actor_type: "member", actor_id: "u-1", name: "Ada", points: 20 }];
    state.members = [{ user_id: "u-2", name: "Grace" }];
    renderDetail();
    // A fresh row with no actor picked yet: saving it would send a row the
    // server cannot attribute, so the button stays disabled.
    fireEvent.click(screen.getByRole("button", { name: "Add actor" }));
    expect(screen.getAllByTestId("actor-capacity-row")).toHaveLength(2);
    expect(screen.getByRole("button", { name: "Save capacities" })).toBeDisabled();

    fireEvent.change(screen.getAllByLabelText("Points")[0]!, { target: { value: "10001" } });
    expect(screen.getByText(/whole number from 0 to 10000/)).toBeInTheDocument();
  });
});

describe("CycleDetail · velocity", () => {
  it("draws one bar per actor, done-only when no capacity was declared", () => {
    state.velocity = velocity({
      actors: [
        { actor_type: "member", actor_id: "u-1", name: "Ada", capacity_points: 20, done_points: 14, done_count: 3 },
        { actor_type: "agent", actor_id: "a-1", name: "Mika", capacity_points: null, done_points: 8, done_count: 2 },
      ],
    });
    renderDetail();
    const section = screen.getByRole("region", { name: "Velocity" });
    const bars = within(section).getAllByTestId("capacity-bar");
    expect(bars.map((b) => b.getAttribute("data-side"))).toEqual(["Ada", "Mika"]);
    expect(bars[0]?.textContent).toContain("14 of 20");
    expect(bars[1]?.textContent).toContain("no capacity declared");
    expect(within(bars[1]!).queryByRole("progressbar")).toBeNull();
  });

  it("shows unassigned or squad work as its own line", () => {
    state.velocity = velocity({ other_done_points: 5 });
    renderDetail();
    expect(screen.getByTestId("velocity-other")).toHaveTextContent(
      "Unassigned or squad work: 5 done",
    );
  });

  it("renders the history strip with links to past cycles", () => {
    state.velocity = velocity({
      history: [
        { cycle_id: "c0", name: "Sprint 12", start_date: "2026-02-16", end_date: "2026-02-27", done_points: 30, done_count: 7 },
      ],
    });
    renderDetail();
    const link = within(screen.getByTestId("velocity-history")).getByRole("link", {
      name: "Sprint 12",
    });
    expect(link).toHaveAttribute("href", "/acme/cycles/c0");
    expect(screen.getByTestId("velocity-history")).toHaveTextContent("30 done");
  });

  it("shows the empty state when nothing is done and no capacity is declared", () => {
    renderDetail();
    expect(screen.getByTestId("velocity-empty")).toHaveTextContent(
      "No done work in this cycle yet.",
    );
  });
});
