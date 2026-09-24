// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  serializeMentions,
  tokenAtCursor,
  type MentionMarker,
} from "./mention-serialize";

const SENTINEL = "⁣";

describe("serializeMentions", () => {
  it("links a mention whose display name contains a space", () => {
    const marker: MentionMarker = {
      type: "member",
      id: "u1",
      name: "Jean Dupont",
    };
    const text = `Hi ${SENTINEL}@Jean Dupont please review`;
    expect(serializeMentions(text, [marker])).toBe(
      "Hi [@Jean Dupont](mention://member/u1) please review",
    );
  });

  it("links multiple multi-word mentions in order", () => {
    const markers: MentionMarker[] = [
      { type: "member", id: "u1", name: "Jean Dupont" },
      { type: "agent", id: "a1", name: "Code Reviewer" },
    ];
    const text = `${SENTINEL}@Jean Dupont and ${SENTINEL}@Code Reviewer please look`;
    expect(serializeMentions(text, markers)).toBe(
      "[@Jean Dupont](mention://member/u1) and [@Code Reviewer](mention://agent/a1) please look",
    );
  });

  it("renders an issue mention without the leading @ in the label", () => {
    const marker: MentionMarker = {
      type: "issue",
      id: "issue-1",
      name: "MUL-123",
    };
    const text = `see ${SENTINEL}@MUL-123 for details`;
    expect(serializeMentions(text, [marker])).toBe(
      "see [MUL-123](mention://issue/issue-1) for details",
    );
  });

  it("links @all", () => {
    const marker: MentionMarker = { type: "all", id: "all", name: "all" };
    const text = `${SENTINEL}@all please review`;
    expect(serializeMentions(text, [marker])).toBe(
      "[@all](mention://all/all) please review",
    );
  });

  it("falls back to plain text (sentinels stripped) when a marker name truly mismatches", () => {
    const marker: MentionMarker = { type: "member", id: "u1", name: "Bo" };
    const text = `Hi ${SENTINEL}@Nope there`;
    expect(serializeMentions(text, [marker])).toBe("Hi @Nope there");
  });
});

// The mobile half of the shared boundary rule. The rule's own matrix lives in
// packages/core/markdown/mention-boundary.test.ts; this covers the wiring — that
// `tokenAtCursor` consults it, and that the sentinel guard still wins.

/** Cursor sits at the end of `text`. */
function at(text: string) {
  return tokenAtCursor(text, text.length);
}

describe("tokenAtCursor boundary", () => {
  it.each([
    ["an empty box", "@Mi"],
    ["after a half-width space", "hello @Mi"],
    ["a full-width space", "你好　@Mi"],
    ["after CJK with no separator", "你好@Mi"],
    ["after katakana", "テレビ@Mi"],
    ["after the prolonged sound mark", "コーヒー@Mi"],
    ["after punctuation", "hello(@Mi"],
  ])("opens %s", (_name, text) => {
    expect(at(text)).toEqual({ start: text.indexOf("@"), query: "Mi" });
  });

  it.each([
    ["an ASCII word", "hello@Mi"],
    ["a digit", "2024@Mi"],
    ["an address", "user@example.com"],
    ["an accented address", "josé@example.com"],
    ["a cyrillic address", "почта@mail.ru"],
    ["a greek address", "αλφα@example.com"],
  ])("stays shut inside %s", (_name, text) => {
    expect(at(text)).toBeNull();
  });

  it("stays shut over a completed mention", () => {
    // The bar inserts `⁣@Name `; a cursor inside the inserted text, or at the
    // space after it, must not re-open the bar.
    expect(tokenAtCursor("⁣@Mika", 6)).toBeNull();
    expect(tokenAtCursor("⁣@Mika", 4)).toBeNull();
    expect(tokenAtCursor("⁣@Mika ", 7)).toBeNull();
  });

  it("still returns the query when the cursor is mid-token", () => {
    expect(tokenAtCursor("你好@Mi", 5)).toEqual({ start: 2, query: "Mi" });
  });
});
