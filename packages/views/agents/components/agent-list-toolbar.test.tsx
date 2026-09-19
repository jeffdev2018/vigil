// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import type { RuntimeMachine } from "../../runtimes/components/runtime-machines";
import type { AgentListRow } from "./agents-page";
import { AgentListToolbar } from "./agent-list-toolbar";

// The dropdown's own behaviour (grouping, badges, empty state) is pinned in
// runtime-machine-filter-dropdown.test.tsx. This file asserts the one thing
// that file structurally cannot: the toolbar RENDERS it.
//
// That is the regression. `RuntimeMachineFilterDropdown` shipped mounted, then
// "rebuild all six list surfaces on a shared Linear-style list grid" replaced
// the header that rendered it. Its nine tests stayed green for months while no
// user could reach the control.

function machine(over: Partial<RuntimeMachine> = {}): RuntimeMachine {
  return {
    id: "m-local",
    daemonId: "daemon-1",
    title: "dev.local",
    subtitle: null,
    deviceInfo: null,
    cliVersion: null,
    launchedBy: null,
    mode: "local",
    section: "local",
    isCurrent: true,
    health: "online",
    runtimes: [],
    onlineCount: 1,
    issueCount: 0,
    runningCount: 0,
    queuedCount: 0,
    providerNames: [],
    lastSeenAt: null,
    ...over,
  };
}

function renderToolbar(over: Partial<React.ComponentProps<typeof AgentListToolbar>> = {}) {
  const onRuntimeMachineChange = vi.fn();
  renderWithI18n(
    <AgentListToolbar
      scope="all"
      onScopeChange={vi.fn()}
      scopeCounts={{ mine: 1, all: 3, archived: 0 }}
      search=""
      onSearchChange={vi.fn()}
      filters={{ availability: [], runtimes: [], owners: [], models: [], access: [] }}
      onToggleFilter={vi.fn()}
      onClearFilters={vi.fn()}
      sortField="lastActive"
      sortDirection="desc"
      onSortFieldChange={vi.fn()}
      onSortDirectionChange={vi.fn()}
      hiddenColumns={[]}
      onToggleColumn={vi.fn()}
      allRows={[] as AgentListRow[]}
      members={[]}
      visibleCount={0}
      machines={[machine()]}
      runtimeMachineId={null}
      onRuntimeMachineChange={onRuntimeMachineChange}
      agentCountByMachine={new Map([["m-local", 2]])}
      totalAgentCount={3}
      {...over}
    />,
  );
  return { onRuntimeMachineChange };
}

describe("AgentListToolbar", () => {
  it("renders the runtime machine filter", () => {
    renderToolbar();
    const trigger = screen.getByTestId("agents-runtime-filter");
    expect(trigger).toBeTruthy();
    // Unselected: the "All runtimes" label and the scope total, NOT the
    // per-machine sum — the page passes scopeRows.length here.
    expect(trigger.textContent).toContain("All runtimes");
    expect(trigger.textContent).toContain("3");
  });

  it("names the selected machine and reports a pick to the page", () => {
    const { onRuntimeMachineChange } = renderToolbar({ runtimeMachineId: "m-local" });
    expect(screen.getByTestId("agents-runtime-filter").textContent).toContain("dev.local");

    fireEvent.click(screen.getByTestId("agents-runtime-filter"));
    fireEvent.click(screen.getByRole("menuitem", { name: /All runtimes/ }));
    expect(onRuntimeMachineChange).toHaveBeenCalledWith(null);
  });

  // Below `md` the display trigger collapses to its direction icon; the name
  // must not collapse with it.
  it("names the display trigger for assistive tech", () => {
    renderToolbar();
    expect(screen.getByRole("button", { name: "Display" })).toBeTruthy();
  });
});
