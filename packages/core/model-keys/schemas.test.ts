// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { usageForKey } from "./schemas";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

// BYOK model keys (K48): tolerant parsing, the hint is all a key exposes.
describe("model keys client", () => {
  it("parses the list tolerantly and sums usage per key", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ keys: [{ id: "k1", scope: "galaxy", key_hint: "sk-***1a2b", active: "yes" }], usage: [{ model_key_id: "k1", model: "m1", task_count: 2, input_tokens: 10, output_tokens: 5, cost_usd_ticks: 7 }, { model_key_id: "k1", model: "m2", task_count: 1, input_tokens: 1 }], vendors: [{ id: "anthropic" }], configured: true });
    const list = await client.listModelKeys("w1");
    expect(list.keys[0]?.scope).toBe("workspace");
    expect(list.keys[0]?.active).toBe(false);
    expect(list.keys[0]?.key_hint).toBe("sk-***1a2b");
    expect(usageForKey(list.usage, "k1")).toEqual({ tasks: 3, tokens: 16, costUsdTicks: 7 });
    stubFetch({ nope: 1 });
    expect((await client.listModelKeys("w1")).configured).toBe(false);
    stubFetch({ id: "k2", key_hint: "sk-***zz99", active: true }, 201);
    expect((await client.createModelKey("w1", { scope: "workspace", provider: "openai", key: "sk-x" })).key_hint).toBe("sk-***zz99");
    stubFetch({ retired: true });
    expect(await client.retireModelKey("w1", "k2")).toEqual({ retired: true });
  });
});
