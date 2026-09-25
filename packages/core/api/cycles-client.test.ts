import { describe, expect, it, vi, afterEach } from "vitest";
import { ApiClient } from "./client";

// Boundary defence for the F29 cycle endpoints (CLAUDE.md, API Compatibility):
// an installed desktop client can meet a backend whose payload has drifted, and
// the page must keep rendering rather than white-screen on a bad field.

afterEach(() => vi.unstubAllGlobals());

function respondWith(body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
  return new ApiClient("https://api.example.test");
}

const validCycle = {
  id: "cycle-1",
  workspace_id: "ws-1",
  project_id: "proj-1",
  name: "Sprint 1",
  description: "",
  start_date: "2026-03-02",
  end_date: "2026-03-13",
  rollover: true,
  closed_at: null,
  status: "active",
  late: false,
  load_unit: "issues",
  load_property_id: null,
  issue_count: 4,
  done_count: 1,
  capacity: {
    human: { capacity: 3, load: 2 },
    agent: { capacity: null, load: 1 },
    unassigned_load: 1,
  },
  created_at: "2026-03-01T00:00:00Z",
  updated_at: "2026-03-01T00:00:00Z",
};

describe("listCycles", () => {
  it("parses a well-formed list", async () => {
    const client = respondWith({ cycles: [validCycle], total: 1 });
    const out = await client.listCycles({ projectId: "proj-1" });
    expect(out.cycles).toHaveLength(1);
    expect(out.cycles[0]?.capacity.human.capacity).toBe(3);
    expect(out.cycles[0]?.capacity.agent.capacity).toBeNull();
  });

  it("falls back to an empty list when the envelope is malformed", async () => {
    const client = respondWith({ cycles: "not-an-array" });
    await expect(client.listCycles()).resolves.toEqual({ cycles: [], total: 0 });
  });

  it("keeps a cycle whose status is a value this client has never seen", async () => {
    // A newer server may add a status. Dropping the row would hide real work;
    // the enum catches to "active" and the UI's switch carries a default.
    const client = respondWith({
      cycles: [{ ...validCycle, status: "frozen" }],
      total: 1,
    });
    const out = await client.listCycles();
    expect(out.cycles[0]?.status).toBe("active");
  });

  it("survives a capacity object that is missing entirely", async () => {
    const { capacity: _dropped, ...withoutCapacity } = validCycle;
    const client = respondWith({ cycles: [withoutCapacity], total: 1 });
    const out = await client.listCycles();
    // The bar renders as "no capacity declared" rather than crashing on
    // `capacity.human` — which is the whole point of the catch.
    expect(out.cycles[0]?.capacity).toEqual({
      human: { capacity: null, load: 0 },
      agent: { capacity: null, load: 0 },
      unassigned_load: 0,
    });
  });
});

describe("getCycleBurndown", () => {
  it("keeps null remaining values as null rather than coercing them to zero", async () => {
    // A null day is "we cannot speak for this day". Zero would read as
    // "everything was done", which is the opposite claim.
    const client = respondWith({
      days: [
        { date: "2026-03-02", remaining_count: 4, remaining_load: 4, ideal_count: 4, ideal_load: 4, human_load: 2, agent_load: 2 },
        { date: "2026-03-03", remaining_count: null, remaining_load: null, ideal_count: 0, ideal_load: 0, human_load: null, agent_load: null },
      ],
      capacity: { human: 3, agent: null },
      load_unit: "issues",
      load_property_id: null,
    });
    const out = await client.getCycleBurndown("cycle-1");
    expect(out.days).toHaveLength(2);
    expect(out.days[1]?.remaining_count).toBeNull();
    expect(out.capacity.agent).toBeNull();
  });

  it("falls back to an empty series on a malformed payload", async () => {
    const client = respondWith({ days: 12 });
    const out = await client.getCycleBurndown("cycle-1");
    expect(out.days).toEqual([]);
    expect(out.load_unit).toBe("issues");
  });
});

