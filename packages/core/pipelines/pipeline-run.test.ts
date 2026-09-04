// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { pipelineKeys, stageStates, type PipelineRun } from "./index";

function stubFetch(body: unknown, status = 200) {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } })));
}
afterEach(() => vi.unstubAllGlobals());

describe("pipelines client", () => {
  it("parses pipelines and runs with fallbacks and computes stage states", async () => {
    const client = new ApiClient("https://api.example.test");
    stubFetch({ pipelines: [{ id: "p", name: "flow", stages: [{ id: "s1", name: "plan", executor_type: "robot", executor_id: "a" }], open_runs: "x" }] });
    const [p] = await client.listPipelines();
    expect(p?.stages[0]?.executor_type).toBe("agent");
    expect(p?.open_runs).toBe(0);
    stubFetch("garbage");
    expect(await client.listPipelines()).toEqual([]);
    stubFetch({ run: { id: "r", status: "weird", current_index: "x", stages: [] } });
    const run = await client.getIssuePipelineRun("i");
    expect(run?.status).toBe("active");
    expect(run?.current_index).toBe(-1);
    stubFetch({ run: null });
    expect(await client.getIssuePipelineRun("i")).toBeNull();
    const base: PipelineRun = { id: "r", pipeline_id: "p", pipeline_name: "flow", issue_id: "i", status: "active", current_stage_id: "s2", current_index: 1, gate_decision_id: null, last_error: null, stages: [{ id: "s1", position: 0, name: "a", executor_type: "agent", executor_id: "x", requires_human_gate: false }, { id: "s2", position: 1, name: "b", executor_type: "agent", executor_id: "y", requires_human_gate: true }, { id: "s3", position: 2, name: "c", executor_type: "squad", executor_id: "z", requires_human_gate: false }], started_at: "", completed_at: null };
    expect(stageStates(base)).toEqual(["done", "current", "todo"]);
    expect(stageStates({ ...base, status: "paused", gate_decision_id: "d" })).toEqual(["done", "gate", "todo"]);
    expect(stageStates({ ...base, status: "completed", current_index: 3 })).toEqual(["done", "done", "done"]);
    expect(pipelineKeys.run("w", "i")).toEqual(["pipelineRun", "w", "i"]);
  });
});
