// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { routingKeys } from "./routing";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("issue routing client", () => {
  it("reads decisions and settings with tolerant fallbacks", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ decision: { risk_level: "weird", escalated: "yes", matched_paths: "x", target_pool_name: "cheap" }, task_id: "t1" });
    const r = await client.getIssueRouting("i1");
    expect(r.decision?.risk_level).toBe("normal");
    expect(r.decision?.escalated).toBe(false);
    expect(r.decision?.matched_paths).toEqual([]);
    expect(r.decision?.target_pool_name).toBe("cheap");
    stubFetch("garbage");
    expect((await client.getIssueRouting("i1")).decision).toBeNull();
    stubFetch({ enabled: true, pools: { high: "p3" }, escalation_failures: "3" });
    const s = await client.getRoutingSettings();
    expect(s.enabled).toBe(true);
    expect(s.pools.high).toBe("p3");
    expect(s.escalation_failures).toBe(2);
    expect(routingKeys.issue("w", "i")).toEqual(["routing-decision", "w", "i"]);
  });
});
