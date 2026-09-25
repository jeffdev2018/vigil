// @vitest-environment node
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { legRoleLabelKey, unknownCostLegs, workflowRootOf } from "./legs";

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
    for (const role of ["draft", "retry", "fallback", "rerun", "review", "critique", "answer", "revision", "watchdog", "duel", "fanout", "shard", "eval", "escalation", "continuation", "subagent"]) {
      expect(legRoleLabelKey(role)).toBe(role);
    }
  });

  // The Go constants are the source of truth: a role stamped by the server
  // but missing here renders as a generic "Leg" badge.
  it("knows every leg role the server declares", () => {
    const dir = fileURLToPath(new URL("../../../server/internal/service/", import.meta.url));
    const roles = readdirSync(dir)
      .filter((f) => f.endsWith(".go") && !f.endsWith("_test.go"))
      .flatMap((f) => [...readFileSync(dir + f, "utf8").matchAll(/\bLegRole\w+\s*=\s*"([^"]+)"/g)].map((m) => m[1]!));
    expect(roles.length).toBeGreaterThan(10);
    expect(roles.filter((role) => legRoleLabelKey(role) !== role)).toEqual([]);
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

describe("unknownCostLegs", () => {
  // "$0.00 au total" beside "Coût indisponible" on the same issue (audit UX,
  // sept. 2026): a zero total is only a figure when no leg is unknown.
  it("trusts the server count, and treats an older backend's zero total as unknown", () => {
    const totals = { legs: 3, cost_usd_ticks: 0, input_tokens: 0, output_tokens: 0, duration_seconds: 0 };
    expect(unknownCostLegs({ ...totals, unknown_cost_legs: 1 })).toBe(1);
    expect(unknownCostLegs({ ...totals, unknown_cost_legs: 0 })).toBe(0);
    expect(unknownCostLegs(totals)).toBe(3);
    expect(unknownCostLegs({ ...totals, cost_usd_ticks: 10 })).toBe(0);
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

  it("drops a malformed unknown-cost count instead of trusting it", async () => {
    stubFetch({ root_task_id: "t1", legs: [{ task_id: "t1", cost_known: "no" }], totals: { legs: 1, unknown_cost_legs: "many" } });
    const r = await client().getTaskLegs("t1");
    expect(r.legs[0]?.cost_known).toBeUndefined();
    expect(r.totals.unknown_cost_legs).toBeUndefined();
  });
});
