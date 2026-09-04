// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { trafficKeys } from "./traffic";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("traffic control client", () => {
  it("reads conflicts with tolerant fallbacks and ignores one", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ conflicts: [{ id: "c1", task_id: "t", kind: "robot", paths: "x", status: "active" }] });
    const [c] = await client.listTrafficConflicts("i1");
    expect(c?.kind).toBe("agent");
    expect(c?.paths).toEqual([]);
    expect(c?.status).toBe("active");
    stubFetch("garbage");
    expect(await client.listTrafficConflicts("i1")).toEqual([]);
    stubFetch({ id: "c1", status: "ignored" });
    expect((await client.ignoreTrafficConflict("i1", "c1")).status).toBe("ignored");
    expect(trafficKeys.conflicts("w", "i")).toEqual(["traffic-conflicts", "w", "i"]);
  });
});
