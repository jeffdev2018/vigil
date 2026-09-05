// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { legRoleLabelKey, workflowRootOf } from "./legs";

// Per-leg accounting (JEF-274). Canonical layer for the pure helpers and for
// the endpoint's tolerance: the views suite mounts the summary, it does not
// re-run this matrix.

function stubFetch(body: unknown) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } }),
    ),
  );
}
afterEach(() => vi.unstubAllGlobals());

const client = () => new ApiClient("https://api.example.test");

describe("legRoleLabelKey", () => {
  it("keeps every role the server can send", () => {
    for (const role of ["draft", "retry", "fallback", "rerun", "review", "critique", "answer", "revision", "watchdog", "duel", "fanout", "shard", "eval", "escalation"]) {
      expect(legRoleLabelKey(role)).toBe(role);
    }
  });

  // A newer backend can add a producer this client has never heard of. The
  // badge must stay a badge, not print a raw server token at the user.
  it("folds an unknown or empty role into `other`", () => {
    expect(legRoleLabelKey("time_travel")).toBe("other");
    expect(legRoleLabelKey("")).toBe("other");
  });
});

describe("workflowRootOf", () => {
  it("is empty for a run that belongs to no workflow", () => {
    expect(workflowRootOf({ id: "t1" })).toBe("");
    expect(workflowRootOf({ id: "t1", leg_role: "" })).toBe("");
  });

  it("resolves the root from a secondary leg, and from the root itself", () => {
    expect(workflowRootOf({ id: "t2", leg_role: "review", workflow_root_task_id: "t1" })).toBe("t1");
    // A stamped root carries a role but no root pointer: it IS the root.
    expect(workflowRootOf({ id: "t3", leg_role: "duel" })).toBe("t3");
  });
});

describe("getTaskLegs", () => {
  it("parses a workflow and totals it", async () => {
    stubFetch({
      root_task_id: "t1",
      legs: [
        { task_id: "t1", leg_role: "draft", status: "completed", agent_id: "a1", agent_name: "Builder", runtime_id: "r1", runtime_name: "Local", provider: "openai", model: "gpt", input_tokens: 100, output_tokens: 10, cost_usd_ticks: 1_000_000_000, duration_seconds: 60, created_at: "2026-09-05T00:00:00Z", completed_at: "2026-09-05T00:01:00Z" },
        { task_id: "t2", leg_role: "review", status: "completed", agent_id: "a2", agent_name: "Critic", runtime_id: "r2", runtime_name: "Cloud", provider: "anthropic", model: "opus", input_tokens: 50, output_tokens: 5, cost_usd_ticks: 2_000_000_000, duration_seconds: 30, created_at: null, completed_at: null },
      ],
      totals: { legs: 2, cost_usd_ticks: 3_000_000_000, input_tokens: 150, output_tokens: 15, duration_seconds: 90 },
    });
    const r = await client().getTaskLegs("t2");
    expect(r.root_task_id).toBe("t1");
    expect(r.legs.map((l) => l.leg_role)).toEqual(["draft", "review"]);
    expect(r.totals.cost_usd_ticks).toBe(3_000_000_000);
    expect(r.legs[1]?.completed_at).toBeNull();
  });

  // A malformed response must not take the issue panel down with it: the
  // fallback is an empty workflow, which the UI reads as "no summary".
  it("falls back to an empty workflow on garbage", async () => {
    stubFetch("not an object");
    const r = await client().getTaskLegs("t9");
    expect(r.legs).toEqual([]);
    expect(r.totals).toEqual({ legs: 0, cost_usd_ticks: 0, input_tokens: 0, output_tokens: 0, duration_seconds: 0 });
    expect(r.root_task_id).toBe("t9");
  });

  // One unreadable leg costs that leg its figures, never the whole workflow.
  it("defaults the fields of a partial leg instead of dropping the response", async () => {
    stubFetch({ root_task_id: "t1", legs: [{ task_id: "t1" }, { task_id: "t2", cost_usd_ticks: "free" }], totals: "nope" });
    const r = await client().getTaskLegs("t1");
    expect(r.legs).toHaveLength(2);
    expect(r.legs[0]?.leg_role).toBe("");
    expect(r.legs[1]?.cost_usd_ticks).toBe(0);
    expect(r.totals.legs).toBe(0);
  });
});
