// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { moveInList, runtimePoolKeys } from "./pools";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("runtime pools client", () => {
  it("reads pools and failover history with tolerant fallbacks, and reorders", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ pools: [{ id: "p1", name: "main", runtime_ids: ["a", "b"], degraded_runtime_id: null, agent_count: "x" }] });
    const [pool] = await client.listRuntimePools();
    expect(pool?.runtime_ids).toEqual(["a", "b"]);
    expect(pool?.agent_count).toBe(0);
    stubFetch("garbage");
    expect(await client.listRuntimePools()).toEqual([]);
    stubFetch({ runs: [{ task_id: "t", status: "failed", degraded: "yes", moves: [{ from_runtime_id: "a", to_runtime_id: "b", reason: "runtime_offline" }] }] });
    const [run] = await client.listIssueFailoverHistory("i");
    expect(run?.degraded).toBe(false);
    expect(run?.moves[0]?.to_runtime_id).toBe("b");
    stubFetch({ id: "a1", runtime_pool_id: "p1" });
    expect((await client.setAgentRuntimePool("a1", "p1")).runtime_pool_id).toBe("p1");
    expect(moveInList(["a", "b", "c"], "b", -1)).toEqual(["b", "a", "c"]);
    expect(moveInList(["a", "b", "c"], "c", 1)).toEqual(["a", "b", "c"]);
    expect(runtimePoolKeys.failovers("w", "i")).toEqual(["issues", "w", "failover-history", "i"]);
  });
});
