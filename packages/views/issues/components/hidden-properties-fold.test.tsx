// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import type { IssueProperty } from "@multica/core/types";
import en from "../../locales/en/issues.json";
import { renderWithI18n } from "../../test/i18n";
import { HiddenPropertiesFold } from "./issue-detail";

// The applicability RULE and the value formatting live in
// issue-detail-property-scope.test.ts. This is the fold's own contract:
// acceptance 6 — out-of-scope values are visible, read-only and never lost.

function property(id: string, name: string): IssueProperty {
  return {
    id,
    workspace_id: "ws-1",
    name,
    type: "text",
    config: {},
    position: 0,
    archived: false,
    created_at: "",
    updated_at: "",
  };
}

afterEach(cleanup);

describe("HiddenPropertiesFold", () => {
  it("renders nothing when no value fell out of scope", () => {
    const { container } = renderWithI18n(
      <HiddenPropertiesFold properties={[]} values={{}} open onToggle={vi.fn()} />,
    );
    expect(container.textContent).toBe("");
  });

  it("counts the folded values without showing them", () => {
    renderWithI18n(
      <HiddenPropertiesFold
        properties={[property("p1", "Severity"), property("p2", "Repro steps")]}
        values={{ p1: "Critical", p2: "Open the app" }}
        open={false}
        onToggle={vi.fn()}
      />,
    );
    expect(
      screen.getByText(en.detail.hidden_properties_other.replace("{{count}}", "2")),
    ).toBeTruthy();
    expect(screen.queryByText("Severity")).toBeNull();
  });

  it("shows the values, read-only, once expanded", () => {
    renderWithI18n(
      <HiddenPropertiesFold
        properties={[property("p1", "Severity")]}
        values={{ p1: "Critical" }}
        open
        onToggle={vi.fn()}
      />,
    );
    expect(screen.getByText("Severity")).toBeTruthy();
    expect(screen.getByText("Critical")).toBeTruthy();
    // Read-only: no control offers a write the server would refuse with
    // property_not_applicable.
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.getByText(en.detail.hidden_properties_hint)).toBeTruthy();
  });

  it("toggles through the disclosure button", () => {
    const onToggle = vi.fn();
    renderWithI18n(
      <HiddenPropertiesFold
        properties={[property("p1", "Severity")]}
        values={{ p1: "Critical" }}
        open={false}
        onToggle={onToggle}
      />,
    );
    const button = screen.getByRole("button");
    expect(button.getAttribute("aria-expanded")).toBe("false");
    fireEvent.click(button);
    expect(onToggle).toHaveBeenCalledTimes(1);
  });
});
