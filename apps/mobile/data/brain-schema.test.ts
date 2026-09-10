// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  BrainCaptureResponseSchema,
  BrainCapturesResponseSchema,
  OrganizeBrainCaptureResponseSchema,
  WorkspaceNoteSchema,
  WorkspaceNoteSearchResponseSchema,
  WorkspaceNotesResponseSchema,
  EMPTY_BRAIN_CAPTURES_RESPONSE,
  EMPTY_WORKSPACE_NOTES_RESPONSE,
  EMPTY_WORKSPACE_NOTE_SEARCH_RESPONSE,
} from "@multica/core/api/schemas";
import { parseWithFallback } from "@/lib/parse-response";

/**
 * Mobile's CLIENT-SIDE parsing of the Brain endpoints.
 *
 * Scope, stated precisely: these are hand-written fixtures run against the
 * SHARED schemas in `@multica/core/api/schemas` (pure zod, on the mobile
 * sharing whitelist — apps/mobile/CLAUDE.md "ApiClient capability list").
 * They pin how the phone REACTS to a payload; nothing here executes Go.
 *
 * Why they live in apps/mobile even though the schemas are shared: mobile's
 * exposure is different. Web renders the Brain in a two-pane page it can
 * reload; mobile hangs a More-popover badge off `raw_count` and drives a
 * pull-to-refresh list off `captures`, so a whole-response fallback shows an
 * empty inbox with a silent zero badge. The malformed cases below document
 * exactly which drift costs one row and which costs the screen.
 *
 * The matching guarantee for the enums is behavioural, not structural: every
 * kind / origin / status / source is a plain `z.string()` on purpose, so a
 * value added server-side reaches lib/brain-display.ts and degrades to a
 * readable label there (see lib/brain-display.test.ts).
 */

const CAPTURE_ROW = {
  id: "11111111-1111-1111-1111-111111111111",
  workspace_id: "ws-1",
  kind: "audio",
  content: "",
  url: "",
  title_hint: "voice-memo-3",
  attachment: {
    id: "att-1",
    url: "mc://file/att-1",
    download_url: "https://cdn.test/att-1",
    markdown_url: "https://cdn.test/att-1",
    filename: "voice-memo-3.m4a",
  },
  origin: "mobile",
  status: "raw",
  transcription_status: "pending",
  suggestion: null,
  note_id: null,
  created_by_type: "member",
  created_by_id: "user-1",
  source_task_id: null,
  organized_by: null,
  organized_at: null,
  created_at: "2026-09-10T08:00:00Z",
  updated_at: "2026-09-10T08:00:00Z",
};

const NOTE_ROW = {
  id: "22222222-2222-2222-2222-222222222222",
  workspace_id: "ws-1",
  title: "Release checklist",
  content: "Tag `v0.x.x` on main.",
  tags: ["release", "ops"],
  source: "capture",
  source_task_id: null,
  source_agent_id: null,
  pinned: true,
  archived_at: null,
  merged_into: null,
  created_by_type: "member",
  created_by_id: "user-1",
  revision: 4,
  created_at: "2026-09-10T08:00:00Z",
  updated_at: "2026-09-10T09:00:00Z",
};

