import { describe, expect, it } from "vitest";
import {
  POSTMORTEM_STATES,
  formatPostmortemCost,
  postmortemEmptyMessage,
  postmortemOriginLabel,
  postmortemStateLabel,
} from "./postmortem-display";

describe("POSTMORTEM_STATES", () => {
  it("lists the three server states in web's filter order", () => {
    expect(POSTMORTEM_STATES).toEqual(["draft", "approved", "discarded"]);
  });
});

describe("postmortemStateLabel", () => {
  it("labels every known state", () => {
    expect(POSTMORTEM_STATES.map(postmortemStateLabel)).toEqual([
      "Draft",
      "Approved",
      "Discarded",
    ]);
  });

  it("falls back to the raw value for a state added server-side", () => {
    expect(postmortemStateLabel("archived")).toBe("archived");
  });
});

describe("formatPostmortemCost", () => {
  // Must stay byte-identical to formatCost in
  // packages/views/postmortem/components/postmortem-page.tsx.
  it("uses four decimals below one cent", () => {
    expect(formatPostmortemCost(7_000_000)).toBe("$0.0007");
    expect(formatPostmortemCost(1)).toBe("$0.0000");
  });

  it("uses two decimals from one cent up", () => {
    expect(formatPostmortemCost(100_000_000)).toBe("$0.01");
    expect(formatPostmortemCost(12_340_000_000)).toBe("$1.23");
  });

  it("formats zero with the sub-cent branch", () => {
    expect(formatPostmortemCost(0)).toBe("$0.0000");
  });
});

describe("postmortemOriginLabel", () => {
  it("distinguishes the LLM draft from the scaffold", () => {
    expect(postmortemOriginLabel(true)).toBe("AI-drafted");
    expect(postmortemOriginLabel(false)).toBe("Auto-filled from run facts");
  });
});

describe("postmortemEmptyMessage", () => {
  it("gives each bucket its own copy", () => {
    const messages = POSTMORTEM_STATES.map(postmortemEmptyMessage);
    expect(new Set(messages).size).toBe(3);
    expect(messages[0]).toContain("No drafts waiting");
  });
});
