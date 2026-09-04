// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Command as CommandPrimitive } from "cmdk";
import type { WhySearchResult } from "@multica/core/search/why";
import { renderWithI18n } from "../test/i18n";

// Query gating and parsing: packages/core/search/why.test.ts.

const state = vi.hoisted(() => ({ results: [] as WhySearchResult[], pushed: [] as string[], closed: 0 }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/w/issues/${id}` }) }));
vi.mock("../navigation", () => ({ useNavigation: () => ({ push: (p: string) => state.pushed.push(p) }) }));
vi.mock("@multica/core/search/why", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/search/why")>()),
  whySearchOptions: (wsId: string, q: string) => ({
    queryKey: ["search", wsId, "why", q],
    queryFn: async () => ({ results: state.results, query: q }),
    enabled: q.trim().length >= 3 && (/\s/.test(q.trim()) || q.trim().endsWith("?")),
  }),
}));

import { WhySearchGroup } from "./why-search-group";

const result = (over: Partial<WhySearchResult> = {}): WhySearchResult => ({
  id: "c1", source_type: "comment", source_id: "s1", issue_id: "i1", issue_identifier: "JEFF-3", issue_title: "Router choice",
  snippet: "We picked <b>Chi</b> over <b>Gin</b>", score: 0.3, created_at: "", ...over,
});

function render(query: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CommandPrimitive shouldFilter={false}>
        <CommandPrimitive.List>
          <WhySearchGroup query={query} groupClassName="g" onNavigated={() => state.closed++} />
        </CommandPrimitive.List>
      </CommandPrimitive>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.results = [];
  state.pushed = [];
  state.closed = 0;
});

describe("WhySearchGroup", () => {
  it("stays silent for a single word", async () => {
    const { container } = render("auth");
    await new Promise((r) => setTimeout(r, 0));
    expect(container.querySelector("[cmdk-group]")).toBeNull();
  });

  it("shows sources with a snippet and opens the issue on select", async () => {
    state.results = [result(), result({ id: "m1", source_type: "task_message", issue_id: "i2", issue_identifier: "JEFF-4" })];
    render("why chi over gin");
    const rows = await screen.findAllByTestId("why-result");
    expect(rows).toHaveLength(2);
    expect(rows[0]?.textContent).toContain("We picked Chi over Gin");
    expect(rows[0]?.textContent).toContain("Comment · JEFF-3");
    expect(rows[1]?.textContent).toContain("Run message");
    fireEvent.click(rows[0]!);
    expect(state.pushed).toEqual(["/w/issues/i1"]);
    expect(state.closed).toBe(1);
  });

  it("says when nothing relevant was found", async () => {
    render("why did we drop redis?");
    expect(await screen.findByTestId("why-empty")).toBeTruthy();
  });
});
