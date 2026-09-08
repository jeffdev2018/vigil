// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { parseWithFallback } from "../api/schema";
import { ProjectMemorySchema, ProjectMemoryHistorySchema, EMPTY_PROJECT_MEMORY } from "../api/schemas";

describe("project memory boundary", () => {
  it("keeps malformed or missing data unpublishable", () => {
    for (const raw of [{}, { rules: null, revision: 2 }, { rules: [17], revision: 2 }, { rules: [], revision: -1 }, { rules: [], revision: "2" }]) {
      expect(parseWithFallback(raw, ProjectMemorySchema, EMPTY_PROJECT_MEMORY, { endpoint: "GET /api/projects/{id}/memory" }).revision).toBe(-1);
    }
    expect(ProjectMemorySchema.parse({ rules: ["Use pnpm"], revision: 2 })).toEqual({ rules: ["Use pnpm"], revision: 2, reviewed_by: null, reviewed_at: null, expires_at: null, expired: false });
  expect(
    ProjectMemorySchema.parse({
      rules: ["Keep context"],
      revision: 3,
      source_review: {
        review_id: "review-1",
        issue_id: "issue-1",
        task_id: "task-1",
        feedback: "Form lost the project.",
        criteria: ["Keep context"],
        assessments: [{ passed: false, evidence: "Empty field" }],
        reviewed_by: "Jeff",
        reviewed_at: "2026-09-07T00:00:00Z",
      },
    }).source_review?.review_id,
  ).toBe("review-1");
  expect(ProjectMemorySchema.parse({ rules: [], revision: 1, source_review: "bad" }).source_review).toBeUndefined();
  });
});

afterEach(() => vi.unstubAllGlobals());
it("rejects malformed expiry and history while preserving the restore contract", async () => {
  for (const fields of [{ expires_at: "bad" }, { expired: "false" }, { restored_from_revision: -1 }]) {
    expect(ProjectMemorySchema.safeParse({ rules: [], revision: 1, ...fields }).success).toBe(false);
  }
  expect(ProjectMemoryHistorySchema.safeParse({ versions: [], next_before_revision: 0 }).success).toBe(false);
  const { ApiClient } = await import("../api/client");
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ rules: ["Restored"], revision: 4, expires_at: "2000-01-01T00:00:00Z", expired: true }), { status: 200 }));
  vi.stubGlobal("fetch", fetch);
  const client = new ApiClient("http://memory.test");
  const restored = await client.restoreProjectMemory("project", 1, 3);
  expect(restored.expired).toBe(true);
  expect(fetch).toHaveBeenCalledWith("http://memory.test/api/projects/project/memory", expect.objectContaining({ body: JSON.stringify({ restore_revision: 1, expected_revision: 3 }) }));
  fetch.mockResolvedValue(new Response(JSON.stringify({ versions: null }), { status: 200 }));
  await expect(client.getProjectMemoryHistory("project", 3)).rejects.toThrow("Invalid project memory history");
});
