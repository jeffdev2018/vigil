// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { agentMemoryEvaluationsOptions } from "./memory";

afterEach(() => vi.unstubAllGlobals());
const memoryId = "11111111-1111-4111-8111-111111111111";
const id = "22222222-2222-4222-8222-222222222222";
const saved = { id, memory_id: memoryId, revision: 1, uploaded_by: "33333333-3333-4333-8333-333333333333", created_at: "2026-09-06T12:00:00Z", adopted_revision: null, eligible: true, reason: "Checks passed", report_hash: "a".repeat(64), total: 2, baseline_passed: 0, candidate_passed: 2, regressions: 0, errors: 0 };

it("validates imported evidence and tenant-scoped queries without manufacturing a passing result", async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify([saved])));
  vi.stubGlobal("fetch", fetch);
  const client = new ApiClient("http://memory.test");
  expect(await client.listAgentMemoryEvaluations("agent", memoryId)).toEqual([saved]);
  expect(agentMemoryEvaluationsOptions("ws", "agent", memoryId).queryKey).toEqual(["agent-memories", "ws", "agent", "evaluations", memoryId]);
  for (const change of [{ eligible: "yes" }, { candidate_passed: 1 }, { regressions: 1 }, { total: 0 }, { memory_id: id }, { created_at: "yesterday" }, { report_hash: "A".repeat(64) }, { report_hash: "not-a-hash" }]) {
    fetch.mockResolvedValue(new Response(JSON.stringify([{ ...saved, ...change }])));
    await expect(client.listAgentMemoryEvaluations("agent", memoryId)).rejects.toThrow("Invalid memory evaluations response");
  }
  // Older backends may omit report_hash; clients must still accept the list.
  fetch.mockResolvedValue(new Response(JSON.stringify([{ ...saved, report_hash: undefined }])));
  expect((await client.listAgentMemoryEvaluations("agent", memoryId))[0]?.report_hash).toBeUndefined();
  fetch.mockResolvedValue(new Response(JSON.stringify(saved)));
  await expect(client.getAgentMemoryEvaluation("agent", memoryId, id)).rejects.toThrow("Invalid memory evaluation response");
  fetch.mockResolvedValue(new Response(JSON.stringify({ ...saved, eligible: false, candidate_passed: 0, report: { candidate: { content: "Check assumptions", revision: 1 }, suite: { image: "sha256:fixture", worker: ["/worker"], verifier: ["/verifier"] }, cases: null } })));
  const partial = await client.getAgentMemoryEvaluation("agent", memoryId, id);
  expect(partial.report?.cases).toEqual([]);
  expect(partial.eligible).toBe(false);
  for (const detail of [
    { ...partial, eligible: true, candidate_passed: 2 },
    { ...partial, report: { ...partial.report, candidate: { content: "Changed", revision: 2 } } },
  ]) {
    fetch.mockResolvedValue(new Response(JSON.stringify(detail)));
    await expect(client.getAgentMemoryEvaluation("agent", memoryId, id)).rejects.toThrow("Invalid memory evaluation response");
  }
  const observations = { provider: "claude", requested_model: "fixture", requested_effort: "low", status: "completed", executable_hash: "a".repeat(64), prompt_hash: "b".repeat(64), brief_hash: "c".repeat(64), tool_calls: 1, usage: null };
  const outcome = { status: "passed", duration_ms: 1, artifact: "result", diagnostic: "", runtime: observations };
  const detail = { ...partial, report: { ...partial.report, cases: [{ id: "a", split: "replay", input_hash: "a", checks_hash: "b", baseline: outcome, candidate: outcome }] } };
  fetch.mockResolvedValue(new Response(JSON.stringify(detail)));
  expect((await client.getAgentMemoryEvaluation("agent", memoryId, id)).report?.cases[0]?.candidate.runtime).toEqual(observations);
  detail.report.cases[0]!.candidate = { ...outcome, runtime: { ...observations, tool_calls: -1 } };
  fetch.mockResolvedValue(new Response(JSON.stringify(detail)));
  await expect(client.getAgentMemoryEvaluation("agent", memoryId, id)).rejects.toThrow("Invalid memory evaluation response");
  fetch.mockResolvedValue(new Response(JSON.stringify(saved)));
  expect(await client.importAgentMemoryEvaluation("agent", memoryId, { version: 1 })).toEqual(saved);
  expect(fetch).toHaveBeenLastCalledWith(`http://memory.test/api/agents/agent/memories/${memoryId}/evaluations`, expect.objectContaining({ method: "POST", body: '{"version":1}' }));
});