describe("getCycleCapacities", () => {
  it("parses the per-actor rows of the JEF-246 contract", async () => {
    const client = respondWith({
      capacities: [
        { actor_type: "member", actor_id: "u-1", name: "Ada", points: 20 },
        { actor_type: "agent", actor_id: "a-1", name: "Mika", points: 40 },
      ],
    });
    const out = await client.getCycleCapacities("cycle-1");
    expect(out.capacities).toHaveLength(2);
    expect(out.capacities[1]?.actor_type).toBe("agent");
    expect(out.capacities[1]?.points).toBe(40);
  });

  it("falls back to no declared capacities on a malformed envelope", async () => {
    const client = respondWith({ capacities: "nope" });
    await expect(client.getCycleCapacities("cycle-1")).resolves.toEqual({ capacities: [] });
  });
});

describe("putCycleCapacities", () => {
  it("sends the full-replace body and parses the response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(
        JSON.stringify({
          capacities: [{ actor_type: "member", actor_id: "u-1", name: "Ada", points: 12 }],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      ),
    );
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    const rows = [
      { actor_type: "member" as const, actor_id: "u-1", points: 12 },
      { actor_type: "agent" as const, actor_id: "a-1", points: 0 },
    ];
    const out = await client.putCycleCapacities("cycle-1", rows);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("https://api.example.test/api/cycles/cycle-1/capacities");
    expect(init.method).toBe("PUT");
    // `name` is server-derived: the write body carries only the write shape.
    expect(JSON.parse(String(init.body))).toEqual({ capacities: rows });
    expect(out.capacities[0]?.name).toBe("Ada");
  });
});

describe("getCycleVelocity", () => {
  const validVelocity = {
    cycle_id: "cycle-1",
    actors: [
      { actor_type: "member", actor_id: "u-1", name: "Ada", capacity_points: 20, done_points: 14, done_count: 3 },
      { actor_type: "agent", actor_id: "a-1", name: "Mika", capacity_points: null, done_points: 8, done_count: 2 },
    ],
    other_done_points: 5,
    history: [
      { cycle_id: "cycle-0", name: "Sprint 12", start_date: "2026-02-16", end_date: "2026-02-27", done_points: 30, done_count: 7 },
    ],
  };

  it("parses actors, other work, and history; keeps a null capacity null", async () => {
    // A null capacity_points is "nothing declared", not zero — coercing it
    // would draw every undeclared actor as over capacity.
    const client = respondWith(validVelocity);
    const out = await client.getCycleVelocity("cycle-1");
    expect(out.actors).toHaveLength(2);
    expect(out.actors[1]?.capacity_points).toBeNull();
    expect(out.other_done_points).toBe(5);
    expect(out.history[0]?.done_points).toBe(30);
  });

  it("falls back to an empty velocity on a malformed payload", async () => {
    const client = respondWith("nope");
    const out = await client.getCycleVelocity("cycle-1");
    expect(out).toEqual({ cycle_id: "cycle-1", actors: [], other_done_points: 0, history: [] });
  });

  it("repairs a bad actors field rather than losing the history", async () => {
    const client = respondWith({ ...validVelocity, actors: { nope: true } });
    const out = await client.getCycleVelocity("cycle-1");
    expect(out.actors).toEqual([]);
    expect(out.history).toHaveLength(1);
  });
});

describe("getGoalProgress", () => {
  it("parses the per-project rows and the aggregate", async () => {
    const client = respondWith({
      goal_id: "goal-1",
      projects: [{ project_id: "p1", name: "Billing", total_count: 4, done_count: 2 }],
      total_count: 4,
      done_count: 2,
    });
    const out = await client.getGoalProgress("goal-1");
    expect(out.projects[0]?.name).toBe("Billing");
    expect(out.done_count).toBe(2);
  });

  it("repairs a bad projects field rather than losing the aggregate", async () => {
    // Field-level `.catch` runs first: the envelope still parses, so the
    // section renders its empty state instead of a broken bar.
    const client = respondWith({ goal_id: "goal-1", projects: { nope: true }, total_count: 4, done_count: 1 });
    const out = await client.getGoalProgress("goal-1");
    expect(out.projects).toEqual([]);
    expect(out.total_count).toBe(4);
  });

  it("falls back to the whole empty aggregate when the payload is not an object", async () => {
    const client = respondWith("nope");
    const out = await client.getGoalProgress("goal-1");
    expect(out).toEqual({ goal_id: "goal-1", projects: [], total_count: 0, done_count: 0 });
  });
});
