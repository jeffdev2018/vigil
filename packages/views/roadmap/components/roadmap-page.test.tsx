// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import type { Issue, IssueDependencyEdge, Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// Pure timeline/graph math: packages/core/roadmap (its own tests). The mocks
// below pin a deterministic layout so this suite only checks the page's
// wiring: states, tabs, links, and what reaches the graph.

const state = vi.hoisted(() => ({
  projects: [] as Project[],
  projectsLoading: false,
  projectsError: false,
  issues: [] as Issue[],
  issuesLoading: false,
  edges: [] as IssueDependencyEdge[],
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));
vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
}));
vi.mock("@multica/core/issues/queries", () => ({
  projectDependencyIssuesOptions: (_ws: string, projectId: string) => ({
    queryKey: ["project-issues", projectId],
  }),
}));
vi.mock("@multica/core/issues/dependency-edges", () => ({
  issueDependencyEdgesOptions: () => ({ queryKey: ["edges"] }),
}));
vi.mock("@multica/core/issues", () => ({
  issueStatusCategory: (issue: { status_category?: string; status: string }) =>
    issue.status_category ?? issue.status,
}));
vi.mock("@multica/core/roadmap", () => ({
  GRAPH_NODE_WIDTH: 220,
  GRAPH_NODE_HEIGHT: 64,
  // Mirrors the real module's semantics (packages/core/roadmap/timeline.ts):
  // a project with only one of start/due still gets a row, a project with
  // neither is unscheduled.
  buildRoadmapTimeline: (projects: Project[]) => {
    const rows = projects
      .filter((p) => p.start_date || p.due_date)
      .map((p) => ({
        project: p,
        startDate: new Date(`${p.start_date ?? p.due_date}T00:00:00Z`),
        dueDate: new Date(`${p.due_date ?? p.start_date}T00:00:00Z`),
        progress: p.issue_count > 0 ? p.done_count / p.issue_count : 0,
      }));
    return {
      rangeStart: new Date("2026-01-01T00:00:00Z"),
      rangeEnd: new Date("2026-06-30T00:00:00Z"),
      rows,
      unscheduled: projects.filter((p) => !p.start_date && !p.due_date),
    };
  },
  dateToOffset: () => 0.5,
  layoutDependencyGraph: (nodes: { id: string }[]) =>
    nodes.map((n, i) => ({ id: n.id, x: i * 260, y: 0 })),
}));
vi.mock("@xyflow/react", () => ({
  ReactFlow: ({
    nodes,
    edges,
  }: {
    nodes: { id: string; data: { identifier: string; title: string } }[];
    edges: { id: string; source: string; target: string; data?: { type: string } }[];
  }) => (
    <div data-testid="react-flow">
      {nodes.map((n) => (
        <div key={n.id} data-testid="flow-node">
          {n.data.identifier} {n.data.title}
        </div>
      ))}
      {edges.map((e) => (
        <div key={e.id} data-testid="flow-edge" data-edge-type={e.data?.type}>
          {e.source}→{e.target}
        </div>
      ))}
    </div>
  ),
  Background: () => null,
  Controls: () => null,
  MarkerType: { ArrowClosed: "arrowclosed" },
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (o: { queryKey?: readonly unknown[] }) => {
    const key = o.queryKey?.[0];
    if (key === "projects")
      return {
        data: state.projects,
        isLoading: state.projectsLoading,
        isError: state.projectsError,
      };
    if (key === "project-issues")
      return { data: state.issues, isLoading: state.issuesLoading };
    if (key === "edges") return { data: state.edges, isLoading: false };
    return { data: undefined, isLoading: false };
  },
}));

import { RoadmapPage } from "./roadmap-page";

const navigationAdapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/roadmap",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (p: string) => p,
};

const renderPage = () =>
  renderWithI18n(
    <NavigationProvider value={navigationAdapter}>
      <RoadmapPage />
    </NavigationProvider>,
  );

