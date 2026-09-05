// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

// Governed MCP gateway (K77): the binding list stays strict (no entry leaks)
// while carrying the catalogue and policy; the catalogue parses tolerantly.
describe("mcp gateway client", () => {
  it("keeps the binding list strict and carries policy and catalogue", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch([{ id: "s1", name: "crm", transport: "http", enabled: true, url: "https://leak", tool_count: 2, tool_policy: { default: "never", tools: { get_issue: "act_alone", weird: "sometimes" } }, tools: [{ name: "get_issue", risk: "read", risk_source: "manual", class: "act_alone" }, { name: "x", risk: "bogus", class: "nope" }] }]);
    const list = await client.setAgentMcpServerPolicy("a1", "s1", { default: "never", tools: { get_issue: "act_alone" } });
    expect((list[0] as unknown as { url?: string }).url).toBeUndefined();
    expect(list[0]?.tool_count).toBe(2);
    expect(list[0]?.tool_policy?.tools?.weird).toBe("ask");
    expect(list[0]?.tools?.[1]?.risk).toBe("unknown");
    expect(list[0]?.tools?.[1]?.class).toBe("ask");
    stubFetch([{ id: "s1", name: "crm" }]);
    expect((await client.listAgentMcpServers("a1"))[0]?.tool_count).toBe(0);
  });

  it("parses the catalogue tolerantly", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ tools: [{ name: "send_email", risk: "external_effect", risk_source: "auto" }], discovered_at: "2026-09-05T00:00:00Z", risks: ["read", "bogus"] });
    const cat = await client.discoverWorkspaceMcpServerTools("w1", "s1");
    expect(cat.tools[0]?.risk).toBe("external_effect");
    expect(cat.risks).toEqual(["read", "unknown"]);
    stubFetch({ nope: 1 });
    expect((await client.listWorkspaceMcpServerTools("w1", "s1")).discovered_at).toBeNull();
    stubFetch({ tools: "garbage" });
    expect((await client.setWorkspaceMcpServerTools("w1", "s1", [{ name: "a" }])).tools).toEqual([]);
  });
});
