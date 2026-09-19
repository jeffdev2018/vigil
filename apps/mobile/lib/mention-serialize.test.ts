// @vitest-environment node
import { describe, expect, it } from "vitest";
import { serializeMentions, type MentionMarker } from "./mention-serialize";

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
