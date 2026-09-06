// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import {
  benchmarkDeltaTone,
  evalScoreTone,
  hasRunningRun,
  latestRunForSuite,
  sortedCorpusClasses,
  type BenchmarkCorpus,
  type EvalRun,
} from "./schemas";

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

// Internal benchmark harness (JEF-276). Same boundary contract as the eval
// endpoints: a drifted payload must degrade, never throw.
describe("benchmark client", () => {
  it("parses a partial benchmark run into safe defaults", async () => {
    const client = new ApiClient("https://api.example.test");

    stubFetch({ runs: [{ id: "b1", suite_id: "s1", status: "running" }] }, 202);
    const [partial] = await client.runBenchmark("s1", {
      agent_id: "a1", agent_version_id: "v1", candidates: [{ runtime_id: "rt1", model: "sonnet" }],
    });
    // Everything the benchmark adds is absent here; none of it may be undefined.
    expect(partial?.benchmark).toBe(false);
    expect(partial?.runtime_id).toBe("");
    expect(partial?.runtime_name).toBe("");
    expect(partial?.model).toBe("");
    expect(partial?.baseline_run_id).toBeNull();
    expect(partial?.per_class).toEqual({});
    expect(partial?.delta_score).toBeNull();
    // The inherited eval-run fields keep their own defaults.
    expect(partial?.cases).toEqual([]);
    expect(partial?.score).toBeNull();

    stubFetch({ nope: true });
    expect(await client.listBenchmarks("w1")).toEqual([]);
  });

  it("keeps an unknown task class and a null cost in the per-class breakdown", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({
      runs: [{
        id: "b2", suite_id: "s1", suite_name: "Regression", status: "completed", score: 75,
        benchmark: true, runtime_id: "rt1", runtime_name: "Codex (host)", model: "gpt-5",
        baseline_run_id: "b1", delta_score: -12, cases: [],
        per_class: {
          bugfix: { cases: 3, passed: 2, score: 66, cost_usd_ticks: 4200, duration_seconds: 90 },
          // A class the client has never heard of — the router may grow one.
          telemetry: { cases: 1, passed: 1, score: null, cost_usd_ticks: null, duration_seconds: null },
        },
      }],
    });
    const [run] = await client.listBenchmarks("w1");
    expect(run?.per_class.bugfix?.cost_usd_ticks).toBe(4200);
    expect(run?.per_class.telemetry?.cases).toBe(1);
    expect(run?.per_class.telemetry?.score).toBeNull();
    expect(run?.per_class.telemetry?.duration_seconds).toBeNull();
    expect(run?.delta_score).toBe(-12);
    expect(run?.baseline_run_id).toBe("b1");
  });

  it("parses a suite corpus and falls back to null on garbage", async () => {
    const client = new ApiClient("https://api.example.test");

    stubFetch({
      suite_id: "s1", suite_name: "Regression", cases: 4, balanced: false,
      classes: { bugfix: { count: 3, share: 0.75 }, docs: { count: 1, share: 0.25 } },
    });
    const corpus = await client.getEvalSuiteCorpus("s1");
    expect(corpus?.cases).toBe(4);
    expect(corpus?.balanced).toBe(false);
    expect(corpus?.classes.bugfix?.share).toBe(0.75);

    // A payload missing everything still parses: every field has a default,
    // and an unrunnable suite is reported balanced rather than skewed.
    stubFetch({});
    expect((await client.getEvalSuiteCorpus("s2"))?.balanced).toBe(true);

    stubFetch("not an object");
    expect(await client.getEvalSuiteCorpus("s3")).toBeNull();
  });

  it("parses a policy search and defaults a missing outcome", async () => {
    const client = new ApiClient("https://api.example.test");

    stubFetch({
      improved: true,
      baseline: { policy: { cost_weight: 0, duration_weight: 0, min_samples: 3 }, baseline: true, scored_classes: 2, cases: 4, passed: 3, passed_rate: 0.75, avg_cost_usd: 0.42, picks: [] },
      winner: {
        policy: { cost_weight: 0.3, duration_weight: 0.1, min_samples: 2 }, baseline: false,
        scored_classes: 2, cases: 4, passed: 4, passed_rate: 1, avg_cost_usd: 0.21,
        picks: [{ task_class: "bugfix", run_id: "b2", runtime_id: "rt1", model: "gpt-5", score: 0.9, cases: 3, passed: 3, avg_cost_usd: null }],
      },
      grid: [],
    });
    const search = await client.benchmarkPolicySearch("w1", { runs: ["b1", "b2"] });
    expect(search?.improved).toBe(true);
    expect(search?.winner.picks[0]?.task_class).toBe("bugfix");
    expect(search?.winner.picks[0]?.avg_cost_usd).toBeNull();
    expect(search?.baseline.passed_rate).toBe(0.75);
    // A missing winner must not sink the whole answer.
    stubFetch({ improved: false, grid: [] });
    const bare = await client.benchmarkPolicySearch("w1", { runs: ["b1"] });
    expect(bare?.winner.picks).toEqual([]);
    expect(bare?.winner.cases).toBe(0);

    stubFetch([1, 2, 3]);
    expect(await client.benchmarkPolicySearch("w1", { runs: ["b1"] })).toBeNull();
  });
});

describe("benchmarkDeltaTone", () => {
  it("reads a gain as success and no movement as a warning, unlike a raw score", () => {
    expect(benchmarkDeltaTone(12)).toBe("success");
    expect(benchmarkDeltaTone(1)).toBe("success");
    // 0 is "no movement", not a failure — this is where it parts from evalScoreTone.
    expect(benchmarkDeltaTone(0)).toBe("warning");
    expect(benchmarkDeltaTone(-1)).toBe("destructive");
    expect(benchmarkDeltaTone(-40)).toBe("destructive");
    expect(benchmarkDeltaTone(null)).toBe("warning");
    expect(benchmarkDeltaTone(undefined)).toBe("warning");
    expect(benchmarkDeltaTone(Number.NaN)).toBe("warning");
  });
});

describe("sortedCorpusClasses", () => {
  const corpus = (classes: BenchmarkCorpus["classes"]): BenchmarkCorpus => ({
    suite_id: "s1", suite_name: "S", cases: 0, classes, balanced: true,
  });

  it("orders by count desc then class asc so the order never flickers", () => {
    const entries = sortedCorpusClasses(corpus({
      docs: { count: 1, share: 0.2 },
      bugfix: { count: 3, share: 0.6 },
      chore: { count: 1, share: 0.2 },
    }));
    expect(entries.map(([name]) => name)).toEqual(["bugfix", "chore", "docs"]);
    expect(entries[0]?.[1].share).toBe(0.6);
  });

  it("answers an empty list for a missing corpus", () => {
    expect(sortedCorpusClasses(null)).toEqual([]);
    expect(sortedCorpusClasses(undefined)).toEqual([]);
    expect(sortedCorpusClasses(corpus({}))).toEqual([]);
  });
});
