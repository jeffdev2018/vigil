// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { IssueDependencies } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueDependenciesSection } from "./issue-dependencies-section";

// Schema and API fallbacks are covered in packages/core/issues/dependencies.test.ts.

const state = vi.hoisted(() => ({
  data: { blocks: [], blocked_by: [], related: [], duplicate: [] } as IssueDependencies,
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
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

import { toast } from "sonner";

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
  state.data = { blocks: [], blocked_by: [], related: [], duplicate: [] };
  state.remove.mockReset();
  vi.mocked(toast.error).mockClear();
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
      duplicate: [],
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

  it("lists related and duplicate relations under their own headings (R01)", async () => {
    state.data = {
      blocks: [],
      blocked_by: [],
      related: [{ id: "d3", type: "related", issue: issue("r", "Sibling work") }],
      duplicate: [{ id: "d4", type: "duplicate", issue: issue("dupe", "Same ask, filed twice") }],
    };
    renderSection();
    expect(await screen.findByText("Sibling work")).toBeTruthy();
    expect(screen.getByText("Related")).toBeTruthy();
    expect(screen.getByText("Same ask, filed twice")).toBeTruthy();
    expect(screen.getByText("Duplicates")).toBeTruthy();
  });

  it("removes a dependency from the row's button", async () => {
    state.data = {
      blocks: [{ id: "d1", type: "blocks", issue: issue("b", "Downstream") }],
      blocked_by: [],
      related: [],
      duplicate: [],
    };
    renderSection();
    await screen.findByText("Downstream");
    fireEvent.click(screen.getByRole("button", { name: "Remove dependency" }));
    expect(state.remove.mock.calls[0]?.[0]).toEqual({ issueId: "a", dependencyId: "d1" });
  });

  // Regression: useRemoveIssueDependency had no onError anywhere in this
  // component — a failed removal resynced silently on the next invalidate.
  it("shows a toast when removing a dependency fails", async () => {
    state.data = {
      blocks: [{ id: "d1", type: "blocks", issue: issue("b", "Downstream") }],
      blocked_by: [],
      related: [],
      duplicate: [],
    };
    state.remove.mockImplementation((_vars, opts?: { onError?: (err: unknown) => void }) => {
      opts?.onError?.(new Error("could not remove"));
    });
    renderSection();
    await screen.findByText("Downstream");

    fireEvent.click(screen.getByRole("button", { name: "Remove dependency" }));

    expect(toast.error).toHaveBeenCalledWith("could not remove");
  });
});
