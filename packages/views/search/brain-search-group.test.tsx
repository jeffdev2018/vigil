// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Command as CommandPrimitive } from "cmdk";
import type { WorkspaceNoteSearchHit } from "@multica/core/types";
import { renderWithI18n } from "../test/i18n";

// Snippet escaping: packages/core/brain/snippet.test.ts.

const state = vi.hoisted(() => ({
  notes: [] as WorkspaceNoteSearchHit[],
  pushed: [] as string[],
  closed: 0,
  captured: [] as unknown[],
  captureFails: false,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ brain: () => "/w/brain" }),
}));
vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: (p: string) => state.pushed.push(p) }),
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/brain/queries", () => ({
  noteSearchOptions: (wsId: string, q: string) => ({
    queryKey: ["brain", wsId, "search", q],
    enabled: q.trim() !== "",
    queryFn: async () => ({ notes: state.notes, vector: false }),
  }),
}));
vi.mock("@multica/core/brain/mutations", () => ({
  useCaptureText: () => ({
    mutateAsync: async (input: unknown) => {
      if (state.captureFails) throw new Error("boom");
      state.captured.push(input);
      return {};
    },
    isPending: false,
  }),
}));

import { BrainSearchGroup } from "./brain-search-group";

const hit = (over: Partial<WorkspaceNoteSearchHit> = {}): WorkspaceNoteSearchHit => ({
  id: "note-1",
  workspace_id: "ws-1",
  title: "Deploys go through the release tag",
  content: "Push v0.x.x on main.",
  tags: [],
  source: "manual",
  pinned: false,
  created_by_type: "member",
  revision: 1,
  created_at: "",
  updated_at: "",
  score: 0.5,
  snippet: "push <mark>v0.x.x</mark> on main",
  lex_rank: 1,
  vec_rank: null,
  ...over,
});

function render(query: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CommandPrimitive shouldFilter={false}>
        <CommandPrimitive.List>
          <BrainSearchGroup
            query={query}
            groupClassName="g"
            onNavigated={() => state.closed++}
          />
        </CommandPrimitive.List>
      </CommandPrimitive>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.notes = [];
  state.pushed = [];
  state.closed = 0;
  state.captured = [];
  state.captureFails = false;
});

describe("BrainSearchGroup", () => {
  it("stays silent with no query", async () => {
    const { container } = render("   ");
    await new Promise((r) => setTimeout(r, 0));
    expect(container.querySelector("[cmdk-group]")).toBeNull();
  });

  it("captures what was typed, without leaving for the Brain page", async () => {
    render("pgbouncer listens on 6432");
    const row = await screen.findByTestId("brain-capture");
    expect(row.textContent).toContain("Capture to Brain: pgbouncer listens on 6432");
    fireEvent.click(row);
    await waitFor(() =>
      expect(state.captured).toEqual([{ content: "pgbouncer listens on 6432" }]),
    );
    expect(state.pushed).toEqual([]);
    expect(state.closed).toBe(1);
  });

  it("keeps the palette open when the capture fails", async () => {
    state.captureFails = true;
    render("pgbouncer");
    fireEvent.click(await screen.findByTestId("brain-capture"));
    await waitFor(() => expect(state.closed).toBe(0));
  });

  it("renders a hostile snippet as text", async () => {
    state.notes = [hit({ snippet: '<mark>x</mark><script>alert("x")</script>' })];
    render("x");
    const row = await screen.findByTestId("brain-note");
    expect(row.querySelector("script")).toBeNull();
    expect(row.textContent).toContain('<script>alert("x")</script>');
  });

  it("opens a note hit on the Brain page with that note selected", async () => {
    state.notes = [hit()];
    render("release");
    const row = await screen.findByTestId("brain-note");
    expect(row.textContent).toContain("Deploys go through the release tag");
    // The server's markers are stripped (this row highlights the query
    // itself), and the snippet reaches the DOM as text, never as HTML.
    expect(row.textContent).toContain("push v0.x.x on main");
    expect(row.querySelector("script")).toBeNull();
    fireEvent.click(row);
    expect(state.pushed).toEqual(["/w/brain?note=note-1"]);
    expect(state.closed).toBe(1);
  });
});
