// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import {
  canDiscardRun,
  canPromoteRun,
  isRunDiffNotFound,
  runActionKeys,
  runBranchActionErrorKind,
} from "./run-actions";
import type { AgentTask } from "../types/agent";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const task = (over: Partial<AgentTask> = {}): AgentTask => ({
  id: "t1",
  agent_id: "a1",
  runtime_id: "r1",
  issue_id: "i1",
  status: "completed",
  priority: 0,
  dispatched_at: null,
  started_at: null,
  completed_at: "2026-09-01T00:00:00Z",
  result: null,
  error: null,
  created_at: "2026-09-01T00:00:00Z",
  branch_name: "agent/jef-255/t1",
  ...over,
});

describe("canPromoteRun / canDiscardRun", () => {
  it("needs a terminal worktree run with no end state and nothing in flight", () => {
    expect(canPromoteRun(task())).toBe(true);
    expect(canDiscardRun(task())).toBe(true);
    // Still in flight, or no branch recorded: nothing to act on.
    expect(canPromoteRun(task({ status: "running" }))).toBe(false);
    expect(canDiscardRun(task({ status: "queued" }))).toBe(false);
    expect(canPromoteRun(task({ branch_name: undefined }))).toBe(false);
    expect(canPromoteRun(task({ branch_name: "" }))).toBe(false);
    // End states close both actions.
    expect(canPromoteRun(task({ promoted_at: "2026-09-01T01:00:00Z" }))).toBe(false);
    expect(canDiscardRun(task({ promoted_at: "2026-09-01T01:00:00Z" }))).toBe(false);
    expect(canPromoteRun(task({ discarded_at: "2026-09-01T01:00:00Z" }))).toBe(false);
    expect(canDiscardRun(task({ discarded_at: "2026-09-01T01:00:00Z" }))).toBe(false);
    // A pending action locks the row.
    expect(canPromoteRun(task({ pending_branch_action: "promote" }))).toBe(false);
    expect(canDiscardRun(task({ pending_branch_action: "discard" }))).toBe(false);
  });
});

describe("runBranchActionErrorKind", () => {
  it("names the three 409 codes and lumps everything else together", () => {
    const conflict = (code: string) => new ApiError("nope", 409, "Conflict", { code });
    expect(runBranchActionErrorKind(conflict("run_not_promotable"))).toBe("not_promotable");
    expect(runBranchActionErrorKind(conflict("run_not_discardable"))).toBe("not_discardable");
    expect(runBranchActionErrorKind(conflict("run_branch_action_pending"))).toBe("action_pending");
    expect(runBranchActionErrorKind(new ApiError("bad", 400, "Bad Request", { error: "no" }))).toBe("generic");
    expect(runBranchActionErrorKind(new Error("offline"))).toBe("generic");
    expect(runBranchActionErrorKind(undefined)).toBe("generic");
  });
});

describe("isRunDiffNotFound", () => {
  it("matches only the diff endpoint's 404 code", () => {
    expect(isRunDiffNotFound(new ApiError("nope", 404, "Not Found", { code: "run_diff_not_found" }))).toBe(true);
    expect(isRunDiffNotFound(new ApiError("nope", 404, "Not Found", { code: "task_not_found" }))).toBe(false);
    expect(isRunDiffNotFound(new Error("offline"))).toBe(false);
  });
});

describe("run diff / branch action client", () => {
  it("parses the diff and degrades garbage instead of failing", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ diff_stat: { files: 2, insertions: 10, deletions: 3 }, diff_unified: "@@ -1 +1 @@\n-old\n+new", diff_truncated: false });
    const diff = await client.getRunDiff("t1");
    expect(diff.diff_unified).toContain("+new");
    expect(diff.diff_truncated).toBe(false);
    expect(runActionKeys.diff("t1")).toEqual(["run-diff", "t1"]);
    stubFetch("garbage");
    const degraded = await client.getRunDiff("t1");
    expect(degraded).toEqual({ diff_stat: null, diff_unified: null, diff_truncated: false });
  });

  it("posts promote and discard and reads the acknowledgement", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ request_id: "req-1", status: "pending" });
    expect((await client.promoteRun("i1", "t1")).request_id).toBe("req-1");
    // A fresh stub per call: a Response body reads only once.
    expect(String((fetch as ReturnType<typeof vi.fn>).mock.calls[0]?.[0])).toContain("/api/issues/i1/runs/t1/promote");
    stubFetch({ request_id: "req-2", status: "pending" });
    expect((await client.discardRun("i1", "t1")).status).toBe("pending");
    expect(String((fetch as ReturnType<typeof vi.fn>).mock.calls[0]?.[0])).toContain("/api/issues/i1/runs/t1/discard");
  });

  it("parses the new task fields and degrades a server that predates them", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch([{ id: "t1", status: "completed", branch_name: "b", promoted_at: "2026-09-01T01:00:00Z", promote_pr_url: "https://x/pr/1", pending_branch_action: "promote" }]);
    const [full] = await client.listTasksByIssue("i1");
    expect(full?.promoted_at).toBe("2026-09-01T01:00:00Z");
    expect(full?.discarded_at).toBeNull();
    expect(full?.promote_pr_url).toBe("https://x/pr/1");
    expect(full?.pending_branch_action).toBe("promote");
    // An older backend omits the fields entirely: no action taken, none in flight.
    stubFetch([{ id: "t2", status: "completed" }]);
    const [legacy] = await client.listTasksByIssue("i1");
    expect(legacy?.promoted_at).toBeNull();
    expect(legacy?.discarded_at).toBeNull();
    expect(legacy?.promote_pr_url).toBe("");
    expect(legacy?.pending_branch_action).toBe("");
    // A malformed row degrades the fields, not the whole execution log.
    stubFetch([{ id: "t3", status: "completed", promoted_at: 7, pending_branch_action: "explode" }]);
    const [broken] = await client.listTasksByIssue("i1");
    expect(broken?.promoted_at).toBeNull();
    expect(broken?.pending_branch_action).toBe("");
  });
});
