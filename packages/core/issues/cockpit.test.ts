// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { cockpitChecksPending, usdFromTicks } from "./cockpit";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

const issue = {
  id: "i1", workspace_id: "w", number: 1, identifier: "T-1", title: "Issue", description: null, status: "in_review",
  priority: "medium", assignee_type: null, assignee_id: null, creator_type: "member", creator_id: "u", parent_issue_id: null,
  project_id: null, position: 0, stage: null, start_date: null, due_date: null, metadata: {}, properties: {},
  created_at: "2026-09-03T00:00:00Z", updated_at: "2026-09-03T00:00:00Z",
};

describe("getReviewCockpit", () => {
  it("keeps the page usable when sections are malformed", async () => {
    stubFetchJson({ issue, run: { id: 5 }, runs: "x", merge_readiness: { prs: "nope" }, usage: { input_tokens: "a" }, open_questions: [{ id: 1 }], criteria: null, plan_verification: 3, failed_sections: "x" });
    const c = await new ApiClient("https://api.example.test").getReviewCockpit("i1");
    expect(c.issue.identifier).toBe("T-1");
    expect(c.run).toBeNull();
    expect(c.runs).toEqual([]);
    expect(c.merge_readiness).toBeNull();
    expect(c.usage).toBeNull();
    expect(c.open_questions).toEqual([]);
    expect(c.criteria).toEqual([]);
    expect(c.plan_verification).toBeNull();
    expect(c.failed_sections).toEqual([]);
  });

  it("rejects a body without an issue and passes the run id through", async () => {
    stubFetchJson({ run: null });
    await expect(new ApiClient("https://api.example.test").getReviewCockpit("i1")).rejects.toThrow();
    stubFetchJson({ issue, runs: [] });
    await new ApiClient("https://api.example.test").getReviewCockpit("i1", "r 1");
    expect((globalThis.fetch as unknown as { mock: { calls: unknown[][] } }).mock.calls[0]?.[0]).toContain("/review-cockpit?run_id=r%201");
  });
});

describe("cockpit helpers", () => {
  it("flags pending checks and converts ticks", () => {
    expect(cockpitChecksPending({ merge_readiness: null })).toBe(false);
    expect(cockpitChecksPending({ merge_readiness: { prs: [], blockers: [{ kind: "checks_pending", label: "", count: 1 }], unresolved_threads: 0, open_todos: 0, ready: false } })).toBe(true);
    expect(usdFromTicks(null)).toBeNull();
    expect(usdFromTicks(3_000_000_000)).toBeCloseTo(0.3);
  });
});
