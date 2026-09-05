// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { fanoutKeys, fanoutProgress, type FanoutBatch } from "./fanout";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("fan-out client", () => {
  it("parses batches with fallbacks and computes progress", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ batch: { id: "b", status: "weird", expected_count: 3, completed_count: "x", members: [{ id: "m", outcome: "maybe" }] } });
    const b = await client.getIssueFanout("i1");
    expect(b?.status).toBe("pending");
    expect(b?.completed_count).toBe(0);
    expect(b?.members[0]?.outcome).toBeNull();
    stubFetch("garbage");
    expect(await client.getIssueFanout("i1")).toBeNull();
    stubFetch({ batch: { id: "b2", status: "pending", expected_count: 2, members: [] } }, 201);
    expect((await client.startFanout("i1", { leader_agent_id: "l", sub_tasks: [] }))?.id).toBe("b2");
    const base = { id: "b", parent_issue_id: "i", leader_agent_id: "l", status: "pending", expected_count: 4, completed_count: 2, failed_count: 1, synthesis_task_id: null, members: [], created_at: "", completed_at: null } as FanoutBatch;
    expect(fanoutProgress(base)).toBe(0.75);
    expect(fanoutProgress({ ...base, expected_count: 0 })).toBe(0);
    expect(fanoutKeys.issue("w", "i")).toEqual(["fanout", "w", "i"]);
  });
});
