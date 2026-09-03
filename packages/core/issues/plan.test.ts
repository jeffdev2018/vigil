// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import {
  latestPlanVerification,
  planSeverityRank,
  planVerificationBlocksDone,
  sortPlanFindings,
} from "./plan";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

const verification = {
  id: "v1", issue_id: "i1", plan_id: "p1", plan_version: 2, task_id: "t1", source_task_id: "t0",
  state: "reported", findings: [], critical_count: 0, major_count: 0, minor_count: 0, outdated_count: 0,
  summary: null, reported_at: "2026-09-03T10:00:00Z", created_at: "2026-09-03T09:00:00Z",
};

describe("plan helpers", () => {
  it("ranks known severities and keeps unknown ones last, never dropped", () => {
    const sorted = sortPlanFindings([
      { severity: "weird", title: "u" },
      { severity: "minor", title: "m" },
      { severity: "critical", title: "c" },
    ]);
    expect(sorted.map((f) => f.severity)).toEqual(["critical", "minor", "weird"]);
    expect(planSeverityRank("CRITICAL")).toBe(0);
    expect(planSeverityRank("nope")).toBe(4);
  });

  it("blocks done only on a reported verification with a critical finding", () => {
    expect(planVerificationBlocksDone(null)).toBe(false);
    expect(planVerificationBlocksDone({ ...verification, critical_count: 1 })).toBe(true);
    expect(planVerificationBlocksDone({ ...verification, critical_count: 1, state: "running" })).toBe(false);
    expect(planVerificationBlocksDone({ ...verification, major_count: 3 })).toBe(false);
  });

  it("picks the newest verification", () => {
    const older = { ...verification, id: "old", created_at: "2026-09-01T00:00:00Z" };
    expect(latestPlanVerification([older, verification])?.id).toBe("v1");
    expect(latestPlanVerification([])).toBeNull();
  });
});

describe("plan endpoints", () => {
  it("parses a plan envelope and keeps an unknown finding severity", async () => {
    stubFetchJson({
      plan: { id: "p1", version: 2, content: "do it", steps: [{ id: "s1", title: "Step" }] },
      versions: [{ id: "p1", version: 2, content: "do it" }],
    });
    const client = new ApiClient("https://api.example.test");
    const plan = await client.getIssuePlan("i1");
    expect(plan.plan?.version).toBe(2);
    expect(plan.versions).toHaveLength(1);

    stubFetchJson({ verifications: [{ ...verification, findings: [{ severity: "unknown", title: "x" }] }] });
    const list = await client.listPlanVerifications("i1");
    expect(list[0]?.findings[0]?.severity).toBe("unknown");
  });

  it("falls back to no plan and no verification on malformed bodies", async () => {
    stubFetchJson({ plan: "nope", versions: 3 });
    const client = new ApiClient("https://api.example.test");
    expect(await client.getIssuePlan("i1")).toEqual({ plan: null, versions: [] });
    stubFetchJson({ verifications: "nope" });
    expect(await client.listPlanVerifications("i1")).toEqual([]);
  });
});
