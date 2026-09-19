// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import {
  canJudgeRunGroup,
  canStartRunGroup,
  diffStatLabel,
  diffUnifiedLines,
  formatAttemptDuration,
  formatUsdTicks,
  parseDiffStat,
  runGroupErrorKind,
  runGroupKeys,
  sortRunGroups,
} from "./run-group";
import type { RunGroup } from "../api/schemas";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

const group = (over: Partial<RunGroup> = {}): RunGroup => ({
  id: "g1", issue_id: "i1", status: "running", attempt_count: 2, winner_task_id: null,
  created_by: null, created_at: "2026-01-01T00:00:00Z", settled_at: null, attempts: [], judgement: null, ...over,
});

describe("run group client", () => {
  it("parses races with fallbacks", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ groups: [{ id: "g", status: "weird", attempt_count: "no", attempts: [{ task_id: "t", diff_truncated: "yes" }] }] });
    const [g] = await client.listRunGroups("i1");
    expect(g?.status).toBe("running");
    expect(g?.attempt_count).toBe(0);
    expect(g?.attempts[0]?.diff_truncated).toBe(false);
    expect(g?.attempts[0]?.diff_unified).toBeNull();
    // JEF-234 fields degrade to their zero values, never a parse failure.
    expect(g?.attempts[0]?.runtime_id).toBe("");
    expect(g?.attempts[0]?.runtime_name).toBe("");
    expect(g?.attempts[0]?.cost_usd_ticks).toBe(0);
    expect(g?.attempts[0]?.duration_seconds).toBe(0);
    stubFetch("garbage");
    expect(await client.listRunGroups("i1")).toEqual([]);
    stubFetch({ group: { id: "g2", status: "settled", winner_task_id: "t2" } });
    expect((await client.settleRunGroup("g2", "t2"))?.winner_task_id).toBe("t2");
    stubFetch({ group: { id: "g2", status: "abandoned" } });
    expect((await client.abandonRunGroup("g2"))?.status).toBe("abandoned");
    expect(runGroupKeys.issue("w", "i")).toEqual(["run-groups", "w", "i"]);
  });

  it("parses the judge verdict and degrades a malformed one instead of failing", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({
      group: {
        id: "g3",
        status: "running",
        judgement: {
          status: "answered",
          winner_task_id: "t1",
          justification: "t1 is cleaner.",
          scores: [{ task_id: "t1", score: 8, rationale: "clean" }, { task_id: "t2", score: 5, rationale: "messy" }],
          model: "judge-model",
          judged_at: "2026-01-02T00:00:00Z",
          cost_usd_ticks: 4_200_000_000,
        },
      },
    });
    const judged = await client.judgeRunGroup("g3");
    expect(judged?.judgement?.status).toBe("answered");
    expect(judged?.judgement?.winner_task_id).toBe("t1");
    expect(judged?.judgement?.scores.map((s) => s.score)).toEqual([8, 5]);
    // Garbage verdicts degrade to null/zero values, never a parse failure.
    stubFetch({ group: { id: "g4", judgement: { status: "mystery", scores: "lots", cost_usd_ticks: "soon" } } });
    const degraded = await client.judgeRunGroup("g4");
    expect(degraded?.judgement?.status).toBe("failed");
    expect(degraded?.judgement?.scores).toEqual([]);
    expect(degraded?.judgement?.cost_usd_ticks).toBeNull();
    stubFetch({ group: { id: "g5", judgement: "not-an-object" } });
    expect((await client.judgeRunGroup("g5"))?.judgement).toBeNull();
  });
});

describe("sortRunGroups", () => {
  it("puts the newest race first and falls back to the id when a date is unusable", () => {
    const old = group({ id: "a", created_at: "2026-01-01T00:00:00Z" });
    const recent = group({ id: "b", created_at: "2026-03-01T00:00:00Z" });
    expect(sortRunGroups([old, recent]).map((g) => g.id)).toEqual(["b", "a"]);
    const broken = [group({ id: "aaa", created_at: "" }), group({ id: "bbb", created_at: "nope" })];
    expect(sortRunGroups(broken).map((g) => g.id)).toEqual(["bbb", "aaa"]);
    // Same instant: the UUIDv7 order decides rather than the input order.
    const tied = [group({ id: "a1" }), group({ id: "a2" })];
    expect(sortRunGroups(tied).map((g) => g.id)).toEqual(["a2", "a1"]);
    // The input array is left alone.
    const input = [old, recent];
    sortRunGroups(input);
    expect(input.map((g) => g.id)).toEqual(["a", "b"]);
  });
});

describe("canStartRunGroup", () => {
  it("refuses a second race while one is running", () => {
    expect(canStartRunGroup([])).toBe(true);
    expect(canStartRunGroup([group({ status: "settled" }), group({ id: "g2", status: "abandoned" })])).toBe(true);
    expect(canStartRunGroup([group({ status: "settled" }), group({ id: "g2", status: "running" })])).toBe(false);
  });
});

