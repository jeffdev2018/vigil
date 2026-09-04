// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { handoffKeys, splitLines } from "./handoff";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("handoff packet client", () => {
  it("reads packets with tolerant fallbacks and splits lines", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ packets: [{ id: "p1", objective: "Ship", decisions: "x", created_by_type: "robot", next_action: null }] });
    const [p] = await client.listHandoffPackets("i1");
    expect(p?.objective).toBe("Ship");
    expect(p?.decisions).toEqual([]);
    expect(p?.created_by_type).toBe("system");
    expect(p?.next_action).toBe("");
    stubFetch("garbage");
    expect(await client.listHandoffPackets("i1")).toEqual([]);
    stubFetch({ packet: null });
    expect(await client.getLatestHandoffPacket("i1")).toBeNull();
    stubFetch({ id: "p2", objective: "Fix", run_id: "t" });
    expect((await client.createHandoffPacket("i1", { run_id: "t", objective: "Fix", decisions: [], evidence: [], failed_attempts: [], next_action: "" })).id).toBe("p2");
    expect(splitLines(" a \n\n b\n")).toEqual(["a", "b"]);
    expect(handoffKeys.packets("w", "i")).toEqual(["handoff-packet", "w", "i"]);
  });
});
