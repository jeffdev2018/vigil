// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { competencyDomainLabel, competencyKeys, competencyRate, estimateCostRange, estimateDurationRange } from "./competency";

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

// What-if estimate (K44).
describe("issue estimate", () => {
  it("formats a cost range in dollars, dropping to four decimals under a cent", () => {
    expect(estimateCostRange(3_000_000_000, 5_000_000_000)).toBe("$0.30\u20130.50");
    expect(estimateCostRange(4_000_000_000, 4_000_000_000)).toBe("$0.40");
    expect(estimateCostRange(10_000_000, 50_000_000)).toBe("$0.0010\u20130.0050");
    // A reversed range must still read low to high.
    expect(estimateCostRange(5_000_000_000, 3_000_000_000)).toBe("$0.30\u20130.50");
    expect(estimateCostRange(null, 5_000_000_000)).toBe("");
    expect(estimateCostRange(3_000_000_000, null)).toBe("");
  });

  it("picks the duration unit from the upper bound", () => {
    expect(estimateDurationRange(30, 45)).toBe("30\u201345s");
    expect(estimateDurationRange(480, 900)).toBe("8\u201315 min");
    expect(estimateDurationRange(600, 600)).toBe("10 min");
    expect(estimateDurationRange(4320, 9000)).toBe("1.2\u20132.5 h");
    expect(estimateDurationRange(null, 900)).toBe("");
    expect(estimateDurationRange(480, null)).toBe("");
  });

  it("keeps a drifted estimate response unusable rather than wrong", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ domain_key: "path:server", min_sample: "nope", candidates: [
      { agent_id: "a", agent_name: "Alpha", sample_size: 6, insufficient_history: false, median_cost_usd_ticks: 3_500_000_000, cost_range_low_usd_ticks: "oops", median_duration_seconds: null, exceeds_budget: "yes" },
    ] });
    const e = await client.getIssueEstimate("i1", ["a"]);
    expect(e.min_sample).toBe(5);
    expect(e.candidates[0]?.median_cost_usd_ticks).toBe(3_500_000_000);
    expect(e.candidates[0]?.cost_range_low_usd_ticks).toBe(null);
    expect(e.candidates[0]?.cost_range_high_usd_ticks).toBe(null);
    expect(e.candidates[0]?.exceeds_budget).toBe(false);
    // A missing measurement must never reach the UI as a number.
    expect(estimateCostRange(e.candidates[0]?.cost_range_low_usd_ticks ?? null, e.candidates[0]?.cost_range_high_usd_ticks ?? null)).toBe("");
    stubFetch("garbage");
    expect((await client.getIssueEstimate("i1", ["a"])).candidates).toEqual([]);
  });
});
