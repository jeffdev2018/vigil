// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { issueKeys } from "./queries";

function stubFetchJson(body: unknown, status = 200) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue(
      new Response(typeof body === "string" ? body : JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
    ),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

const validIssue = {
  id: "issue-2",
  workspace_id: "ws-1",
  number: 2,
  identifier: "MUL-2",
  title: "Blocked one",
  description: null,
  status: "todo",
  priority: "none",
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "user-1",
  parent_issue_id: null,
  project_id: null,
  position: 0,
  start_date: null,
  due_date: null,
  created_at: "2025-01-01T00:00:00Z",
  updated_at: "2025-01-01T00:00:00Z",
};

describe("listIssueDependencies", () => {
  it("parses a well-formed response and defaults the lists an older server omits", async () => {
    stubFetchJson({ blocks: [{ id: "dep-1", type: "blocks", issue: validIssue }] });
    const deps = await new ApiClient("https://api.example.test").listIssueDependencies("issue-1");
    expect(deps.blocks).toHaveLength(1);
    expect(deps.blocks[0]?.issue.identifier).toBe("MUL-2");
    expect(deps.blocked_by).toEqual([]);
    expect(deps.related).toEqual([]);
  });

  it("degrades a malformed response to empty lists instead of throwing", async () => {
    stubFetchJson({ blocks: "nope", blocked_by: [{ id: 1 }] });
    const deps = await new ApiClient("https://api.example.test").listIssueDependencies("issue-1");
    expect(deps).toEqual({ blocks: [], blocked_by: [], related: [] });
  });

  it("keeps a 404 as an ApiError", async () => {
    stubFetchJson({ error: "issue not found" }, 404);
    await expect(
      new ApiClient("https://api.example.test").listIssueDependencies("missing"),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("issueKeys.dependencies", () => {
  it("nests under the workspace prefix so one invalidation reaches every list", () => {
    expect(issueKeys.dependencies("ws-1", "issue-1")).toEqual([
      ...issueKeys.dependenciesAll("ws-1"),
      "issue-1",
    ]);
    expect(issueKeys.dependenciesAll("ws-1")[1]).toBe("ws-1");
  });
});
