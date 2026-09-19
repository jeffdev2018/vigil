// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import type { QueryClient } from "@tanstack/react-query";
import { ApiClient, ApiError } from "../api/client";
import { EMPTY_BRAIN_CAPTURE } from "../api/schemas";
import { brainCaptureKeys, brainKeys } from "./queries";
import { onBrainCaptureChanged } from "./ws-updaters";

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

const validCapture = {
  id: "cap-1",
  workspace_id: "ws-1",
  kind: "link",
  content: "",
  url: "https://example.test/post",
  title_hint: "A post",
  attachment: null,
  origin: "web",
  status: "raw",
  transcription_status: "none",
  suggestion: {
    title: "Read the post",
    tags: ["reading"],
    summary: "A post about deploys.",
    action: "note",
    merge_note: null,
    candidates: [{ id: "note-1", title: "Deploys" }],
    reason: "New topic.",
    model: "test-model",
  },
  note_id: null,
  created_by_type: "member",
  created_by_id: "user-1",
  source_task_id: null,
  organized_by: null,
  organized_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

describe("listBrainCaptures", () => {
  it("parses a well-formed capture list", async () => {
    stubFetchJson({ captures: [validCapture], raw_count: 4 });
    const res = await new ApiClient("https://api.example.test").listBrainCaptures();
    expect(res.captures).toHaveLength(1);
    expect(res.captures[0]?.suggestion?.action).toBe("note");
    expect(res.raw_count).toBe(4);
  });

  it("fills defaults for fields an older server omits", async () => {
    stubFetchJson({ captures: [{ id: "cap-2" }] });
    const res = await new ApiClient("https://api.example.test").listBrainCaptures();
    expect(res.captures[0]?.kind).toBe("text");
    expect(res.captures[0]?.status).toBe("raw");
    expect(res.captures[0]?.transcription_status).toBe("none");
    expect(res.captures[0]?.suggestion ?? null).toBeNull();
    expect(res.raw_count).toBe(0);
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ captures: "not-an-array" });
    const res = await new ApiClient("https://api.example.test").listBrainCaptures();
    expect(res).toEqual({ captures: [], raw_count: 0 });
  });

  it("keeps the capture when only its suggestion is malformed", async () => {
    // The inbox must still show the item: a bad suggestion costs the
    // suggestion block, not the capture.
    stubFetchJson({
      captures: [{ ...validCapture, suggestion: { tags: "reading" } }],
      raw_count: 1,
    });
    const res = await new ApiClient("https://api.example.test").listBrainCaptures();
    expect(res.captures).toHaveLength(1);
    expect(res.captures[0]?.suggestion ?? null).toBeNull();
  });

  it("keeps the capture when only its attachment is malformed", async () => {
    stubFetchJson({
      captures: [{ ...validCapture, kind: "image", attachment: { id: 7 } }],
      raw_count: 1,
    });
    const res = await new ApiClient("https://api.example.test").listBrainCaptures();
    expect(res.captures).toHaveLength(1);
    expect(res.captures[0]?.attachment ?? null).toBeNull();
  });

  it("sends the status filter as a query param", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ captures: [], raw_count: 0 }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await new ApiClient("https://api.example.test").listBrainCaptures({
      status: "discarded",
    });
    expect(String(fetchMock.mock.calls[0]?.[0])).toContain("status=discarded");
  });
});

describe("createBrainCapture", () => {
  it("unwraps {capture}", async () => {
    stubFetchJson({ capture: validCapture }, 201);
    const capture = await new ApiClient("https://api.example.test").createBrainCapture({
      url: "https://example.test/post",
    });
    expect(capture.id).toBe("cap-1");
  });

  it("degrades a malformed body to the empty capture instead of throwing", async () => {
    stubFetchJson({ capture: { kind: "link" } }, 201);
    const capture = await new ApiClient("https://api.example.test").createBrainCapture({
      url: "https://example.test/post",
    });
    expect(capture.id).toBe("");
  });
});

describe("suggestBrainCapture", () => {
  it("surfaces a 503 as an ApiError so the UI can say 'no model configured'", async () => {
    stubFetchJson({ error: "no model configured" }, 503);
    await expect(
      new ApiClient("https://api.example.test").suggestBrainCapture("cap-1"),
    ).rejects.toMatchObject({ status: 503 });
  });
});

