// @vitest-environment jsdom

import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// The list row is a plain <div> whose click/auxclick handlers are a mouse-only
// convenience (ui/list-grid documents the split, and
// views/navigation/use-row-link repeats it): the keyboard path, "open in new
// tab" and the browser context menu belong to a real <AppLink> in the name
// cell. The squads list has no selection toggles, so the anchor is all there
// is to pin here.

const mocks = vi.hoisted(() => ({
  squads: [] as Squad[],
  viewState: {
    scope: "active",
    sortField: "created" as string,
    sortDirection: "desc" as string,
    hiddenColumns: [] as string[],
    filters: { leaders: [] as string[], creators: [] as string[] },
    setScope: vi.fn(),
    toggleSort: vi.fn(),
    setSortField: vi.fn(),
    setSortDirection: vi.fn(),
    toggleColumn: vi.fn(),
    toggleFilter: vi.fn(),
    clearFilters: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    if (options.queryKey?.[0] === "squads") {
      return {
        data: mocks.squads,
        isLoading: false,
        error: null,
        refetch: vi.fn(),
      };
    }
    return { data: [], isLoading: false, error: null, refetch: vi.fn() };
  },
  queryOptions: (options: unknown) => options,
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
  useQueryClient: () => ({ invalidateQueries: vi.fn(), setQueryData: vi.fn() }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

vi.mock("@multica/core/api", () => ({ api: { deleteSquad: vi.fn() } }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(
    (selector: (state: unknown) => unknown) => selector({ open: vi.fn() }),
    { getState: () => ({ open: vi.fn() }) },
  ),
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }),
  useWorkspacePaths: () => ({
    squadDetail: (id: string) => `/acme/squads/${id}`,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  squadListOptions: () => ({ queryKey: ["squads"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  workspaceKeys: { squads: (wsId: string) => ["squads", wsId] },
}));

vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (u: string | null) => u,
}));

vi.mock("@multica/core/squads/stores", () => ({
  useSquadsViewStore: (selector: (state: unknown) => unknown) =>
    selector(mocks.viewState),
  SQUAD_DEFAULT_HIDDEN_COLUMNS: [],
  SQUAD_SCOPES: ["active", "archived"],
}));

// View-layer children with heavy / portal deps — stubbed to keep the test on
// the row's link wiring.
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: () => null,
}));

import { SquadsPage } from "./squads-page";

const SQUAD: Squad = {
  id: "squad-1",
  workspace_id: "ws-1",
  name: "Release crew",
  description: "Ships the weekly build",
  instructions: "",
  avatar_url: null,
  leader_id: "agent-1",
  creator_id: "user-1",
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/squads",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderPage(adapter: NavigationAdapter = makeAdapter()) {
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <SquadsPage />
    </NavigationProvider>,
  );
}

function squadRow(): HTMLElement {
  return screen.getByRole("row", { name: /Release crew/ });
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.squads = [SQUAD];
});

describe("SquadsPage row title link", () => {
  it("renders the squad name as a real link", () => {
    renderPage();

    const link = within(squadRow()).getByRole("link", {
      name: "Release crew",
    });
    expect(link.tagName).toBe("A");
    expect(link).toHaveAttribute("href", "/acme/squads/squad-1");
  });

  it("navigates once from the keyboard when the title link is activated", async () => {
    const user = userEvent.setup();
    const push = vi.fn();
    renderPage(makeAdapter({ push }));

    const link = within(squadRow()).getByRole("link", { name: "Release crew" });
    link.focus();
    expect(link).toHaveFocus();
    await user.keyboard("{Enter}");

    expect(push).toHaveBeenCalledWith("/acme/squads/squad-1");
    expect(push).toHaveBeenCalledTimes(1);
  });

  // Web has no tab adapter, so AppLink leaves a modifier click to the browser.
  // If the event reached the row, rowLink would ALSO run its window.open
  // fallback and the user would get two tabs.
  it("leaves a modifier click on the title to the browser, without the row fallback", () => {
    const push = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderPage(makeAdapter({ push }));

    fireEvent.click(
      within(squadRow()).getByRole("link", { name: "Release crew" }),
      { metaKey: true },
    );

    expect(open).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
    open.mockRestore();
  });

  // Sanity check that the rig exercises the row's own handler at all —
  // otherwise the assertions above could pass vacuously.
  it("still navigates from the row surface outside the link", () => {
    const push = vi.fn();
    renderPage(makeAdapter({ push }));

    fireEvent.click(screen.getByText("Ships the weekly build"));

    expect(push).toHaveBeenCalledWith("/acme/squads/squad-1");
  });
});
