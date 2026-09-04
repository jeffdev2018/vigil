// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { DecisionRecord } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Client parsing and keys: packages/core/projects/decisions.test.ts.

const state = vi.hoisted(() => ({ decisions: [] as DecisionRecord[], authors: [] as string[] }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/ws/issues/${id}` }) }));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...rest }: { href: string; children: React.ReactNode; className?: string }) => <a href={href} {...rest}>{children}</a>,
}));
vi.mock("@multica/core/projects/decisions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/projects/decisions")>()),
  projectDecisionsOptions: (wsId: string, projectId: string, author: string) => {
    state.authors.push(author);
    return {
      queryKey: ["decisions", wsId, projectId, author],
      queryFn: async () => state.decisions.filter((d) => !author || d.author_type === author),
    };
  },
}));

import { ProjectDecisionsSection } from "./project-decisions-section";

const record = (over: Partial<DecisionRecord> = {}): DecisionRecord => ({
  id: "d1", workspace_id: "ws-1", project_id: "p1", issue_id: "i1", issue_identifier: "JEFF-7", issue_title: "Denormalize",
  run_id: "r1", source_message_seq: 4, title: "Keep the table denormalized", context: "One read path", decision: "No join",
  consequences: null, author_type: "agent", author_id: "a1", created_at: "2026-09-04T00:00:00Z", ...over,
});

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ProjectDecisionsSection projectId="p1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.decisions = [];
  state.authors = [];
});

describe("ProjectDecisionsSection", () => {
  it("says the project has no decision yet", async () => {
    renderSection();
    expect(await screen.findByTestId("project-decisions-empty")).toBeTruthy();
  });

  it("lists decisions with their issue link and source, and filters by author", async () => {
    state.decisions = [record(), record({ id: "d2", title: "Manual note", author_type: "member" })];
    renderSection();
    const rows = await screen.findAllByTestId("decision-record");
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain("Keep the table denormalized");
    expect(rows[0]?.textContent).toContain("message #4");
    expect(screen.getAllByRole("link", { name: "JEFF-7" })[0]?.getAttribute("href")).toBe("/ws/issues/i1");
    fireEvent.change(screen.getByLabelText("Author"), { target: { value: "member" } });
    expect(await screen.findAllByTestId("decision-record")).toHaveLength(1);
    expect(state.authors.at(-1)).toBe("member");
  });
});
