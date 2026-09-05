// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueDependencies } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueDependenciesSection } from "./issue-dependencies-section";

// Schema and API fallbacks are covered in packages/core/issues/dependencies.test.ts.

const state = vi.hoisted(() => ({
  data: { blocks: [], blocked_by: [], related: [] } as IssueDependencies,
  remove: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/ws/issues/${id}` }),
}));
vi.mock("@multica/core/issues/dependencies", () => ({
  issueDependenciesOptions: (wsId: string, issueId: string) => ({
    queryKey: ["issues", wsId, "dependencies", issueId],
    queryFn: async () => state.data,
  }),
  useRemoveIssueDependency: () => ({ mutate: state.remove }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => (
    <a href={href} className={className}>{children}</a>
  ),
}));
vi.mock("./status-icon", () => ({ StatusIcon: () => null }));

function issue(id: string, title: string, status = "todo") {
  return {
    id, title, status,
    workspace_id: "ws-1", number: 1, identifier: `MUL-${id}`, description: null,
    priority: "none", assignee_type: null, assignee_id: null, creator_type: "member",
    creator_id: "u", parent_issue_id: null, project_id: null, position: 0,
    start_date: null, due_date: null, created_at: "", updated_at: "",
  } as IssueDependencies["blocks"][number]["issue"];
}

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <IssueDependenciesSection issueId="a" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = { blocks: [], blocked_by: [], related: [] };
  state.remove.mockReset();
});

describe("IssueDependenciesSection", () => {
  it("renders nothing when the issue has no dependencies", async () => {
    const { container } = renderSection();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("lists both directions and greys out a finished blocker", async () => {
    state.data = {
      blocks: [{ id: "d1", type: "blocks", issue: issue("b", "Downstream") }],
      blocked_by: [{ id: "d2", type: "blocked_by", issue: issue("c", "Upstream", "done") }],
      related: [],
    };
    renderSection();
    expect(await screen.findByText("Downstream")).toBeTruthy();
    expect(screen.getByText("Blocks")).toBeTruthy();
    expect(screen.getByText("Blocked by")).toBeTruthy();
    const upstream = screen.getByText("Upstream").closest("[data-done]");
    expect(upstream?.getAttribute("data-done")).toBe("true");
    expect(screen.getByText("Downstream").closest("[data-done]")).toBeNull();
    expect(screen.getByText("Upstream").closest("a")?.getAttribute("href")).toBe("/ws/issues/c");
  });

  it("removes a dependency from the row's button", async () => {
    state.data = {
      blocks: [{ id: "d1", type: "blocks", issue: issue("b", "Downstream") }],
      blocked_by: [],
      related: [],
    };
    renderSection();
    await screen.findByText("Downstream");
    fireEvent.click(screen.getByRole("button", { name: "Remove dependency" }));
    expect(state.remove).toHaveBeenCalledWith({ issueId: "a", dependencyId: "d1" });
  });
});
