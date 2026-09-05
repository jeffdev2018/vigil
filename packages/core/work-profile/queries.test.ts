// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { busiestHours, formatReviewLoad, ruleSummary, type WorkProfileObservation } from "./queries";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const obs = (over: Partial<WorkProfileObservation> = {}): WorkProfileObservation => ({
  id: "o1", key: "decision:question:no,yes", kind: "decision_rule", value: { option_id: "yes", option_label: "Yes", count: 9, total: 10, family: "question" },
  source: "decisions", count: 10, corrections: 1, auto: false, state: "learned", stake: "normal", first_observed_at: "", last_observed_at: "", ...over,
});

describe("work profile", () => {
  it("parses the profile tolerantly and the learned hint on a decision", async () => {
    stubFetch({ observations: [{ id: "o1", state: "weird", value: "nope" }], examples: "3", review_load_seconds: 90 });
    const p = await new ApiClient("https://api.example.test").getWorkProfile();
    expect(p.observations[0]?.state).toBe("learned");
    expect(p.observations[0]?.value).toEqual({});
    expect(p.examples).toBe(0);
    expect(p.review_load_seconds).toBe(90);
    stubFetch({ decisions: [{ id: "d1", learned: { option_id: "yes", count: 5, total: 5, rate: 1, auto: "no" } }] });
    const ds = await new ApiClient("https://api.example.test").listIssueDecisions("i1");
    expect(ds[0]?.learned?.count).toBe(5);
    expect(ds[0]?.learned?.auto).toBe(false);
  });

  it("summarizes a rule, ranks decision hours and formats the review load", () => {
    expect(ruleSummary(obs())).toEqual({ option_label: "Yes", count: 9, total: 10, rate: 0.9, family: "question" });
    expect(ruleSummary(obs({ value: {} })).rate).toBe(0);
    expect(busiestHours(obs({ key: "decision_hour", kind: "decision_hour", value: { "09": 4, "14": 7, "18": 1, x: 3 } }))).toEqual([14, 9, 18]);
    expect(formatReviewLoad(45)).toBe("45s");
    expect(formatReviewLoad(270)).toBe("5 min");
    expect(formatReviewLoad(4000)).toBe("1 h 7 min");
  });
});