describe("organizeBrainCapture", () => {
  it("parses {capture, note}", async () => {
    stubFetchJson({
      capture: { ...validCapture, status: "organized", note_id: "note-9" },
      note: validNote,
    });
    const res = await new ApiClient("https://api.example.test").organizeBrainCapture(
      "cap-1",
      { action: "note", title: "A post" },
    );
    expect(res.capture.status).toBe("organized");
    expect(res.note?.id).toBe("note-1");
  });

  it("accepts a null note (discard)", async () => {
    stubFetchJson({ capture: { ...validCapture, status: "discarded" }, note: null });
    const res = await new ApiClient("https://api.example.test").organizeBrainCapture(
      "cap-1",
      { action: "discard" },
    );
    expect(res.note).toBeNull();
  });

  it("degrades a malformed body instead of throwing", async () => {
    stubFetchJson({ capture: "nope", note: 7 });
    const res = await new ApiClient("https://api.example.test").organizeBrainCapture(
      "cap-1",
      { action: "discard" },
    );
    expect(res).toEqual({ capture: EMPTY_BRAIN_CAPTURE, note: null });
  });

  it("keeps a 409 (already organized) as an ApiError", async () => {
    stubFetchJson({ error: "capture is not raw" }, 409);
    await expect(
      new ApiClient("https://api.example.test").organizeBrainCapture("cap-1", {
        action: "note",
      }),
    ).rejects.toMatchObject({ status: 409 });
  });
});

describe("searchWorkspaceNotes", () => {
  it("parses hits with their snippet, score and ranks", async () => {
    stubFetchJson({
      notes: [
        {
          ...validNote,
          score: 0.83,
          snippet: "push <mark>v0.x.x</mark>",
          passage_heading: "Release › Tags",
          lex_rank: 1,
          vec_rank: 2,
        },
      ],
      vector: true,
    });
    const res = await new ApiClient("https://api.example.test").searchWorkspaceNotes({
      q: "release",
    });
    expect(res.vector).toBe(true);
    expect(res.notes[0]?.snippet).toBe("push <mark>v0.x.x</mark>");
    expect(res.notes[0]?.passage_heading).toBe("Release › Tags");
    expect(res.notes[0]?.lex_rank).toBe(1);
  });

  it("fills defaults when the server omits the ranking fields", async () => {
    stubFetchJson({ notes: [{ id: "note-3" }] });
    const res = await new ApiClient("https://api.example.test").searchWorkspaceNotes({
      q: "release",
    });
    expect(res.notes[0]?.score).toBe(0);
    expect(res.notes[0]?.snippet).toBe("");
    expect(res.notes[0]?.passage_heading).toBe("");
    expect(res.vector).toBe(false);
  });

  it("keeps the hits when a server sends a malformed passage_heading", async () => {
    stubFetchJson({ notes: [{ ...validNote, snippet: "x", passage_heading: 42 }], vector: false });
    const res = await new ApiClient("https://api.example.test").searchWorkspaceNotes({
      q: "release",
    });
    expect(res.notes).toHaveLength(1);
    expect(res.notes[0]?.passage_heading).toBe("");
  });

  it("degrades a malformed body to the empty fallback instead of throwing", async () => {
    stubFetchJson({ notes: { id: "note-3" } });
    const res = await new ApiClient("https://api.example.test").searchWorkspaceNotes({
      q: "release",
    });
    expect(res).toEqual({ notes: [], vector: false });
  });

  it("sends q, tag, archived and limit as query params", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ notes: [], vector: false }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    await new ApiClient("https://api.example.test").searchWorkspaceNotes({
      q: "pg bouncer",
      tag: "db",
      archived: true,
      limit: 5,
    });
    const url = String(fetchMock.mock.calls[0]?.[0]);
    expect(url).toContain("q=pg+bouncer");
    expect(url).toContain("tag=db");
    expect(url).toContain("archived=true");
    expect(url).toContain("limit=5");
  });
});

describe("brainCaptureKeys", () => {
  it("hangs the list and the detail off one capture prefix", () => {
    expect(brainCaptureKeys.list("ws-1", "raw")).toEqual([
      ...brainCaptureKeys.captures("ws-1"),
      "list",
      "raw",
    ]);
    expect(brainCaptureKeys.detail("ws-1", "cap-1")).toEqual([
      ...brainCaptureKeys.captures("ws-1"),
      "detail",
      "cap-1",
    ]);
  });
});

describe("onBrainCaptureChanged", () => {
  function spyClient() {
    const calls: unknown[][] = [];
    return {
      calls,
      qc: {
        invalidateQueries: (filters: { queryKey: unknown[] }) => {
          calls.push(filters.queryKey);
        },
      } as unknown as QueryClient,
    };
  }

  it("refreshes only the capture projection for a suggestion", () => {
    const { calls, qc } = spyClient();
    onBrainCaptureChanged(qc, "ws-1", "suggested");
    expect(calls).toEqual([brainCaptureKeys.captures("ws-1")]);
  });

  it("refreshes the whole Brain prefix when a note was written", () => {
    for (const change of ["note", "merge"]) {
      const { calls, qc } = spyClient();
      onBrainCaptureChanged(qc, "ws-1", change);
      expect(calls).toEqual([brainKeys.all("ws-1")]);
    }
  });

  it("still refreshes the inbox for an unknown change", () => {
    const { calls, qc } = spyClient();
    onBrainCaptureChanged(qc, "ws-1", undefined);
    expect(calls).toEqual([brainCaptureKeys.captures("ws-1")]);
  });
});
