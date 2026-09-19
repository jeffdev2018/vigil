// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// The toolbar reads the workspace label catalog for its Labels dimension.
// That is a server round-trip of its own, tested where the query lives; here
// it only has to not be the reason the toolbar cannot render.
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/labels/queries", () => ({
  labelListOptions: (wsId: string, resourceType: string) => ({
    queryKey: ["labels", wsId, resourceType],
    queryFn: async () => [],
  }),
}));

import { SkillListToolbar } from "./skill-list-toolbar";

describe("SkillListToolbar", () => {
  // Regression: the clear control was a span nested in the filter trigger's
  // native <button>, so no keyboard user could reach it. The same structure is
  // shared by the agents, autopilots, projects and squads toolbars.
  it("offers clear-filters as its own focusable button", () => {
    const onClearFilters = vi.fn();
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
      <SkillListToolbar
        search=""
        onSearchChange={vi.fn()}
        filters={{ usage: ["used"], origins: [], agents: [], creators: [], labels: [] }}
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
      />
      </QueryClientProvider>,
    );

    const clear = screen.getByRole("button", { name: "Clear filters" });
    expect(clear.parentElement?.closest("button")).toBeNull();
    clear.focus();
    expect(clear).toHaveFocus();
    fireEvent.click(clear);
    expect(onClearFilters).toHaveBeenCalledTimes(1);
  });
});
