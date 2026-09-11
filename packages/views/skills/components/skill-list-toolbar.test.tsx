// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { SkillListToolbar } from "./skill-list-toolbar";

describe("SkillListToolbar", () => {
  // Regression: the clear control was a span nested in the filter trigger's
  // native <button>, so no keyboard user could reach it. The same structure is
  // shared by the agents, autopilots, projects and squads toolbars.
  it("offers clear-filters as its own focusable button", () => {
    const onClearFilters = vi.fn();
    renderWithI18n(
      <SkillListToolbar
        search=""
        onSearchChange={vi.fn()}
        filters={{ usage: ["used"], origins: [], agents: [], creators: [] }}
        onToggleFilter={vi.fn()}
        onClearFilters={onClearFilters}
        sortField="name"
        sortDirection="asc"
        onSortFieldChange={vi.fn()}
        onSortDirectionChange={vi.fn()}
        hiddenColumns={[]}
        onToggleColumn={vi.fn()}
        allRows={[]}
        visibleCount={0}
      />,
    );

    const clear = screen.getByRole("button", { name: "Clear filters" });
    expect(clear.parentElement?.closest("button")).toBeNull();
    clear.focus();
    expect(clear).toHaveFocus();
    fireEvent.click(clear);
    expect(onClearFilters).toHaveBeenCalledTimes(1);
  });
});
