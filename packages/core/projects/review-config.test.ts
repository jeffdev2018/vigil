// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { projectReviewConfigKeys, projectReviewConfigOptions } from "./review-config";

function stubFetch(body: unknown) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("project review config client", () => {
  it("gets and puts the config, tolerating malformed answers", async () => {
    stubFetch({ project_id: "p1", checklist: ["tests added"], reviewer_agent_id: "a1", gate_enabled: true, max_cycles: 5 });
    const got = await new ApiClient("https://api.example.test").getProjectReviewConfig("p1");
    expect(got).toMatchObject({ checklist: ["tests added"], reviewer_agent_id: "a1", gate_enabled: true, max_cycles: 5 });
    stubFetch("nope");
    const fallback = await new ApiClient("https://api.example.test").getProjectReviewConfig("p1");
    expect(fallback).toEqual({ project_id: "p1", checklist: [], reviewer_agent_id: null, gate_enabled: false, max_cycles: 3 });
    stubFetch({ project_id: "p1", checklist: [], reviewer_agent_id: null, gate_enabled: false, max_cycles: 3 });
    const put = await new ApiClient("https://api.example.test").putProjectReviewConfig("p1", { checklist: [], reviewer_agent_id: null, gate_enabled: false, max_cycles: 3 });
    expect(put.max_cycles).toBe(3);
    expect(projectReviewConfigKeys.config("w", "p1")).toEqual(["project-review-config", "w", "p1"]);
    expect(projectReviewConfigOptions("w", "").enabled).toBe(false);
  });
});
