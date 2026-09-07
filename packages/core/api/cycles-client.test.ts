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
