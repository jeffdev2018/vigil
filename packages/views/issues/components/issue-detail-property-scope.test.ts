// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IssueProperty } from "@multica/core/types";
import {
  formatHiddenPropertyValue,
  propertyAppliesToIssueType,
} from "./issue-detail";

// The applicability rule and the folded-value rendering, tested where they
// live. The component suite covers the fold's wiring; this is the matrix.

function property(over: Partial<IssueProperty> = {}): IssueProperty {
  return {
    id: "p1",
    workspace_id: "ws-1",
    name: "Severity",
    type: "select",
    config: {},
    position: 0,
    archived: false,
    created_at: "",
    updated_at: "",
    ...over,
  };
}

describe("propertyAppliesToIssueType", () => {
  const cases: [string, string[] | undefined, string | null, boolean][] = [
    ["absent scope (a pre-F30 backend) is global", undefined, "bug", true],
    ["empty scope is global", [], "bug", true],
    ["empty scope reaches untyped issues too", [], null, true],
    ["scoped property on its own type", ["bug"], "bug", true],
    ["scoped property on another type", ["bug"], "story", false],
    ["scoped property never reaches an untyped issue", ["bug"], null, false],
    ["multi-scoped property on its second type", ["bug", "story"], "story", true],
  ];
  for (const [name, scope, issueType, want] of cases) {
    it(name, () => {
      expect(propertyAppliesToIssueType(scope, issueType)).toBe(want);
    });
  }
});

describe("formatHiddenPropertyValue", () => {
  it("resolves a select option to its name", () => {
    const p = property({
      config: { options: [{ id: "o1", name: "Critical", color: "#f00" }] },
    });
    expect(formatHiddenPropertyValue(p, "o1")).toBe("Critical");
  });

  it("joins a multi-select and keeps an option id it cannot resolve", () => {
    const p = property({
      type: "multi_select",
      config: { options: [{ id: "o1", name: "Critical", color: "#f00" }] },
    });
    expect(formatHiddenPropertyValue(p, ["o1", "o-gone"])).toBe("Critical, o-gone");
  });

  it("renders scalars and an empty value without throwing", () => {
    expect(formatHiddenPropertyValue(property({ type: "text" }), "hello")).toBe("hello");
    expect(formatHiddenPropertyValue(property({ type: "number" }), 42)).toBe("42");
    expect(formatHiddenPropertyValue(property({ type: "checkbox" }), true)).toBe("✓");
    expect(formatHiddenPropertyValue(property({ type: "checkbox" }), false)).toBe("—");
    expect(formatHiddenPropertyValue(property(), undefined)).toBe("");
  });
});
