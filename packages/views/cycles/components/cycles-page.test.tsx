// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Cycle } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// Capacity math and status bucketing: packages/core/cycles/queries.test.ts.

const state = vi.hoisted(() => ({
  cycles: [] as Cycle[],
  created: [] as unknown[],
  deleteError: null as Error | null,
  toastError: vi.fn(),
  cyclesLoading: false,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ cycleDetail: (id: string) => `/acme/cycles/${id}` }),
}));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("@multica/core/properties/queries", () => ({ propertyListOptions: () => ({ queryKey: ["properties"] }) }));
vi.mock("sonner", () => ({ toast: { error: state.toastError } }));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) => {
    const key = o.queryKey?.[0];
    if (key === "cycles") return { data: state.cycles, isLoading: state.cyclesLoading, isPending: false };
    if (key === "projects") return { data: [{ id: "p1", title: "Billing" }], isLoading: false };
    if (key === "properties") return { data: [], isLoading: false };
    return { data: undefined, isLoading: false, isPending: true };
  },
}));
vi.mock("@multica/core/cycles", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/cycles")>();
  return {
    ...actual,
    cycleListOptions: () => ({ queryKey: ["cycles"] }),
    useCreateCycle: () => ({
      isPending: false,
      mutate: (data: unknown, o: { onSuccess: () => void }) => {
        state.created.push(data);
        o.onSuccess();
      },
    }),
    useUpdateCycle: () => ({ isPending: false, mutate: vi.fn() }),
    useDeleteCycle: () => ({
      isPending: false,
      mutate: (_id: string, o: { onError: (e: unknown) => void; onSettled: () => void }) => {
        if (state.deleteError) o.onError(state.deleteError);
        o.onSettled();
      },
    }),
  };
});

import { CyclesPage } from "./cycles-page";

const navigationAdapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/cycles",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (p: string) => p,
};

const renderPage = () =>
  renderWithI18n(
    <NavigationProvider value={navigationAdapter}>
      <CyclesPage />
    </NavigationProvider>,
  );

const cycle = (over: Partial<Cycle>): Cycle => ({
  id: "c", workspace_id: "ws-1", project_id: "p1", name: "Cycle", description: "",
  start_date: "2026-03-02", end_date: "2026-03-13", rollover: true, closed_at: null,
  status: "active", late: false, load_unit: "issues", load_property_id: null,
  issue_count: 0, done_count: 0,
  capacity: {
    human: { capacity: null, load: 0 },
    agent: { capacity: null, load: 0 },
    unassigned_load: 0,
  },
  created_at: "", updated_at: "", ...over,
});

beforeEach(() => {
  state.cycles = [];
  state.created = [];
  state.deleteError = null;
  state.toastError.mockReset();
  state.cyclesLoading = false;
});

// Base UI Select portals its popup onto document.body.
afterEach(() => cleanup());

async function pickOption(
  scope: { getByRole: typeof screen.getByRole; findByRole: typeof screen.findByRole },
  comboboxName: string,
  optionName: string,
) {
  const user = userEvent.setup();
  await user.click(scope.getByRole("combobox", { name: comboboxName }));
  await user.click(await screen.findByRole("option", { name: optionName }));
}

