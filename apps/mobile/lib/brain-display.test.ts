// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { BrainCapture, WorkspaceNote } from "@multica/core/types";
import {
  captureHeadline,
  captureKindLabel,
  captureOriginLabel,
  captureStatusLabel,
  captureSubline,
  captureTranscriptionChip,
  isNoteArchived,
  lonelyHttpUrl,
  noteSourceLabel,
  parseSearchSnippet,
  parseTagInput,
  searchSnippetText,
} from "./brain-display";

/**
 * Canonical layer for the Brain presentation rules (apps/mobile/CLAUDE.md
 * "Give each product behavior ONE canonical layer"). The screens are not
 * expected to re-run this matrix.
 *
 * The snippet cases are the ones that matter most: `snippet` is the note's
 * own member-written text with `<mark>` inserted by PostgreSQL and NOTHING
 * escaped, so the parser is the only thing standing between a note body and
 * the render path.
 */

const capture = (over: Partial<BrainCapture> = {}): BrainCapture => ({
  id: "c1",
  workspace_id: "ws1",
  kind: "text",
  content: "",
  url: "",
  title_hint: "",
  attachment: null,
  origin: "mobile",
  status: "raw",
  transcription_status: "none",
  suggestion: null,
  note_id: null,
  created_by_type: "member",
  created_at: "2026-09-10T00:00:00Z",
  updated_at: "2026-09-10T00:00:00Z",
  ...over,
});

const note = (over: Partial<WorkspaceNote> = {}): WorkspaceNote => ({
  id: "n1",
  workspace_id: "ws1",
  title: "Release",
  content: "",
  tags: [],
  source: "manual",
  pinned: false,
  created_by_type: "member",
  revision: 1,
  created_at: "",
  updated_at: "",
  ...over,
});

describe("server-driven enums never blank a row", () => {
  it("labels every documented capture kind", () => {
    expect(
      ["text", "link", "image", "audio", "file", "todo"].map(captureKindLabel),
    ).toEqual(["Note", "Link", "Photo", "Voice", "File", "To do"]);
  });

  it("falls back to the raw value for a kind the phone does not know", () => {
    expect(captureKindLabel("sketch")).toBe("sketch");
    expect(captureOriginLabel("slack")).toBe("slack");
    expect(captureStatusLabel("frozen")).toBe("frozen");
    expect(noteSourceLabel("distilled")).toBe("distilled");
  });

  it("names the capture origins the contract lists", () => {
    expect(captureOriginLabel("mobile")).toBe("Phone");
    expect(captureOriginLabel("channel")).toBe("Chat");
    expect(captureOriginLabel("")).toBe("Unknown");
  });

  it("names the note source the organize flow stamps", () => {
    expect(noteSourceLabel("capture")).toBe("Captured");
    expect(noteSourceLabel("curation")).toBe("Curated");
  });
});

describe("transcription chip", () => {
  it("only speaks up while pending or after a failure", () => {
    expect(captureTranscriptionChip("pending")).toBe("Transcribing…");
    expect(captureTranscriptionChip("failed")).toBe("Transcription failed");
    expect(captureTranscriptionChip("done")).toBeNull();
    expect(captureTranscriptionChip("none")).toBeNull();
  });
});

describe("captureHeadline", () => {
  it("prefers the title hint, like the server does on organize", () => {
    expect(
      captureHeadline(capture({ title_hint: "Ship notes", content: "body" })),
    ).toBe("Ship notes");
  });

  it("falls back to the first line of the content", () => {
    expect(captureHeadline(capture({ content: "first\nsecond" }))).toBe("first");
  });

  it("falls back to the url, then the file name", () => {
    expect(captureHeadline(capture({ url: "https://x.test/a" }))).toBe(
      "https://x.test/a",
    );
    expect(
      captureHeadline(
        capture({
          kind: "file",
          attachment: {
            id: "a1",
            url: "u",
            download_url: "d",
            filename: "spec.pdf",
          },
        }),
      ),
    ).toBe("spec.pdf");
  });

  it("says so rather than rendering an empty row", () => {
    expect(captureHeadline(capture())).toBe("Empty capture");
  });

  it("truncates by characters, not bytes", () => {
    expect(captureHeadline(capture({ content: "é".repeat(10) }), 4)).toBe("éééé…");
  });
});

