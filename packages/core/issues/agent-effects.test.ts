// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { agentEffectKeys, effectField, effectState, groupEffectsByRun, type AgentEffect } from "./agent-effects";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const effect = (over: Partial<AgentEffect> = {}): AgentEffect => ({
  id: "e1", task_id: "t1", agent_id: "a1", agent_name: "Scout", issue_id: "i1", kind: "issue_status", target_type: "issue", target_id: "i1",
  before: { field: "status", value: "todo" }, after: { field: "status", value: "in_progress" }, reversible: true, reversed_at: null,
  status: "applied", decision_id: null, payload: {},
  reversed_by_type: null, reverse_error: null, within_window: true, expires_at: "2026-09-06T00:00:00Z", created_at: "2026-09-05T00:00:00Z", ...over,
});

describe("agent effects client (K69)", () => {
  it("parses with fallbacks and derives states and runs", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ effects: [{ id: "e1", kind: "issue_field", before: { field: "priority", value: "low" }, reversible: "yes", within_window: true }], window_hours: "24" });
    const out = await client.listIssueAgentEffects("i1");
    expect(out.window_hours).toBe(24);
    expect(out.effects[0]?.reversible).toBe(false);
    expect(effectField(out.effects[0]!)).toBe("priority");
    stubFetch("garbage");
    expect(await client.listIssueAgentEffects("i1")).toEqual({ effects: [], window_hours: 24 });
    stubFetch({ reversed: 2, skipped: [{ id: "x", kind: "note_create", reason: "window_expired" }], breaker: { tripped: true, trust_mode: "approval" } });
    const report = await client.undoTask("t1");
    expect(report.reversed).toBe(2);
    expect(report.skipped[0]?.reason).toBe("window_expired");
    expect(report.breaker.trust_mode).toBe("approval");
    stubFetch(null);
    expect((await client.undoAgentEffect("e1")).breaker.tripped).toBe(false);
    stubFetch({ window_hours: 48, breaker_threshold: "3" });
    expect(await client.getUndoSettings()).toEqual({ window_hours: 48, breaker_threshold: 5 });

    expect(effectState(effect())).toBe("pending");
    expect(effectState(effect({ reversed_at: "2026-09-05T01:00:00Z" }))).toBe("reversed");
    expect(effectState(effect({ reversible: false }))).toBe("not_reversible");
    expect(effectState(effect({ reverse_error: "boom" }))).toBe("failed");
    expect(effectState(effect({ within_window: false }))).toBe("expired");
    expect(effectState(effect({ status: "pending" }))).toBe("held");
    expect(effectState(effect({ status: "approved" }))).toBe("approved");
    expect(effectState(effect({ status: "rejected", reverse_error: "run failed" }))).toBe("rejected");
    expect(out.effects[0]?.status).toBe("applied");
    const runs = groupEffectsByRun([effect({ id: "e3", task_id: "t2" }), effect({ id: "e2", reversed_at: "x" }), effect({ id: "e1" })]);
    expect(runs.map((r) => r.task_id)).toEqual(["t2", "t1"]);
    expect(runs[1]?.effects.length).toBe(2);
    expect(runs[1]?.pending).toBe(1);
    expect(agentEffectKeys.issue("w", "i")).toEqual(["agent-effects", "w", "i"]);
  });
});
