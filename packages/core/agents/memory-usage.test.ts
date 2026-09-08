// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { AgentMemoryUsageSchema } from "../api/schemas";
import { agentMemoryUsageOptions } from "./memory";

afterEach(() => vi.unstubAllGlobals());
const version = { memory_id: "11111111-1111-4111-8111-111111111111", revision: 2, prepared_runs: 2, last_started_at: "2026-09-04T12:00:00Z" };
const usage = { since: "2026-08-06T00:00:00Z", until: "2026-09-05T00:00:00Z", started_runs: 6, recorded_runs: 4, unrecorded_runs: 2, load_failed_runs: 1, runs_with_agent_memory: 2, versions: [version] };

it("keeps unknown coverage and exact versions instead of manufacturing zero counts", async () => {
  const fetch = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify(usage))));
  vi.stubGlobal("fetch",fetch);
  const client = new ApiClient("http://memory.test");
  expect(await client.getAgentMemoryUsage("agent")).toEqual(usage);
  expect(fetch).toHaveBeenLastCalledWith("http://memory.test/api/agents/agent/memories/usage",expect.anything());
  expect(agentMemoryUsageOptions("ws","agent").queryKey).toEqual(["agent-memories","ws","agent","usage"]);
  for (const extra of [{started_runs:-1},{recorded_runs:0},{unrecorded_runs:0},{load_failed_runs:4},{versions:[]},{versions:[version,version]},{versions:[{...version,prepared_runs:3}]},{versions:[{...version,revision:0}]},{versions:[{...version,last_started_at:usage.until}]},{until:"yesterday"},{since:"2026-08-07T00:00:00Z"},{started_runs:Number.MAX_SAFE_INTEGER+1}]) {
    expect(AgentMemoryUsageSchema.safeParse({...usage,...extra}).success).toBe(false);
    fetch.mockImplementation(() => Promise.resolve(new Response(JSON.stringify({...usage,...extra}))));
    await expect(client.getAgentMemoryUsage("agent")).rejects.toThrow("Invalid agent memory usage response");
  }
  for (const started_runs of [0,10]) {
    expect(AgentMemoryUsageSchema.parse({...usage,started_runs,recorded_runs:0,unrecorded_runs:started_runs,load_failed_runs:0,runs_with_agent_memory:0,versions:[]}).unrecorded_runs).toBe(started_runs);
  }
});