describe("capture list schema", () => {
  it("parses the documented payload, including raw_count", () => {
    const parsed = BrainCapturesResponseSchema.safeParse({
      captures: [CAPTURE_ROW],
      raw_count: 7,
    });
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data.raw_count).toBe(7);
    expect(parsed.success && parsed.data.captures[0]?.kind).toBe("audio");
    expect(parsed.success && parsed.data.captures[0]?.attachment?.filename).toBe(
      "voice-memo-3.m4a",
    );
  });

  it("keeps a capture whose kind / origin / status the phone has never seen", () => {
    const parsed = BrainCapturesResponseSchema.safeParse({
      captures: [
        { ...CAPTURE_ROW, kind: "sketch", origin: "slack", status: "queued" },
      ],
      raw_count: 1,
    });
    // Never silently drop a category: the row survives and the display layer
    // labels the unknown value.
    expect(parsed.success && parsed.data.captures[0]?.kind).toBe("sketch");
  });

  it("degrades a malformed attachment to absent, keeping the capture", () => {
    const parsed = BrainCapturesResponseSchema.safeParse({
      captures: [{ ...CAPTURE_ROW, attachment: { id: 7 } }],
      raw_count: 1,
    });
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data.captures[0]?.attachment).toBeNull();
    expect(parsed.success && parsed.data.captures[0]?.id).toBe(CAPTURE_ROW.id);
  });

  it("degrades a malformed suggestion to absent, keeping the capture", () => {
    const parsed = BrainCapturesResponseSchema.safeParse({
      captures: [{ ...CAPTURE_ROW, suggestion: { tags: "release" } }],
      raw_count: 1,
    });
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data.captures[0]?.suggestion).toBeNull();
  });

  it("defaults raw_count and captures rather than crashing the screen", () => {
    const parsed = BrainCapturesResponseSchema.safeParse({});
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data.raw_count).toBe(0);
    expect(parsed.success && parsed.data.captures).toEqual([]);
  });

  it("falls back to an empty inbox when the whole body is the wrong shape", () => {
    // An `id`-less capture fails the array element, which fails the response
    // — the blast radius is the whole inbox, not one row. That is why the
    // enums are lenient and only `id` is required.
    const value = parseWithFallback(
      { captures: [{ kind: "text" }], raw_count: 3 },
      BrainCapturesResponseSchema,
      EMPTY_BRAIN_CAPTURES_RESPONSE,
      { endpoint: "GET /api/brain/captures" },
    );
    expect(value).toEqual(EMPTY_BRAIN_CAPTURES_RESPONSE);
    expect(value.raw_count).toBe(0);

    expect(
      parseWithFallback(
        "not json at all",
        BrainCapturesResponseSchema,
        EMPTY_BRAIN_CAPTURES_RESPONSE,
        { endpoint: "GET /api/brain/captures" },
      ),
    ).toEqual(EMPTY_BRAIN_CAPTURES_RESPONSE);
  });
});

describe("capture envelope schemas", () => {
  it("unwraps {capture} from create / get / suggest / reopen", () => {
    const parsed = BrainCaptureResponseSchema.safeParse({
      capture: CAPTURE_ROW,
    });
    expect(parsed.success && parsed.data.capture.title_hint).toBe("voice-memo-3");
  });

  it("rejects an envelope with no capture at all", () => {
    expect(BrainCaptureResponseSchema.safeParse({}).success).toBe(false);
  });

  it("parses a suggestion with a merge target and candidates", () => {
    const parsed = BrainCaptureResponseSchema.safeParse({
      capture: {
        ...CAPTURE_ROW,
        suggestion: {
          title: "Release checklist",
          tags: ["release"],
          summary: "Steps to cut a release.",
          action: "merge",
          merge_note: { id: NOTE_ROW.id, title: "Release checklist" },
          candidates: [{ id: NOTE_ROW.id, title: "Release checklist" }],
          reason: "Same topic as an existing note.",
          model: "test-model",
        },
      },
    });
    expect(parsed.success && parsed.data.capture.suggestion?.action).toBe("merge");
    expect(parsed.success && parsed.data.capture.suggestion?.merge_note?.id).toBe(
      NOTE_ROW.id,
    );
  });

  it("keeps a suggestion whose action is unknown", () => {
    const parsed = BrainCaptureResponseSchema.safeParse({
      capture: {
        ...CAPTURE_ROW,
        suggestion: {
          title: "t",
          tags: [],
          summary: "",
          action: "escalate",
          candidates: [],
          reason: "",
        },
      },
    });
    expect(parsed.success && parsed.data.capture.suggestion?.action).toBe(
      "escalate",
    );
  });

  it("parses organize with a note and organize with none", () => {
    const withNote = OrganizeBrainCaptureResponseSchema.safeParse({
      capture: { ...CAPTURE_ROW, status: "organized", note_id: NOTE_ROW.id },
      note: NOTE_ROW,
    });
    expect(withNote.success && withNote.data.note?.revision).toBe(4);

    const discarded = OrganizeBrainCaptureResponseSchema.safeParse({
      capture: { ...CAPTURE_ROW, status: "discarded" },
      note: null,
    });
    expect(discarded.success && discarded.data.note).toBeNull();

    // A discard response that omits `note` entirely still parses.
    const omitted = OrganizeBrainCaptureResponseSchema.safeParse({
      capture: CAPTURE_ROW,
    });
    expect(omitted.success && omitted.data.note).toBeNull();
  });
});

