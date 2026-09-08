// @vitest-environment node
import { expect, it } from "vitest";
import { AgentTaskSchema } from "./schemas";

it("preserves exact version references and degrades malformed receipts to unknown", () => {
  const version = { id: "11111111-1111-4111-8111-111111111111", revision: 3 };
  const context = { dispatched_at: "2026-09-05T10:00:00.123456Z", agent_status: "loaded", agent_versions: [version], project_version: version };
  expect(AgentTaskSchema.parse({ id: "run", memory_context: context }).memory_context).toEqual(context);
  for (const value of [null, { ...context, agent_status: "future" }, { ...context, dispatched_at: "yesterday" }, { ...context, agent_versions: [version, version] }, { ...context, agent_status: "unavailable" }, { ...context, project_version: {...version, revision: 0} }, { ...context, agent_versions: [{...version, id: "bad"}] }, {...context, agent_versions: [{...version, revision: 1.5}]}]) {
    const run = AgentTaskSchema.parse({ id: "run", memory_context: value });
    expect(run.id).toBe("run");
    expect(run.memory_context).toBeUndefined();
  }
  expect(AgentTaskSchema.parse({ id: "old-run" }).memory_context).toBeUndefined();
  for (const agent_status of ["loaded", "unavailable"]) {
    expect(AgentTaskSchema.parse({ id: "run", memory_context: { ...context, agent_status, agent_versions: [], project_version: null } }).memory_context?.agent_status).toBe(agent_status);
  }
});