describe("canJudgeRunGroup", () => {
  const finished = (task_id: string) => ({
    task_id, agent_id: "a", status: "completed", model: "", runtime_id: "", runtime_name: "",
    cost_usd_ticks: 0, duration_seconds: 0, diff_stat: null, diff_unified: null, diff_truncated: false,
    created_at: "", completed_at: null,
  });
  it("needs two completed attempts on a race that is not settled", () => {
    expect(canJudgeRunGroup(group({ attempts: [finished("t1"), finished("t2")] }))).toBe(true);
    // Still running alongside: the two finished ones are enough.
    expect(canJudgeRunGroup(group({ attempts: [finished("t1"), finished("t2"), { ...finished("t3"), status: "running" }] }))).toBe(true);
    expect(canJudgeRunGroup(group({ attempts: [finished("t1"), { ...finished("t2"), status: "running" }] }))).toBe(false);
    expect(canJudgeRunGroup(group({ attempts: [] }))).toBe(false);
    // Settled is over; abandoned races can still be judged for the record.
    expect(canJudgeRunGroup(group({ status: "settled", attempts: [finished("t1"), finished("t2")] }))).toBe(false);
    expect(canJudgeRunGroup(group({ status: "abandoned", attempts: [finished("t1"), finished("t2")] }))).toBe(true);
  });
});

describe("parseDiffStat / diffStatLabel", () => {
  it("reads the plausible field spellings and rejects everything else", () => {
    expect(parseDiffStat({ files: 3, insertions: 120, deletions: 18 })).toEqual({ files: 3, insertions: 120, deletions: 18 });
    expect(parseDiffStat({ files_changed: 2, additions: 5 })).toEqual({ files: 2, insertions: 5, deletions: 0 });
    expect(parseDiffStat({ files: 1.7 })).toEqual({ files: 1, insertions: 0, deletions: 0 });
    expect(parseDiffStat(null)).toBeNull();
    expect(parseDiffStat([1, 2])).toBeNull();
    expect(parseDiffStat("2 files")).toBeNull();
    expect(parseDiffStat({})).toBeNull();
    expect(parseDiffStat({ files: -1, insertions: Number.NaN })).toBeNull();
    expect(parseDiffStat({ files: "3" })).toBeNull();
    expect(diffStatLabel({ files: 3, insertions: 120, deletions: 18 })).toBe("3 · +120 −18");
    expect(diffStatLabel(null)).toBeNull();
  });
});

describe("diffUnifiedLines", () => {
  it("classifies file headers apart from added and removed lines", () => {
    const lines = diffUnifiedLines(["diff --git a/x b/x", "--- a/x", "+++ b/x", "@@ -1 +1 @@", "-gone", "+kept", " same"].join("\n"));
    expect(lines.map((l) => l.kind)).toEqual(["meta", "meta", "meta", "hunk", "removed", "added", "context"]);
    expect(lines[5]?.text).toBe("+kept");
    expect(diffUnifiedLines("")).toEqual([]);
  });
});

describe("formatUsdTicks", () => {
  it("returns null for unreported cost and formats the rest like the runtimes views", () => {
    expect(formatUsdTicks(0)).toBeNull();
    expect(formatUsdTicks(-5)).toBeNull();
    expect(formatUsdTicks(Number.NaN)).toBeNull();
    expect(formatUsdTicks(1_500_000_000_000)).toBe("$150");
    expect(formatUsdTicks(4_200_000_000)).toBe("$0.42");
    expect(formatUsdTicks(42_000_000)).toBe("$0.0042");
  });
});

describe("formatAttemptDuration", () => {
  it("returns null while running and renders m:ss / h:mm afterwards", () => {
    expect(formatAttemptDuration(0)).toBeNull();
    expect(formatAttemptDuration(-3)).toBeNull();
    expect(formatAttemptDuration(Number.NaN)).toBeNull();
    expect(formatAttemptDuration(9)).toBe("0:09");
    expect(formatAttemptDuration(754)).toBe("12:34");
    expect(formatAttemptDuration(3600)).toBe("1:00");
    expect(formatAttemptDuration(4525)).toBe("1:15");
  });
});

describe("runGroupErrorKind", () => {
  it("names the three 409 codes and lumps everything else together", () => {
    const conflict = (code: string) => new ApiError("nope", 409, "Conflict", { code });
    expect(runGroupErrorKind(conflict("run_group_already_active"))).toBe("already_active");
    expect(runGroupErrorKind(conflict("run_group_already_settled"))).toBe("already_settled");
    expect(runGroupErrorKind(conflict("run_group_not_judgeable"))).toBe("not_judgeable");
    expect(runGroupErrorKind(new ApiError("bad winner", 400, "Bad Request", { error: "winner_task_id is not an attempt of this race" }))).toBe("generic");
    expect(runGroupErrorKind(new Error("offline"))).toBe("generic");
    expect(runGroupErrorKind(undefined)).toBe("generic");
  });
});
