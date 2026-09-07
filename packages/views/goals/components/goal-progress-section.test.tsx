// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { GoalProgress } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// A goal used as a cross-project initiative (F29): the aggregate must name
// which projects it is made of, and an initiative nothing serves must say so
// with a way out rather than rendering an empty bar.

const state = vi.hoisted(() => ({ progress: null as GoalProgress | null, pending: false }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projects: () => "/acme/projects",
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: state.progress, isPending: state.pending }),
}));
vi.mock("@multica/core/cycles", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/cycles")>()),
  goalProgressOptions: () => ({ queryKey: ["goal-progress"] }),
}));

import { GoalProgressSection } from "./goal-progress-section";

const navigationAdapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/goals",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (p: string) => p,
};

const renderSection = () =>
  renderWithI18n(
    <NavigationProvider value={navigationAdapter}>
      <GoalProgressSection goalId="g1" />
    </NavigationProvider>,
  );

beforeEach(() => {
  state.pending = false;
  state.progress = {
    goal_id: "g1",
    projects: [
      { project_id: "p1", name: "Billing", total_count: 4, done_count: 1 },
      { project_id: "p2", name: "Mobile", total_count: 2, done_count: 2 },
    ],
    total_count: 6,
    done_count: 3,
  };
});

describe("GoalProgressSection", () => {
  it("lists every linked project with its own progress and the aggregate", () => {
    renderSection();
    const section = screen.getByTestId("goal-progress-section");
    expect(section.textContent).toContain("Billing");
    expect(section.textContent).toContain("1 / 4 done");
    expect(section.textContent).toContain("Mobile");
    expect(section.textContent).toContain("2 / 2 done");
    expect(section.textContent).toContain("3 / 6 done");
  });

  it("gives each project a bar of its own", () => {
    renderSection();
    const bars = screen.getAllByRole("progressbar");
    expect(bars.map((b) => b.getAttribute("aria-valuenow"))).toEqual(["25", "100"]);
  });

  it("offers a way to link a project when the goal aggregates nothing", () => {
    state.progress = { goal_id: "g1", projects: [], total_count: 0, done_count: 0 };
    renderSection();
    expect(screen.getByText("No project serves this goal yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Link a project" })).toHaveAttribute(
      "href",
      "/acme/projects",
    );
  });
});