describe("note schemas", () => {
  it("parses the list plus its tag facets", () => {
    const parsed = WorkspaceNotesResponseSchema.safeParse({
      items: [NOTE_ROW],
      tags: ["ops", "release"],
    });
    expect(parsed.success && parsed.data.items[0]?.source).toBe("capture");
    expect(parsed.success && parsed.data.tags).toEqual(["ops", "release"]);
  });

  it("defaults items and tags on an empty body", () => {
    const parsed = WorkspaceNotesResponseSchema.safeParse({});
    expect(parsed.success && parsed.data.items).toEqual([]);
    expect(parsed.success && parsed.data.tags).toEqual([]);
  });

  it("falls back to an empty Brain on a malformed list", () => {
    expect(
      parseWithFallback(
        { items: "nope" },
        WorkspaceNotesResponseSchema,
        EMPTY_WORKSPACE_NOTES_RESPONSE,
        { endpoint: "GET /api/workspace/notes" },
      ),
    ).toEqual(EMPTY_WORKSPACE_NOTES_RESPONSE);
  });

  it("keeps revision as a number — the conflict guard depends on it", () => {
    const parsed = WorkspaceNoteSchema.safeParse(NOTE_ROW);
    expect(parsed.success && parsed.data.revision).toBe(4);
    // A string revision is drift the PATCH must not inherit: it fails the
    // parse, so getWorkspaceNote falls back to revision 0 and the server
    // answers 400 ("send the revision you read") rather than clobbering.
    expect(
      WorkspaceNoteSchema.safeParse({ ...NOTE_ROW, revision: "4" }).success,
    ).toBe(false);
  });
});

describe("ranked search schema", () => {
  it("parses hits with score, snippet and both ranks", () => {
    const parsed = WorkspaceNoteSearchResponseSchema.safeParse({
      notes: [
        {
          ...NOTE_ROW,
          score: 0.032,
          snippet: "Tag <mark>v0.x.x</mark> on main.",
          lex_rank: 1,
          vec_rank: 3,
        },
      ],
      vector: true,
    });
    expect(parsed.success && parsed.data.vector).toBe(true);
    expect(parsed.success && parsed.data.notes[0]?.snippet).toContain("<mark>");
    expect(parsed.success && parsed.data.notes[0]?.lex_rank).toBe(1);
  });

  it("accepts a lexical-only response (no embedder configured)", () => {
    const parsed = WorkspaceNoteSearchResponseSchema.safeParse({
      notes: [{ ...NOTE_ROW, score: 0.1, snippet: "", lex_rank: 1, vec_rank: null }],
      vector: false,
    });
    expect(parsed.success && parsed.data.notes[0]?.vec_rank).toBeNull();
    expect(parsed.success && parsed.data.notes[0]?.snippet).toBe("");
  });

  it("defaults score / snippet / vector when a server omits them", () => {
    const parsed = WorkspaceNoteSearchResponseSchema.safeParse({
      notes: [NOTE_ROW],
    });
    expect(parsed.success && parsed.data.notes[0]?.score).toBe(0);
    expect(parsed.success && parsed.data.notes[0]?.snippet).toBe("");
    expect(parsed.success && parsed.data.vector).toBe(false);
  });

  it("falls back to no results on a malformed search body", () => {
    expect(
      parseWithFallback(
        { notes: [{ title: "no id" }] },
        WorkspaceNoteSearchResponseSchema,
        EMPTY_WORKSPACE_NOTE_SEARCH_RESPONSE,
        { endpoint: "GET /api/workspace/notes/search" },
      ),
    ).toEqual(EMPTY_WORKSPACE_NOTE_SEARCH_RESPONSE);
  });
});
