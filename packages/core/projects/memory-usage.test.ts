// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";
import { ProjectMemoryUsageSchema } from "../api/schemas";
import { projectMemoryUsageOptions } from "./memory";

afterEach(() => vi.unstubAllGlobals());
const version = {
  project_id: "11111111-1111-4111-8111-111111111111",
  revision: 2,
  prepared_runs: 2,
  last_started_at: "2026-09-04T12:00:00Z",
};
const usage = {
  since: "2026-08-06T00:00:00Z",
  until: "2026-09-05T00:00:00Z",
  started_runs: 6,
  recorded_runs: 4,
  unrecorded_runs: 2,
  runs_with_project_memory: 2,
  versions: [version],
};

it("keeps unknown project-memory coverage instead of manufacturing zero counts", async () => {
  const fetch = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify(usage))));
  vi.stubGlobal("fetch", fetch);
  const client = new ApiClient("http://memory.test");
  expect(await client.getProjectMemoryUsage("project")).toEqual(usage);
  expect(fetch).toHaveBeenLastCalledWith(
    "http://memory.test/api/projects/project/memory/usage",
    expect.anything(),
  );
  expect(projectMemoryUsageOptions("ws", "project").queryKey).toEqual([
    "projects",
    "ws",
    "detail",
    "project",
    "memory-usage",
  ]);
  for (const extra of [
    { started_runs: -1 },
    { recorded_runs: 0 },
    { unrecorded_runs: 0 },
    { versions: [] },
    { versions: [version, version] },
    { versions: [{ ...version, prepared_runs: 3 }] },
    { versions: [{ ...version, revision: 0 }] },
    { versions: [{ ...version, last_started_at: usage.until }] },
    { until: "yesterday" },
    { since: "2026-08-07T00:00:00Z" },
    { started_runs: Number.MAX_SAFE_INTEGER + 1 },
  ]) {
    expect(ProjectMemoryUsageSchema.safeParse({ ...usage, ...extra }).success).toBe(false);
    fetch.mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ ...usage, ...extra }))));
    await expect(client.getProjectMemoryUsage("project")).rejects.toThrow(
      "Invalid project memory usage response",
    );
  }
  for (const started_runs of [0, 10]) {
    expect(
      ProjectMemoryUsageSchema.parse({
        ...usage,
        started_runs,
        recorded_runs: 0,
        unrecorded_runs: started_runs,
        runs_with_project_memory: 0,
        versions: [],
      }).unrecorded_runs,
    ).toBe(started_runs);
  }
});
