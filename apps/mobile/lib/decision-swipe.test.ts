// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { InboxDecision } from "@/data/schemas";
import {
  condensedSummary,
  swipeRightAnswer,
  swipeRightLabel,
} from "./decision-swipe";

type Decision = InboxDecision["decision"];

const decision = (over: Partial<Decision> = {}): Decision =>
  ({
    id: "dec-1",
    issue_id: "issue-1",
    question: "Ship the migration tonight?",
    options: [
      { id: "a", label: "Ship it", impact: "deploys today" },
      { id: "b", label: "Wait" },
    ],
    urgency: "normal",
    sla_deadline_at: null,
    created_at: "2026-09-01T00:00:00Z",
    ...over,
  }) as Decision;

describe("swipeRightAnswer", () => {
  it("sends the recommended option", () => {
    expect(swipeRightAnswer(decision({ recommended_option_id: "b" }))).toEqual({
      option_id: "b",
    });
    expect(swipeRightLabel(decision({ recommended_option_id: "b" }))).toBe("Wait");
  });

  it("falls back to the first option when the server recommended none", () => {
    expect(swipeRightAnswer(decision())).toEqual({ option_id: "a" });
    expect(swipeRightLabel(decision())).toBe("Ship it");
  });

  it("ignores a recommendation pointing at an option that is not there", () => {
    expect(swipeRightAnswer(decision({ recommended_option_id: "gone" }))).toEqual(
      { option_id: "a" },
    );
  });

  it("refuses to invent an answer for an option-less card", () => {
    // Free-text-only card: the swipe must open the sheet, not answer.
    expect(swipeRightAnswer(decision({ options: [] }))).toBeNull();
    expect(swipeRightLabel(decision({ options: [] }))).toBeNull();
  });

  it("refuses an option the server sent without an id", () => {
    const broken = decision({ options: [{ id: "", label: "Nameless" }] });
    expect(swipeRightAnswer(broken)).toBeNull();
  });
});

describe("condensedSummary", () => {
  it("maps the three urgencies web renders", () => {
    expect(condensedSummary(decision({ urgency: "high" })).urgencyLabel).toBe("urgent");
    expect(condensedSummary(decision({ urgency: "normal" })).urgencyLabel).toBe("normal");
    expect(condensedSummary(decision({ urgency: "low" })).urgencyLabel).toBe("low");
  });

  it("coerces an urgency the server added later to normal", () => {
    // Enum-fallback rule: never drop the card, never render a raw value.
    expect(condensedSummary(decision({ urgency: "blocking" })).urgencyLabel).toBe("normal");
  });

  it("has no deadline line without an SLA", () => {
    expect(condensedSummary(decision()).deadlineText).toBeNull();
  });

  it("phrases the deadline the way web does", () => {
    const past = new Date(Date.now() - 2 * 3600_000).toISOString();
    expect(condensedSummary(decision({ sla_deadline_at: past })).deadlineText).toBe("due 2h ago");
  });

  it("lines up each option with its impact and marks the recommendation", () => {
    expect(condensedSummary(decision({ recommended_option_id: "a" })).optionLines).toEqual([
      "Ship it · recommended — deploys today",
      "Wait",
    ]);
  });

  it("survives a card the server sent with no options array", () => {
    const bare = { ...decision(), options: undefined } as unknown as Decision;
    expect(condensedSummary(bare).optionLines).toEqual([]);
  });
});
