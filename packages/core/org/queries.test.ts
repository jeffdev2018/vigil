// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { orgIsLive, orgMermaid, orgModelLabel } from "./queries";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("org client and helpers", () => {
  it("parses structures, templates and health tolerantly", async () => {
    stubFetch({ structures: [{ id: "s1", model: "weird", status: "active", definition: { units: [{ id: "u", name: "U", autonomy: "nope" }] } }] });
    const list = await new ApiClient("https://api.example.test").listOrgStructures();
    expect(list[0]?.model).toBe("owner_network");
    expect(list[0]?.definition.units[0]?.autonomy).toBe("draft");
    expect(list[0]?.definition.units[0]?.members).toEqual([]);
    expect(list[0]?.paused_units).toEqual([]);
    stubFetch("garbage");
    expect(await new ApiClient("https://api.example.test").listOrgStructures()).toEqual([]);
    stubFetch({ templates: [{ model: "market", definition: {} }] });
    expect((await new ApiClient("https://api.example.test").listOrgTemplates())[0]?.definition.market.min_offers).toBe(2);
    stubFetch({ structure_id: "s1", proposals: [{ key: "drift", title: "Drift" }], units: "nope" });
    const health = await new ApiClient("https://api.example.test").getOrgHealth("s1");
    expect(health.proposals[0]?.key).toBe("drift");
    expect(health.units).toEqual([]);
    stubFetch({ offers: [{ id: "o1", status: "sideways" }] });
    expect((await new ApiClient("https://api.example.test").listIssueOrgOffers("i1"))[0]?.status).toBe("pending");
  });

  it("renders a mermaid graph and labels models", () => {
    const src = orgMermaid({ units: [{ id: "a b", name: 'Le "chef"', autonomy: "draft", excludes: [], allow: [], deny: [], escalation_quota_per_day: 5, members: [{ type: "member", id: "m" }], roles: [] }, { id: "c", name: "C", autonomy: "draft", excludes: [], allow: [], deny: [], escalation_quota_per_day: 5, members: [], roles: [] }], edges: [{ from: "c", to: "a b", kind: "reports_to" }], rules: [], committees: [], market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 5, min_offers: 2 } }, ["c"]);
    expect(src).toContain("graph TD");
    expect(src).toContain(`u_a_b["Le 'chef' (1)"]`);
    expect(src).toContain("u_c -->|reports to| u_a_b");
    expect(src).toContain("⏸");
    expect(orgModelLabel("market")).toBe("Internal market");
    expect(orgIsLive({ status: "paused" })).toBe(false);
  });
});