describe("captureSubline", () => {
  it("is null when the headline already is the content", () => {
    expect(captureSubline(capture({ content: "one line" }))).toBeNull();
  });

  it("shows the collapsed body when the headline came from the hint", () => {
    expect(
      captureSubline(capture({ title_hint: "Hint", content: "a\n  b   c" })),
    ).toBe("a b c");
  });
});

describe("parseSearchSnippet", () => {
  it("marks the runs ts_headline wrapped", () => {
    expect(parseSearchSnippet("push <mark>v0.x.x</mark> on main")).toEqual([
      { text: "push ", hit: false },
      { text: "v0.x.x", hit: true },
      { text: " on main", hit: false },
    ]);
  });

  it("drops every other tag instead of rendering it", () => {
    expect(parseSearchSnippet('<script>alert("x")</script>')).toEqual([
      { text: 'alert("x")', hit: false },
    ]);
    expect(parseSearchSnippet('<img src=x onerror="1">after')).toEqual([
      { text: "after", hit: false },
    ]);
  });

  it("drops an unclosed trailing tag", () => {
    expect(parseSearchSnippet("text <b")).toEqual([
      { text: "text ", hit: false },
    ]);
  });

  it("strips markup inside a marked run too", () => {
    expect(parseSearchSnippet("<mark><i>hit</i></mark>")).toEqual([
      { text: "hit", hit: true },
    ]);
  });

  it("decodes the entities ts_headline can emit", () => {
    expect(parseSearchSnippet("a &amp; b &lt;c&gt;")).toEqual([
      { text: "a & b <c>", hit: false },
    ]);
  });

  it("handles several marks and an empty snippet", () => {
    expect(parseSearchSnippet("<mark>a</mark>-<mark>b</mark>")).toEqual([
      { text: "a", hit: true },
      { text: "-", hit: false },
      { text: "b", hit: true },
    ]);
    expect(parseSearchSnippet("")).toEqual([]);
  });

  it("flattens to plain text for accessibility labels", () => {
    expect(searchSnippetText("push <mark>v0.x.x</mark> on main")).toBe(
      "push v0.x.x on main",
    );
  });
});

describe("lonelyHttpUrl", () => {
  it("accepts a single http(s) url", () => {
    expect(lonelyHttpUrl("  https://x.test/a?b=1 ")).toBe("https://x.test/a?b=1");
    expect(lonelyHttpUrl("http://x.test")).toBe("http://x.test");
  });

  it("refuses anything the server would not treat as a link capture", () => {
    expect(lonelyHttpUrl("see https://x.test")).toBeNull();
    expect(lonelyHttpUrl("ftp://x.test")).toBeNull();
    expect(lonelyHttpUrl("x.test")).toBeNull();
    expect(lonelyHttpUrl("https://")).toBeNull();
    expect(lonelyHttpUrl("")).toBeNull();
    expect(lonelyHttpUrl(`https://x.test/${"a".repeat(2100)}`)).toBeNull();
  });
});

describe("note helpers", () => {
  it("reads archived off archived_at only", () => {
    expect(isNoteArchived(note())).toBe(false);
    expect(isNoteArchived(note({ archived_at: null }))).toBe(false);
    expect(isNoteArchived(note({ archived_at: "" }))).toBe(false);
    expect(isNoteArchived(note({ archived_at: "2026-09-10T00:00:00Z" }))).toBe(
      true,
    );
  });

  it("parses the tag field the way web does", () => {
    expect(parseTagInput(" release , ops ,, ")).toEqual(["release", "ops"]);
    expect(parseTagInput("")).toEqual([]);
  });
});
