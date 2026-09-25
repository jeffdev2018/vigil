// @vitest-environment node
import { describe, expect, it } from "vitest";
import { orgEscalationLabel, orgFormFromIssue, orgRequestFromText } from "./tester";

describe("orgRequestFromText", () => {
  it("splits the first line off as the title", () => {
    expect(orgRequestFromText("  Card declined\n\nSince the 3.2 release.  ")).toEqual({
      title: "Card declined",
      description: "Since the 3.2 release.",
    });
  });

  it("leaves the description empty for a one-line request", () => {
    expect(orgRequestFromText("Refund order 4821")).toEqual({ title: "Refund order 4821", description: "" });
  });

  it("keeps the whole text as the description when the single line is longer than a title", () => {
    const long = "a".repeat(250);
    expect(orgRequestFromText(long)).toEqual({ title: "a".repeat(200), description: long });
  });

  it("answers an empty request for blank input", () => {
    expect(orgRequestFromText("   \n  ")).toEqual({ title: "", description: "" });
  });
});

describe("orgFormFromIssue", () => {
  it("joins title and body and keeps label names apart", () => {
    expect(
      orgFormFromIssue({
        title: "Payment webhook retries forever",
        description: "Stripe answers 500 and we never stop.",
        labels: [{ name: "bug" }, { name: " billing " }, { name: "  " }],
      }),
    ).toEqual({ text: "Payment webhook retries forever\nStripe answers 500 and we never stop.", labels: ["bug", "billing"] });
  });

  it("handles an issue with no body and no label", () => {
    expect(orgFormFromIssue({ title: "Ship the changelog", description: null })).toEqual({
      text: "Ship the changelog",
      labels: [],
    });
  });
});

describe("orgEscalationLabel", () => {
  it("joins the ladder in reading order", () => {
    expect(orgEscalationLabel([{ unit_id: "lead", unit_name: "Lead" }, { unit_id: "cto", unit_name: "CTO" }], "root")).toBe("Lead → CTO");
  });

  it("falls back to the unit id when the name is missing", () => {
    expect(orgEscalationLabel([{ unit_id: "lead", unit_name: "" }], "root")).toBe("lead");
  });

  it("says the unit is the root rather than showing nothing", () => {
    expect(orgEscalationLabel([], "— (root)")).toBe("— (root)");
  });
});
