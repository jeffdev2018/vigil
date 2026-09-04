// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { competencyDomainLabel, competencyKeys, competencyRate } from "./competency";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("competency client", () => {
  it("parses agent competency and assignee suggestions with fallbacks, and formats", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ agent_id: "a", min_sample: "x", rows: [{ agent_id: "a", domain_key: "path:server", score: "bad", reliable: "yes" }] });
    const c = await client.getAgentCompetency("a");
    expect(c.min_sample).toBe(5);
    expect(c.rows[0]?.score).toBe(0);
    expect(c.rows[0]?.reliable).toBe(false);
    stubFetch("garbage");
    expect((await client.getAgentCompetency("a")).rows).toEqual([]);
    stubFetch({ domain_key: "label:backend", min_sample: 3, candidates: [{ agent_id: "a", agent_name: "Alpha", score: 0.82, sample_size: 14, reliable: true }], ownership: { rule_id: "r", owner_user_id: "u", matched: "label:x", pattern: "label:x" } });
    const s = await client.getAssigneeSuggestion("i1");
    expect(s.candidates[0]?.agent_name).toBe("Alpha");
    expect(s.ownership?.owner_user_id).toBe("u");
    expect(competencyRate(0.824)).toBe("82%");
    expect(competencyRate(2)).toBe("100%");
    expect(competencyDomainLabel("label:backend")).toBe("backend");
    expect(competencyDomainLabel("path:server")).toBe("server/");
    expect(competencyDomainLabel("general")).toBe("general");
    expect(competencyKeys.issue("w", "i")).toEqual(["competency", "w", "issue", "i"]);
  });
});
