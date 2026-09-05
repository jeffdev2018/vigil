// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "../api/client";
import { brainKeys } from "./queries";

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

const validNote = {
  id: "note-1",
  workspace_id: "ws-1",
  title: "Deploys go through the release tag",
  content: "Push `v0.x.x` on main.",
  tags: ["deploy", "release"],
  source: "manual",
  source_task_id: null,
  source_agent_id: null,
  pinned: true,
  archived_at: null,
  merged_into: null,
  created_by_type: "member",
  created_by_id: "user-1",
  revision: 3,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
};

describe("listWorkspaceNotes", () => {
  it("parses a well-formed response", async () => {
    stubFetchJson({ items: [validNote], tags: ["deploy", "release"] });
    const res = await new ApiClient("https://api.example.test").listWorkspaceNotes();
    expect(res.items).toHaveLength(1);
    expect(res.items[0]?.title).toBe("Deploys go through the release tag");
    expect(res.tags).toEqual(["deploy", "release"]);
  });

  it("fills defaults for fields an older server omits", async () => {
    stubFetchJson({ items: [{ id: "note-2" }] });
    const res = await new ApiClient("https://api.example.test").listWorkspaceNotes();
    expect(res.items[0]?.tags).toEqual([]);
    expect(res.items[0]?.pinned).toBe(false);
    expect(res.items[0]?.source).toBe("manual");
    expect(res.tags).toEqual([]);
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ items: "not-an-array" });
    const res = await new ApiClient("https://api.example.test").listWorkspaceNotes();
    expect(res).toEqual({ items: [], tags: [] });
  });

  it("keeps a 500 as an ApiError", async () => {
    stubFetchJson({ error: "boom" }, 500);
    await expect(
      new ApiClient("https://api.example.test").listWorkspaceNotes(),
    ).rejects.toBeInstanceOf(ApiError);
  });

  it("sends search, tag and archived as query params", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [], tags: [] }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await new ApiClient("https://api.example.test").listWorkspaceNotes({
      search: "pgbouncer",
      tag: "db",
      archived: true,
    });
    const url = String(fetchMock.mock.calls[0]?.[0]);
    expect(url).toContain("search=pgbouncer");
    expect(url).toContain("tag=db");
    expect(url).toContain("archived=true");
  });
});

describe("updateWorkspaceNote", () => {
  it("surfaces a 409 as an ApiError so the UI can offer a reload", async () => {
    stubFetchJson({ error: "workspace note was modified by someone else" }, 409);
    await expect(
      new ApiClient("https://api.example.test").updateWorkspaceNote("note-1", {
        content: "x",
        revision: 1,
      }),
    ).rejects.toMatchObject({ status: 409 });
  });
});

describe("brainKeys", () => {
  it("nests the list under the workspace prefix, keyed by its server-side filters", () => {
    expect(brainKeys.list("ws-1", "pg", "db", false)).toEqual([
      ...brainKeys.all("ws-1"),
      "list",
      "pg",
      "db",
      false,
    ]);
  });
});
