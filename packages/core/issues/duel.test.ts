// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { duelCostUsd, duelDuration, duelKeys } from "./duel";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("agent duel client", () => {
  it("parses duels with fallbacks and formats cost and duration", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ duel: { id: "d", status: "weird", a: { agent_id: "x", cost_usd_ticks: "no" }, winner: "c", arbiter_winner: "b" } });
    const d = await client.getIssueDuel("i1");
    expect(d?.status).toBe("running");
    expect(d?.a.cost_usd_ticks).toBe(0);
    expect(d?.b.agent_id).toBe("");
    expect(d?.winner).toBeNull();
    expect(d?.arbiter_winner).toBe("b");
    stubFetch("garbage");
    expect(await client.getIssueDuel("i1")).toBeNull();
    stubFetch({ duel: { id: "d2", status: "running" } }, 201);
    expect((await client.startDuel("i1", { agent_a_id: "a", agent_b_id: "b" }))?.id).toBe("d2");
    stubFetch({ duel: { id: "d2", status: "confirmed", winner: "tie" } });
    expect((await client.confirmDuel("d2", "tie"))?.winner).toBe("tie");
    expect(duelCostUsd(1_200_000_000)).toBe("$0.12");
    expect(duelCostUsd(12_000_000)).toBe("$0.0012");
    expect(duelCostUsd(0)).toBe("$0.00");
    expect(duelDuration(90)).toBe("1m 30s");
    expect(duelDuration(7.4)).toBe("7s");
    expect(duelKeys.issue("w", "i")).toEqual(["duel", "w", "i"]);
  });
});