const project = (over: Partial<Project>): Project => ({
  id: "p",
  workspace_id: "ws-1",
  title: "Project",
  description: null,
  icon: null,
  status: "in_progress",
  priority: "none",
  lead_type: null,
  lead_id: null,
  start_date: "2026-01-05",
  due_date: "2026-02-13",
  created_at: "",
  updated_at: "",
  issue_count: 4,
  done_count: 1,
  resource_count: 0,
  ...over,
});

const issue = (over: Partial<Issue>): Issue =>
  ({
    id: "i",
    identifier: "MUL-1",
    title: "Issue",
    status: "todo",
    status_category: "todo",
    ...over,
  }) as unknown as Issue;

beforeEach(() => {
  state.projects = [];
  state.projectsLoading = false;
  state.projectsError = false;
  state.issues = [];
  state.issuesLoading = false;
  state.edges = [];
});

// Base UI Select portals its popup onto document.body.
afterEach(() => cleanup());

describe("RoadmapPage", () => {
  it("shows the empty state with no projects", () => {
    renderPage();
    expect(screen.getByText("No projects yet")).toBeInTheDocument();
  });

  it("shows a loading indicator while projects load", () => {
    state.projectsLoading = true;
    renderPage();
    expect(screen.getByRole("status")).toHaveTextContent("Loading roadmap");
    expect(screen.queryByText("No projects yet")).toBeNull();
  });

  it("renders a bar per scheduled project and lists the unscheduled ones below", () => {
    state.projects = [
      project({ id: "p1", title: "Apollo" }),
      project({ id: "p2", title: "Gemini", start_date: null, due_date: null }),
    ];
    renderPage();

    const row = screen.getByTestId("project-timeline-row");
    expect(row).toHaveTextContent("Apollo");
    expect(row).toHaveTextContent("25% done");
    expect(
      within(row).getByRole("link", { name: "Apollo" }),
    ).toHaveAttribute("href", "/acme/projects/p1");

    const unscheduled = screen.getByTestId("roadmap-unscheduled");
    expect(unscheduled).toHaveTextContent("Gemini");
    expect(within(unscheduled).getByRole("link", { name: "Gemini" })).toHaveAttribute(
      "href",
      "/acme/projects/p2",
    );
  });

  it("renders a bar for a project with only one date (one-day marker)", () => {
    state.projects = [project({ id: "p1", title: "Apollo", start_date: null })];
    renderPage();
    expect(screen.getByTestId("project-timeline-row")).toHaveTextContent("Apollo");
    expect(screen.queryByText("Nothing scheduled")).toBeNull();
  });

  it("shows the unscheduled-only state when no project has any date", () => {
    state.projects = [
      project({ id: "p1", title: "Apollo", start_date: null, due_date: null }),
    ];
    renderPage();
    expect(screen.getByText("Nothing scheduled")).toBeInTheDocument();
    expect(screen.getByTestId("roadmap-unscheduled")).toHaveTextContent("Apollo");
  });

  it("renders the dependency graph of the first project on the graph tab", () => {
    state.projects = [project({ id: "p1", title: "Apollo" })];
    state.issues = [
      issue({ id: "i1", identifier: "MUL-1", title: "Design" }),
      issue({ id: "i2", identifier: "MUL-2", title: "Build" }),
    ];
    state.edges = [{ id: "e1", from: "i1", to: "i2", type: "blocks" }];
    renderPage();

    fireEvent.click(screen.getByRole("tab", { name: "Dependencies" }));

    const graph = screen.getByTestId("react-flow");
    expect(within(graph).getByText("MUL-1 Design")).toBeInTheDocument();
    expect(within(graph).getByText("MUL-2 Build")).toBeInTheDocument();
    expect(screen.getByTestId("flow-edge")).toHaveTextContent("i1→i2");
  });

  it("shows the issues empty state when the selected project has none", () => {
    state.projects = [project({ id: "p1", title: "Apollo" })];
    state.issues = [];
    renderPage();

    fireEvent.click(screen.getByRole("tab", { name: "Dependencies" }));

    expect(
      screen.getByText("This project has no scheduled issues"),
    ).toBeInTheDocument();
  });
});
