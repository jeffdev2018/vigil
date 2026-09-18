// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Autopilot } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// The compact list row is a plain <div> whose click/auxclick handlers are a
// mouse-only convenience (ui/list-grid documents the split, and
// views/navigation/use-row-link repeats it): the keyboard path, "open in new
// tab" and the browser context menu belong to a real <AppLink> in the name
// cell. This rig pins that anchor and the selection toggles' accessible names.

const mocks = vi.hoisted(() => ({
  autopilots: [] as Autopilot[],
  viewState: {
    scope: "all",
    sortField: "created" as string,
    sortDirection: "desc" as string,
    hiddenColumns: [] as string[],
    filters: {
      assignees: [] as string[],
      modes: [] as string[],
      triggerKinds: [] as string[],
      creators: [] as string[],
    },
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
    if (options.queryKey?.[0] === "autopilots") {
      return {
        data: mocks.autopilots,
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

// Render every row so the anchor under test is always mounted.
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: index,
        start: index * 48,
        end: (index + 1) * 48,
        size: 48,
      })),
    getTotalSize: () => count * 48,
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    autopilotDetail: (id: string) => `/acme/autopilots/${id}`,
  }),
}));

vi.mock("@multica/core/autopilots/queries", () => ({
  autopilotListOptions: () => ({ queryKey: ["autopilots"] }),
}));

vi.mock("@multica/core/autopilots/stores", () => ({
  useAutopilotsViewStore: (selector: (state: unknown) => unknown) =>
    selector(mocks.viewState),
  AUTOPILOT_DEFAULT_HIDDEN_COLUMNS: [],
  AUTOPILOT_SCOPES: ["all", "active", "paused"],
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Ada" }),
}));

// View-layer children with heavy / portal deps — stubbed to keep the test on
// the row's link and toggle wiring.
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("./autopilot-dialog", () => ({ AutopilotDialog: () => null }));
vi.mock("./autopilot-draft-dialog", () => ({
  AutopilotDraftDialog: () => null,
}));
vi.mock("./autopilot-list-toolbar", () => ({
  AutopilotListToolbar: () => null,
  actorFilterValue: (type: string, id: string) => `${type}:${id}`,
}));
vi.mock("./autopilot-list-actions", () => ({
  AutopilotBatchToolbar: () => null,
  AutopilotRowActions: () => null,
}));

import { AutopilotsPage } from "./autopilots-page";

const AUTOPILOT: Autopilot = {
  id: "ap-1",
  workspace_id: "ws-1",
  title: "Nightly triage",
  description: null,
  assignee_type: "agent",
  assignee_id: "agent-1",
  status: "active",
  execution_mode: "create_issue",
  issue_title_template: null,
  created_by_type: "member",
  created_by_id: "user-1",
  last_run_at: null,
  created_at: "2026-07-01T00:00:00Z",
  updated_at: "2026-07-01T00:00:00Z",
};

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/autopilots",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderPage(adapter: NavigationAdapter = makeAdapter()) {
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <AutopilotsPage />
    </NavigationProvider>,
  );
}

function autopilotRow(): HTMLElement {
  return screen.getByRole("row", { name: /Nightly triage/ });
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.autopilots = [AUTOPILOT];
});

describe("AutopilotsPage row title link", () => {
  it("renders the automation title as a real link", () => {
    renderPage();

    const link = within(autopilotRow()).getByRole("link", {
      name: "Nightly triage",
    });
    expect(link.tagName).toBe("A");
    expect(link).toHaveAttribute("href", "/acme/autopilots/ap-1");
  });

  it("navigates once from the keyboard when the title link is activated", async () => {
    const user = userEvent.setup();
    const push = vi.fn();
    renderPage(makeAdapter({ push }));

    const link = within(autopilotRow()).getByRole("link", {
      name: "Nightly triage",
    });
    link.focus();
    expect(link).toHaveFocus();
    await user.keyboard("{Enter}");

    expect(push).toHaveBeenCalledWith("/acme/autopilots/ap-1");
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
      within(autopilotRow()).getByRole("link", { name: "Nightly triage" }),
      { metaKey: true },
    );

    expect(open).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
    open.mockRestore();
  });

  it("names the selection toggles and reveals them on focus", async () => {
    const user = userEvent.setup();
    renderPage();

    const rowToggle = within(autopilotRow()).getByRole("button", {
      name: "Select Nightly triage",
    });
    // Both are hidden by `opacity-0` while nothing is selected, so focus has
    // to reveal them too or the keyboard lands on an invisible control.
    expect(rowToggle.className).toContain("focus-visible:opacity-100");
    expect(
      screen.getByRole("button", { name: "Select all automations" }).className,
    ).toContain("focus-visible:opacity-100");

    rowToggle.focus();
    expect(rowToggle).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(rowToggle).toHaveAttribute("aria-pressed", "true");
  });
});
