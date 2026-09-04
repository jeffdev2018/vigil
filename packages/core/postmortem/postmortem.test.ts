// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { postmortemKeys } from "./queries";

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

const validPostmortem = {
  id: "pm-1",
  source_task_id: "task-1",
  issue_id: "issue-1",
  agent_id: "agent-1",
  trigger: "failed",
  state: "draft",
  failure_reason: "agent_error.context_overflow",
  summary: "The run exhausted the model context.",
  root_cause: "Too many large files were loaded at once.",
  impact: "The intended change was not delivered.",
  preventive_rules: ["Split large tasks into smaller sub-tasks."],
  cost_usd_ticks: 12345,
  llm_generated: true,
  revision: 1,
  created_at: "2026-01-01T00:00:00Z",
};

describe("listPostmortems", () => {
  it("parses a well-formed response and threads the cursor", async () => {
    stubFetchJson({ items: [validPostmortem], next_cursor: "abc" });
    const res = await new ApiClient("https://api.example.test").listPostmortems({
      state: "draft",
    });
    expect(res.items).toHaveLength(1);
    expect(res.items[0]?.summary).toBe("The run exhausted the model context.");
    expect(res.next_cursor).toBe("abc");
  });

  it("fills defaults for fields an older server omits", async () => {
    stubFetchJson({ items: [{ id: "pm-2", created_at: "2026-01-01T00:00:00Z" }] });
    const res = await new ApiClient("https://api.example.test").listPostmortems();
    expect(res.items).toHaveLength(1);
    expect(res.items[0]?.id).toBe("pm-2");
    expect(res.items[0]?.state).toBe("draft");
    expect(res.items[0]?.summary).toBe("");
    expect(res.items[0]?.preventive_rules).toEqual([]);
    expect(res.items[0]?.llm_generated).toBe(false);
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ items: "not-an-array" });
    const res = await new ApiClient("https://api.example.test").listPostmortems();
    expect(res).toEqual({ items: [] });
  });

  it("keeps a 500 as an ApiError", async () => {
    stubFetchJson({ error: "boom" }, 500);
    await expect(
      new ApiClient("https://api.example.test").listPostmortems(),
    ).rejects.toBeInstanceOf(ApiError);
  });
});

describe("getPostmortemStats", () => {
  it("parses a well-formed stats payload", async () => {
    stubFetchJson({ draft: 3, approved: 1, discarded: 2 });
    const stats = await new ApiClient("https://api.example.test").getPostmortemStats();
    expect(stats.draft).toBe(3);
    expect(stats.approved).toBe(1);
    expect(stats.discarded).toBe(2);
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ draft: "nope" });
    const stats = await new ApiClient("https://api.example.test").getPostmortemStats();
    expect(stats).toEqual({ draft: 0, approved: 0, discarded: 0 });
  });
});

describe("postmortemKeys", () => {
  it("nests items and stats under the workspace prefix", () => {
    expect(postmortemKeys.items("ws-1", "draft")).toEqual([
      ...postmortemKeys.all("ws-1"),
      "items",
      "draft",
    ]);
    expect(postmortemKeys.stats("ws-1")).toEqual([...postmortemKeys.all("ws-1"), "stats"]);
  });
});
