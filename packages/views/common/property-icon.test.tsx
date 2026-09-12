// @vitest-environment jsdom
/**
 * PropertyIconPicker's 35 icon buttons (#audit2).
 *
 * PROPERTY_ICON_OPTIONS.label was a hardcoded-English literal ("Status",
 * "Bug", "Lightning", ...) rendered directly as aria-label/title and as the
 * selected-icon caption, with no useT() in the file at all — every
 * non-English workspace saw the English catalog seed in the icon picker.
 * Labels now resolve through useT("common")'s property_icons.<value> keys.
 */
import { describe, expect, it } from "vitest";
import { screen } from "@testing-library/react";
import { renderWithI18n } from "../test/i18n";
import { PropertyIconPicker } from "./property-icon";

function renderPicker(value: string, locale: "en" | "fr" | "zh-Hans" = "en") {
  return renderWithI18n(
    <PropertyIconPicker
      value={value}
      label="Icon"
      removeLabel="Remove"
      onSelect={() => {}}
      onRemove={() => {}}
    />,
    { locale },
  );
}

describe("PropertyIconPicker", () => {
  it("labels every icon button in English by default", () => {
    renderPicker("");
    expect(screen.getByRole("button", { name: "Status" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Bug" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Lightning" })).toBeInTheDocument();
  });

  it("translates the icon button labels for a non-English locale", () => {
    renderPicker("", "fr");
    expect(screen.getByRole("button", { name: "Statut" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Bug" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Éclair" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Status" })).not.toBeInTheDocument();
  });

  it("shows the selected icon's localized name as the caption", () => {
    renderPicker("circle-dot", "zh-Hans");
    expect(screen.getByText("状态")).toBeInTheDocument();
  });
});
