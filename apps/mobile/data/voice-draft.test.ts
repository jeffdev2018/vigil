// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "@/lib/parse-response";
import {
  EMPTY_VOICE_ISSUE_DRAFT,
  VoiceIssueDraftSchema,
} from "./schemas";

/**
 * Client-side parsing of POST /api/issues/from-voice-transcript (K36).
 *
 * Scope, stated precisely: these are hand-written fixtures run against
 * `VoiceIssueDraftSchema` through the same `parseWithFallback` the API client
 * uses. They pin how this client REACTS to a payload; nothing here executes
 * server code, so they cannot fail when the Go handler starts sending
 * something new.
 *
 * Why it matters here more than on a read-only list: the draft lands directly
 * in a title input, a description input and a chip row. A partial value would
 * be an uneditable form rather than a caught error, so every field defaults
 * and the whole response degrades to EMPTY_VOICE_ISSUE_DRAFT — an empty but
 * fully editable draft — rather than throwing at the speaker.
 */
const parse = (raw: unknown) =>
  parseWithFallback(raw, VoiceIssueDraftSchema, EMPTY_VOICE_ISSUE_DRAFT, {
    endpoint: "issueDraftFromVoice",
  });

describe("voice issue draft schema", () => {
  it("parses the documented server payload", () => {
    expect(
      parse({
        title: "Fix the export button",
        description: "It does nothing on Safari.",
        suggested_labels: ["bug", "web"],
      }),
    ).toEqual({
      title: "Fix the export button",
      description: "It does nothing on Safari.",
      suggested_labels: ["bug", "web"],
    });
  });

  it("keeps a field the server adds later", () => {
    const parsed = parse({
      title: "T",
      description: "D",
      suggested_labels: [],
      confidence: 0.4,
    }) as Record<string, unknown>;
    expect(parsed.title).toBe("T");
    expect(parsed.confidence).toBe(0.4);
  });

  it("defaults every field the server omits", () => {
    expect(parse({})).toEqual(EMPTY_VOICE_ISSUE_DRAFT);
  });

  it("survives a wrongly-typed field without losing the rest", () => {
    // `.catch` is per-field, so a bad labels array costs the labels only.
    expect(parse({ title: "Kept", description: "Kept too", suggested_labels: "bug" })).toEqual({
      title: "Kept",
      description: "Kept too",
      suggested_labels: [],
    });
    expect(parse({ title: 12, description: null, suggested_labels: [] })).toEqual(
      EMPTY_VOICE_ISSUE_DRAFT,
    );
  });

  it("falls back to an empty editable draft when the body is not an object", () => {
    for (const raw of [null, "boom", [], 7]) {
      expect(parse(raw)).toEqual(EMPTY_VOICE_ISSUE_DRAFT);
    }
  });
});
