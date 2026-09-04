// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { isPlanMaterialized, planStepStages } from "./plan";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("plan gate helpers", () => {
  it("stages steps from their after edges and tolerates bad references", () => {
    const stages = planStepStages([
      { id: "s1", title: "a" },
      { id: "s2", title: "b", after: ["s1"] },
      { id: "s3", title: "c", after: ["s1"] },
      { id: "s4", title: "d", after: ["s2", "s3"] },
      { id: "s5", title: "e", after: ["nope", "s5"] },
    ]);
    expect([...stages.entries()]).toEqual([["s1", 1], ["s2", 2], ["s3", 2], ["s4", 3], ["s5", 1]]);
    expect(isPlanMaterialized({ materialized_at: "2026-09-03T00:00:00Z" })).toBe(true);
    expect(isPlanMaterialized({ materialized_at: null })).toBe(false);
    expect(isPlanMaterialized({})).toBe(false);
  });
});

describe("materializeIssuePlan", () => {
  it("returns the plan and its sub-issues, dropping malformed issues", async () => {
    const plan = { id: "p1", issue_id: "a", version: 1, content: "x", steps: [{ id: "s1", title: "a", issue_id: "c1" }], author_type: "agent", author_id: "ag", superseded_at: null, materialized_at: "2026-09-03T00:00:00Z", created_at: "2026-09-03T00:00:00Z" };
    stubFetchJson({ plan, issues: "nope" });
    const out = await new ApiClient("https://api.example.test").materializeIssuePlan("a", 1);
    expect(out.plan.materialized_at).toBe("2026-09-03T00:00:00Z");
    expect(out.plan.steps[0]?.issue_id).toBe("c1");
    expect(out.issues).toEqual([]);
  });

  it("rejects a body without a plan", async () => {
    stubFetchJson({ issues: [] });
    await expect(new ApiClient("https://api.example.test").materializeIssuePlan("a", 1)).rejects.toThrow();
  });
});
