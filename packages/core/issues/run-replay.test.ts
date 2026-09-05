// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import { ApiClient } from "../api/client";
import { fetchWholeReplay, replayCountsUpTo, replayResumable, sealState, type ReplayEvent } from "./run-replay";

function stubFetch(bodies: unknown[]) {
  const fn = vi.fn();
  for (const b of bodies) fn.mockResolvedValueOnce(new Response(JSON.stringify(b), { status: 200, headers: { "Content-Type": "application/json" } }));
  vi.stubGlobal("fetch", fn);
  return fn;
}
afterEach(() => vi.unstubAllGlobals());

const ev = (seq: number, kind: string, over: Partial<ReplayEvent> = {}): ReplayEvent => ({
  seq, at: "2026-09-05T00:00:00Z", kind, actor: { type: "agent", id: "a", name: "Builder" }, title: kind, text: "", data: {}, data_class: "internal", in_plan: null, source: "task_message", source_id: String(seq), prev_hash: "", hash: "h" + seq, ...over,
});

describe("run replay", () => {
  it("parses a replay tolerantly and resumes with the parsed result", async () => {
    stubFetch([{ run: { id: "t1", agent_name: "Builder", links: "nope" }, events: [ev(0, "text"), { seq: 1, kind: 7 }], total: 2, next_cursor: null, sealed: { events: 2, head_hash: "h1", sealed_at: "x", verified: true } }]);
    const r = await new ApiClient("https://api.example.test").getTaskReplay("t1");
    expect(r.run.links).toEqual([]);
    expect(r.events[1]?.kind).toBe("unknown");
    expect(sealState(r)).toBe("verified");
    expect(sealState({ sealed: null })).toBe("unsealed");
    expect(sealState({ sealed: { events: 1, head_hash: "", sealed_at: "", verified: false } })).toBe("broken");
    expect(r.run.snapshot).toBeNull();
    expect(r.events[0]?.data_class).toBe("internal");
    stubFetch(["garbage"]);
    expect((await new ApiClient("https://api.example.test").resumeTaskReplay("t1", 3, "go")).from_seq).toBe(3);
    stubFetch([{ task_id: "t9", safe_mode: "yes" }]);
    expect(await new ApiClient("https://api.example.test").simulateTaskReplay("t1")).toEqual({ task_id: "t9", safe_mode: true });
  });

  it("follows the cursor to load the whole run", async () => {
    const fn = stubFetch([
      { run: { id: "t1" }, events: [ev(0, "text"), ev(1, "tool_use")], total: 3, next_cursor: 2 },
      { run: { id: "t1" }, events: [ev(2, "effect")], total: 3, next_cursor: null },
    ]);
    vi.stubGlobal("fetch", fn);
    setApiInstance(new ApiClient("https://api.example.test"));
    const r = await fetchWholeReplay("t1");
    expect(r.events.map((e) => e.kind)).toEqual(["text", "tool_use", "effect"]);
    expect(r.next_cursor).toBeNull();
  });

  it("counts what happened up to a position and knows when a run can be resumed", () => {
    const events = [ev(0, "text"), ev(1, "tool_use"), ev(2, "effect"), ev(3, "steer"), ev(4, "decision_asked"), ev(5, "handoff"), ev(6, "error")];
    expect(replayCountsUpTo(events, 2)).toEqual({ tool_calls: 1, effects: 1, decisions: 0, steers: 0, errors: 0, handoffs: 0, drift: 0, redacted: 0 });
    expect(replayCountsUpTo(events, 6)).toEqual({ tool_calls: 1, effects: 1, decisions: 1, steers: 1, errors: 1, handoffs: 1, drift: 0, redacted: 0 });
    const flagged = [ev(0, "text", { data_class: "confidential" }), ev(1, "tool_use", { in_plan: false }), ev(2, "tool_use", { in_plan: true })];
    expect(replayCountsUpTo(flagged, 2)).toMatchObject({ tool_calls: 2, drift: 1, redacted: 1 });
    expect(replayCountsUpTo(events, -1).tool_calls).toBe(0);
    expect(replayResumable("running")).toBe(false);
    expect(replayResumable("completed")).toBe(true);
  });
});
