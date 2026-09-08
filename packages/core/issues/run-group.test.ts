// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import {
  canStartRunGroup,
  diffStatLabel,
  diffUnifiedLines,
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
  created_by: null, created_at: "2026-01-01T00:00:00Z", settled_at: null, attempts: [], ...over,
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
    stubFetch("garbage");
    expect(await client.listRunGroups("i1")).toEqual([]);
    stubFetch({ group: { id: "g2", status: "settled", winner_task_id: "t2" } });
    expect((await client.settleRunGroup("g2", "t2"))?.winner_task_id).toBe("t2");
    stubFetch({ group: { id: "g2", status: "abandoned" } });
    expect((await client.abandonRunGroup("g2"))?.status).toBe("abandoned");
    expect(runGroupKeys.issue("w", "i")).toEqual(["run-groups", "w", "i"]);
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

describe("runGroupErrorKind", () => {
  it("names the two 409 codes and lumps everything else together", () => {
    const conflict = (code: string) => new ApiError("nope", 409, "Conflict", { code });
    expect(runGroupErrorKind(conflict("run_group_already_active"))).toBe("already_active");
    expect(runGroupErrorKind(conflict("run_group_already_settled"))).toBe("already_settled");
    expect(runGroupErrorKind(new ApiError("bad winner", 400, "Bad Request", { error: "winner_task_id is not an attempt of this race" }))).toBe("generic");
    expect(runGroupErrorKind(new Error("offline"))).toBe("generic");
    expect(runGroupErrorKind(undefined)).toBe("generic");
  });
});
