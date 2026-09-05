// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { evalScoreTone, hasRunningRun, latestRunForSuite, type EvalRun } from "./schemas";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

// Eval Lab (K24): the boundary must survive a drifted payload — a missing
// envelope, a status the client has never heard of, a null score.
describe("eval client", () => {
  it("parses cases, suites and runs tolerantly", async () => {
    const client = new ApiClient("https://api.example.test");

    stubFetch({ cases: [{ id: "c1", source_issue_number: "12", title: "Login flow", criteria: [{ id: "cr1", text: "logs in", proof_state: "satisfied" }] }, { title: "no id" }] });
    const cases = await client.listEvalCases("w1");
    // The row without an `id` fails the schema, so the whole array falls back.
    expect(cases).toEqual([]);

    stubFetch({ cases: [{ id: "c1", source_issue_number: 12, title: "Login flow", description: null, criteria: [{ id: "cr1", text: "logs in", proof_state: "satisfied" }] }] });
    const good = await client.listEvalCases("w1");
    expect(good[0]?.source_issue_number).toBe(12);
    expect(good[0]?.description).toBe("");
    expect(good[0]?.criteria[0]?.proof_type).toBe("");

    stubFetch({ suites: [{ id: "s1", name: "Regression", case_ids: ["c1"], case_count: 1 }] });
    expect((await client.listEvalSuites("w1"))[0]?.case_count).toBe(1);

    stubFetch({ nope: true });
    expect(await client.listEvalSuites("w1")).toEqual([]);

    stubFetch({ runs: [{ id: "r1", suite_id: "s1", suite_name: "Regression", status: "running", score: null, agent_version_number: 3, cases: [{ case_id: "c1", status: "pending", score: null }] }] });
    const runs = await client.listEvalRuns("w1");
    expect(runs[0]?.status).toBe("running");
    expect(runs[0]?.score).toBeNull();
    expect(runs[0]?.cases[0]?.detail).toBe("");

    // A status the client does not know still parses; the UI defaults it.
    stubFetch({ run: { id: "r2", suite_id: "s1", status: "quantum", score: 91, cases: [] } });
    const run = await client.getEvalRun("r2");
    expect(run?.status).toBe("quantum");
    expect(run?.score).toBe(91);

    stubFetch({ garbage: 1 });
    expect(await client.getEvalRun("r3")).toBeNull();

    stubFetch({ case: { id: "c9", title: "Promoted" } }, 201);
    expect((await client.promoteIssueToEvalCase("i1"))?.title).toBe("Promoted");

    stubFetch({ suite: { id: "s2", name: "Nightly", case_ids: ["c1", "c9"], case_count: 2 } }, 201);
    expect((await client.createEvalSuite("w1", { name: "Nightly", case_ids: ["c1", "c9"] }))?.case_count).toBe(2);

    stubFetch({ run: { id: "r4", suite_id: "s2", status: "running", cases: [] } }, 202);
    expect((await client.runEvalSuite("s2", { agent_id: "a1", agent_version_id: "v1" }))?.id).toBe("r4");
  });
});

describe("evalScoreTone", () => {
  it("bands a score into a semantic tone and treats a missing score as a failure", () => {
    expect(evalScoreTone(100)).toBe("success");
    expect(evalScoreTone(80)).toBe("success");
    expect(evalScoreTone(79)).toBe("warning");
    expect(evalScoreTone(50)).toBe("warning");
    expect(evalScoreTone(49)).toBe("destructive");
    expect(evalScoreTone(0)).toBe("destructive");
    expect(evalScoreTone(null)).toBe("destructive");
    expect(evalScoreTone(undefined)).toBe("destructive");
    expect(evalScoreTone(Number.NaN)).toBe("destructive");
  });
});

describe("suite run lookups", () => {
  const run = (over: Partial<EvalRun>): EvalRun => ({
    id: "r", workspace_id: "w1", suite_id: "s1", suite_name: "S", agent_id: "a", agent_version_id: "v",
    agent_version_number: 1, status: "completed", score: 90, started_by: null, started_at: "", completed_at: null, cases: [],
    ...over,
  });

  it("takes the newest run of a suite and reports one in flight", () => {
    // The list endpoint answers newest first, so `find` is the newest run.
    const runs = [run({ id: "new", score: 40 }), run({ id: "old", score: 90 }), run({ id: "other", suite_id: "s2" })];
    expect(latestRunForSuite(runs, "s1")?.id).toBe("new");
    expect(latestRunForSuite(runs, "s3")).toBeUndefined();
    expect(hasRunningRun(runs, "s1")).toBe(false);
    expect(hasRunningRun([run({ status: "running" })], "s1")).toBe(true);
    expect(hasRunningRun([run({ status: "running", suite_id: "s2" })], "s1")).toBe(false);
  });
});
