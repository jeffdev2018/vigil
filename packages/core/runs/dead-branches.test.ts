// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import {
  BRANCH_GC_SETTINGS_KEY,
  branchGcPolicy,
  deadBranchKeys,
  groupDeadBranchesByRuntime,
} from "./dead-branches";
import type { DeadBranchEntry } from "../api/schemas";
import type { Workspace } from "../types";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}
afterEach(() => vi.unstubAllGlobals());

const ws = (settings: unknown): Workspace =>
  ({ id: "ws-1", name: "Acme", slug: "acme", settings }) as Workspace;

describe("branchGcPolicy", () => {
  it("is off with a 30-day TTL when the key is absent or malformed", () => {
    expect(branchGcPolicy(null)).toEqual({ enabled: false, ttl_days: 30 });
    expect(branchGcPolicy(ws(null))).toEqual({ enabled: false, ttl_days: 30 });
    expect(branchGcPolicy(ws({}))).toEqual({ enabled: false, ttl_days: 30 });
    expect(branchGcPolicy(ws({ [BRANCH_GC_SETTINGS_KEY]: "junk" }))).toEqual({
      enabled: false,
      ttl_days: 30,
    });
  });

  it("reads a stored policy and fails closed on a non-boolean enabled", () => {
    expect(branchGcPolicy(ws({ [BRANCH_GC_SETTINGS_KEY]: { enabled: true, ttl_days: 7 } }))).toEqual({
      enabled: true,
      ttl_days: 7,
    });
    expect(branchGcPolicy(ws({ [BRANCH_GC_SETTINGS_KEY]: { enabled: "yes", ttl_days: 7 } })).enabled).toBe(false);
  });

  it("snaps an out-of-range or fractional TTL back to bounds the contract allows", () => {
    expect(branchGcPolicy(ws({ [BRANCH_GC_SETTINGS_KEY]: { enabled: true, ttl_days: 0 } })).ttl_days).toBe(30);
    expect(branchGcPolicy(ws({ [BRANCH_GC_SETTINGS_KEY]: { enabled: true, ttl_days: 400 } })).ttl_days).toBe(30);
    expect(branchGcPolicy(ws({ [BRANCH_GC_SETTINGS_KEY]: { enabled: true, ttl_days: "14" } })).ttl_days).toBe(30);
    expect(branchGcPolicy(ws({ [BRANCH_GC_SETTINGS_KEY]: { enabled: true, ttl_days: 14.9 } })).ttl_days).toBe(14);
  });
});

describe("groupDeadBranchesByRuntime", () => {
  const entry = (over: Partial<DeadBranchEntry>): DeadBranchEntry => ({
    task_id: "t1",
    issue_id: null,
    issue_identifier: null,
    issue_title: null,
    branch_name: "agent/x/t1",
    runtime_id: "r1",
    runtime_name: "macbook",
    finished_at: null,
    actionable: true,
    skip_reason: null,
    ...over,
  });

  it("groups by runtime name in first-seen order", () => {
    const groups = groupDeadBranchesByRuntime([
      entry({ task_id: "t1", runtime_name: "macbook" }),
      entry({ task_id: "t2", runtime_name: "ci-box" }),
      entry({ task_id: "t3", runtime_name: "macbook" }),
    ]);
    expect(groups.map((g) => g.runtimeName)).toEqual(["macbook", "ci-box"]);
    expect(groups[0]?.entries.map((e) => e.task_id)).toEqual(["t1", "t3"]);
    expect(groups[1]?.entries.map((e) => e.task_id)).toEqual(["t2"]);
    expect(groupDeadBranchesByRuntime([])).toEqual([]);
  });
});

describe("dead-branch cleanup client", () => {
  it("fetches the plan and degrades garbage to an empty list", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({
      entries: [
        {
          task_id: "t1",
          issue_id: "i1",
          issue_identifier: "JEF-388",
          issue_title: "Branch GC",
          branch_name: "agent/jef-388/t1",
          runtime_id: "r1",
          runtime_name: "macbook",
          finished_at: "2026-09-01T00:00:00Z",
          actionable: true,
          skip_reason: null,
        },
        {
          task_id: "t2",
          issue_id: null,
          issue_identifier: null,
          issue_title: null,
          branch_name: "agent/x/t2",
          runtime_id: "r2",
          runtime_name: "ci-box",
          finished_at: null,
          actionable: false,
          skip_reason: "runtime_offline",
        },
      ],
    });
    const plan = await client.listDeadBranches();
    expect(plan.entries).toHaveLength(2);
    expect(plan.entries[0]?.issue_identifier).toBe("JEF-388");
    expect(plan.entries[1]?.skip_reason).toBe("runtime_offline");
    expect(deadBranchKeys.plan("ws-1")).toEqual(["runs", "ws-1", "dead-branches"]);

    stubFetch("garbage");
    expect((await client.listDeadBranches()).entries).toEqual([]);
  });

  it("degrades an unknown skip reason to null without dropping the row", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({
      entries: [
        {
          task_id: "t1",
          branch_name: "b",
          runtime_id: "r1",
          runtime_name: "m",
          actionable: false,
          skip_reason: "daemon_asleep",
        },
      ],
    });
    const [row] = (await client.listDeadBranches()).entries;
    expect(row?.task_id).toBe("t1");
    expect(row?.skip_reason).toBeNull();
    expect(row?.actionable).toBe(false);
  });

  it("posts the batch discard and reads enqueued/skipped", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ enqueued: 3, skipped: [{ task_id: "t9", reason: "action_pending" }] });
    const res = await client.discardDeadBranches(["t1", "t2", "t3", "t9"]);
    expect(res.enqueued).toBe(3);
    expect(res.skipped).toEqual([{ task_id: "t9", reason: "action_pending" }]);
    const call = (fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(String(call?.[0])).toContain("/api/runs/dead-branches/discard");
    expect(JSON.parse(String((call?.[1] as RequestInit).body))).toEqual({
      task_ids: ["t1", "t2", "t3", "t9"],
    });

    stubFetch("garbage");
    expect(await client.discardDeadBranches(["t1"])).toEqual({ enqueued: 0, skipped: [] });
  });
});