describe("CyclesPage", () => {
  it("groups cycles into active, upcoming and closed sections", () => {
    state.cycles = [
      cycle({ id: "closed", name: "Sprint 12", status: "closed" }),
      cycle({ id: "active", name: "Sprint 13", status: "active", issue_count: 4, done_count: 1 }),
      cycle({ id: "next", name: "Sprint 14", status: "upcoming" }),
    ];
    renderPage();

    expect(within(screen.getByTestId("cycle-section-active")).getByText("Sprint 13")).toBeInTheDocument();
    expect(within(screen.getByTestId("cycle-section-upcoming")).getByText("Sprint 14")).toBeInTheDocument();
    expect(within(screen.getByTestId("cycle-section-closed")).getByText("Sprint 12")).toBeInTheDocument();
    expect(screen.getByTestId("cycle-section-active").textContent).toContain("1 / 4 done");
  });

  it("badges an open cycle whose end date has passed as late", () => {
    // "Late" is the server's judgement (open past its end date); the page must
    // surface it rather than leave it indistinguishable from a healthy cycle.
    state.cycles = [cycle({ id: "late", name: "Sprint 9", status: "active", late: true })];
    renderPage();
    const row = screen.getByTestId("cycle-row");
    expect(row.textContent).toContain("Late");
  });

  it("names the cycle's project so a workspace-wide list is readable", () => {
    state.cycles = [cycle({ id: "c1", name: "Sprint 13" })];
    renderPage();
    expect(screen.getByTestId("cycle-row").textContent).toContain("Billing");
  });

  it("shows the empty state with no cycles", () => {
    renderPage();
    expect(screen.getByText("No cycles yet")).toBeInTheDocument();
  });

  // P3 audit finding: the empty-state branch required `!isLoading`, but
  // there was no isLoading branch of its own — so while the initial fetch
  // was in flight, cycles.length === 0 and isLoading === true fell through
  // both the error and empty checks, rendering the section list over an
  // empty array: a blank content area with no loading indicator at all.
  it("shows a loading indicator instead of a blank area while the fetch is in flight", () => {
    state.cyclesLoading = true;
    renderPage();
    expect(screen.getByRole("status")).toHaveTextContent("Loading cycles");
    expect(screen.queryByText("No cycles yet")).toBeNull();
  });

  it("creates a cycle with the project filter preselected", async () => {
    state.cycles = [cycle({ id: "c1" })];
    renderPage();
    await pickOption(screen, "Project", "Billing");
    fireEvent.click(screen.getByRole("button", { name: "New cycle" }));

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("combobox", { name: "Project" }).textContent).toContain("Billing");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Sprint 14" } });
    fireEvent.change(within(dialog).getByLabelText("Start date"), { target: { value: "2026-04-01" } });
    fireEvent.change(within(dialog).getByLabelText("End date"), { target: { value: "2026-04-14" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Create cycle" }));

    // An empty capacity field is "not declared", which must reach the server as
    // null — 0 would render as a cycle nobody can put any work into.
    expect(state.created[0]).toMatchObject({
      project_id: "p1",
      name: "Sprint 14",
      start_date: "2026-04-01",
      end_date: "2026-04-14",
      human_capacity: null,
      agent_capacity: null,
      load_property_id: null,
      rollover: true,
    });
  });

  it("refuses to submit an end date before the start date", async () => {
    // With a cycle present, the header action is the only "New cycle" button —
    // the empty state renders a second one.
    state.cycles = [cycle({ id: "c1" })];
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "New cycle" }));
    const dialog = await screen.findByRole("dialog");
    await pickOption(within(dialog), "Project", "Billing");
    fireEvent.change(within(dialog).getByLabelText("Name"), { target: { value: "Backwards" } });
    fireEvent.change(within(dialog).getByLabelText("Start date"), { target: { value: "2026-04-14" } });
    fireEvent.change(within(dialog).getByLabelText("End date"), { target: { value: "2026-04-01" } });

    expect(within(dialog).getByRole("button", { name: "Create cycle" })).toBeDisabled();
    expect(within(dialog).getByText(/end date must be on or after/i)).toBeInTheDocument();
    expect(state.created).toHaveLength(0);
  });

  it("surfaces the server's refusal when a delete fails", async () => {
    state.cycles = [cycle({ id: "c1", name: "Sprint 13" })];
    state.deleteError = new Error("cycle is referenced");
    renderPage();
    fireEvent.click(screen.getByRole("button", { name: "Delete cycle" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    expect(state.toastError).toHaveBeenCalledWith("cycle is referenced");
  });
});
