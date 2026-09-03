// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { effectiveMergeReady, unknownMergeBlockers } from "./merge-readiness";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
    ),
  );
}

afterEach(() => vi.unstubAllGlobals());

describe("effectiveMergeReady", () => {
  it("is never true unless the server said ready with no blockers", () => {
    expect(effectiveMergeReady({ ready: true, blockers: [] })).toBe(true);
    expect(effectiveMergeReady({ ready: false, blockers: [] })).toBe(false);
    expect(effectiveMergeReady({ ready: true, blockers: [{ kind: "no_pr", label: "No PR" }] })).toBe(false);
  });

  it("treats an unknown blocker kind as a blocker", () => {
    const blockers = [{ kind: "policy_hold", label: "Held by policy" }];
    expect(effectiveMergeReady({ ready: true, blockers })).toBe(false);
    expect(unknownMergeBlockers(blockers)).toEqual(blockers);
    expect(unknownMergeBlockers([{ kind: "checks_failing", label: "x" }])).toEqual([]);
  });
});

describe("merge readiness endpoints", () => {
  it("parses a well-formed readiness and defaults what the server omits", async () => {
    stubFetchJson({
      prs: [{ id: "pr-1", source: "github", number: 4, title: "t", html_url: "u", state: "open",
        mergeable: "mergeable", merge_state: "clean",
        checks: { total: 2, passed: 2, failed: 0, pending: 0 }, stale_snapshot: false, ready: true }],
      blockers: [],
      ready: true,
    });
    const out = await new ApiClient("https://api.example.test").getIssueMergeReadiness("issue-1");
    expect(out.ready).toBe(true);
    expect(out.prs[0]?.checks.passed).toBe(2);
    expect(out.unresolved_threads).toBe(0);
    expect(out.open_todos).toBe(0);
  });

  it("falls back to a not-ready empty shape on a malformed readiness", async () => {
    stubFetchJson({ prs: "nope", ready: "yes" });
    const out = await new ApiClient("https://api.example.test").getIssueMergeReadiness("issue-1");
    expect(out).toEqual({ prs: [], blockers: [], unresolved_threads: 0, open_todos: 0, ready: false });
  });

  it("never defaults ready to true when the field is missing", async () => {
    stubFetchJson({ prs: [], blockers: [] });
    const out = await new ApiClient("https://api.example.test").getIssueMergeReadiness("issue-1");
    expect(out.ready).toBe(false);
  });

  it("falls back to an empty stack on a malformed pr-stack", async () => {
    stubFetchJson({ nodes: [{ issue_id: 3 }] });
    const out = await new ApiClient("https://api.example.test").getIssuePRStack("issue-1");
    expect(out).toEqual({ nodes: [], truncated: false, cyclic: false });
  });
});
