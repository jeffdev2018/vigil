// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PRStack, PRStackNode } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  data: { nodes: [], truncated: false, cyclic: false } as PRStack,
}));

vi.mock("@multica/core/github/queries", () => ({
  issuePRStackOptions: (issueId: string) => ({
    queryKey: ["github", "pr-stack", issueId],
    queryFn: async () => state.data,
    enabled: !!issueId,
  }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/ws/issues/${id}` }),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => (
    <a href={href} className={className}>{children}</a>
  ),
}));

import { PRStackList } from "./pr-stack-list";

function node(id: string, depth: number, ready = false): PRStackNode {
  return { issue_id: id, identifier: `MUL-${id}`, title: `Issue ${id}`, status: "todo", depth, prs: [], ready };
}

function renderStack() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PRStackList issueId="a" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = { nodes: [], truncated: false, cyclic: false };
});

describe("PRStackList", () => {
  it("renders nothing when the issue has no blockers", async () => {
    state.data = { nodes: [node("a", 0)], truncated: false, cyclic: false };
    const { container } = renderStack();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("indents each level by depth and links every issue", async () => {
    state.data = { nodes: [node("a", 0), node("b", 1, true), node("c", 2)], truncated: false, cyclic: false };
    renderStack();
    expect(await screen.findByText("Issue c")).toBeTruthy();
    const row = screen.getByText("Issue c").closest("li");
    expect(row?.getAttribute("data-depth")).toBe("2");
    expect(row?.style.paddingLeft).toBe("24px");
    expect(screen.getByText("Issue b").closest("a")?.getAttribute("href")).toBe("/ws/issues/b");
  });

  it("announces truncation and cycles", async () => {
    state.data = { nodes: [node("a", 0), node("b", 1)], truncated: true, cyclic: true };
    renderStack();
    expect(await screen.findByText("Stack cut after 1 levels")).toBeTruthy();
    expect(screen.getByText(/Dependency cycle/)).toBeTruthy();
  });
});
