// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider, keepPreviousData } from "@tanstack/react-query";
import { toast } from "sonner";
import type {
  WorkspaceNote,
  WorkspaceNoteSearchResponse,
  WorkspaceNotesResponse,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { BrainPage } from "./brain-page";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspaceSlug: () => "acme",
}));

const note = (over: Partial<WorkspaceNote> = {}): WorkspaceNote => ({
  id: "note-1",
  workspace_id: "ws-1",
  title: "Deploys go through the release tag",
  content: "Push `v0.x.x` on main.",
  tags: ["deploy"],
  source: "manual",
  source_task_id: null,
  source_agent_id: null,
  pinned: false,
  archived_at: null,
  merged_into: null,
  created_by_type: "member",
  created_by_id: "user-1",
  revision: 3,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-02T00:00:00Z",
  ...over,
});

const data = vi.hoisted(() => ({
  response: { items: [], tags: [] } as WorkspaceNotesResponse,
  searchResponse: { notes: [], vector: false } as WorkspaceNoteSearchResponse,
  lastParams: undefined as unknown,
  lastSearch: undefined as unknown,
  rawCount: 0,
}));

vi.mock("@multica/core/brain/queries", () => ({
  brainNotesOptions: (_wsId: string, params?: unknown) => {
    data.lastParams = params;
    return {
      queryKey: ["brain", "ws-1", "list", JSON.stringify(params ?? {})],
      // Mirrors the real options: a filter change must not blank the list and
      // the tag chips back to the skeleton.
      placeholderData: keepPreviousData,
      queryFn: async () => data.response,
    };
  },
  noteSearchOptions: (_wsId: string, q: string, params?: unknown) => {
    data.lastSearch = { q, params };
    return {
      queryKey: ["brain", "ws-1", "search", q, JSON.stringify(params ?? {})],
      placeholderData: keepPreviousData,
      enabled: q.trim() !== "",
      queryFn: async () => data.searchResponse,
    };
  },
  brainCapturesOptions: () => ({
    queryKey: ["brain", "ws-1", "captures", "list", "raw"],
    queryFn: async () => ({ captures: [], raw_count: data.rawCount }),
  }),
  useBrainRawCount: () => ({ data: data.rawCount }),
}));

const mutations = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  archive: vi.fn(),
  remove: vi.fn(),
  noop: vi.fn(),
}));

vi.mock("@multica/core/brain/mutations", () => ({
  useCreateWorkspaceNote: () => ({ mutateAsync: mutations.create, isPending: false }),
  useUpdateWorkspaceNote: () => ({ mutateAsync: mutations.update, isPending: false }),
  useSetWorkspaceNoteArchived: () => ({ mutateAsync: mutations.archive, isPending: false }),
  useDeleteWorkspaceNote: () => ({ mutateAsync: mutations.remove, isPending: false }),
  useCaptureText: () => ({ mutateAsync: mutations.noop, isPending: false }),
  useCaptureUpload: () => ({ mutateAsync: mutations.noop, isPending: false, error: null }),
  useSuggestCapture: () => ({ mutateAsync: mutations.noop, isPending: false }),
  useOrganizeCapture: () => ({ mutateAsync: mutations.noop, isPending: false }),
  useReopenCapture: () => ({ mutateAsync: mutations.noop, isPending: false }),
  useDeleteCapture: () => ({ mutateAsync: mutations.noop, isPending: false }),
}));

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
  const rendered = renderWithI18n(
    <NavigationProvider value={adapter}>
      <QueryClientProvider client={client}>
        <BrainPage />
      </QueryClientProvider>
    </NavigationProvider>,
  );
  return { ...rendered, client };
}

/**
 * The page opens on the capture inbox ("capture first, organize later"), so
 * every note assertion starts by switching to the Notes tab.
 */
async function renderNotes() {
  const rendered = renderPage();
  fireEvent.click(await screen.findByRole("button", { name: "Notes" }));
  return rendered;
}

describe("BrainPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mutations.create.mockResolvedValue(note({ id: "note-new" }));
    mutations.update.mockResolvedValue(note({ revision: 4 }));
    mutations.archive.mockResolvedValue(note({ archived_at: "2026-01-03T00:00:00Z" }));
    mutations.remove.mockResolvedValue(undefined);
    data.response = { items: [note()], tags: ["deploy", "release"] };
    data.searchResponse = { notes: [], vector: false };
    data.rawCount = 0;
  });

  it("lists notes with their source badge and tags", async () => {
    await renderNotes();
    expect(await screen.findByText("Deploys go through the release tag")).toBeTruthy();
    expect(screen.getByText("Manual")).toBeTruthy();
    // The tag facets come from the server, not from the notes on screen, so a
    // tag with no match in the current filter is still offered.
    expect(screen.getByRole("button", { name: "release" })).toBeTruthy();
  });

  it("shows a note's body as rendered markdown once selected", async () => {
    await renderNotes();
    fireEvent.click(await screen.findByText("Deploys go through the release tag"));
    expect(await screen.findByRole("heading", { name: "Deploys go through the release tag" })).toBeTruthy();
    // The body is markdown: the backticks become a code element, not literal text.
    await waitFor(() => expect(document.querySelector("code")?.textContent).toBe("v0.x.x"));
  });

  it("offers to create the first note from the empty state", async () => {
    data.response = { items: [], tags: [] };
    await renderNotes();
    expect(await screen.findByText("No notes yet")).toBeTruthy();
    const buttons = screen.getAllByRole("button", { name: "New note" });
    expect(buttons).toHaveLength(2);
    fireEvent.click(buttons[1]!);
    expect(screen.getByLabelText("Title")).toBeTruthy();
  });

  it("creates a note from the header action", async () => {
    await renderNotes();
    fireEvent.click(await screen.findByRole("button", { name: "New note" }));
    fireEvent.change(screen.getByLabelText("Title"), {
      target: { value: "Postgres runs behind pgbouncer" },
    });
    fireEvent.change(screen.getByLabelText("Tags"), { target: { value: "db, infra" } });
    fireEvent.change(screen.getByLabelText("Content"), { target: { value: "port 6432" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() =>
      expect(mutations.create).toHaveBeenCalledWith({
        title: "Postgres runs behind pgbouncer",
        content: "port 6432",
        tags: ["db", "infra"],
      }),
    );
    expect(toast.success).toHaveBeenCalled();
  });

  it("sends the note's revision on edit so a concurrent write conflicts", async () => {
    await renderNotes();
    fireEvent.click(await screen.findByText("Deploys go through the release tag"));
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Content"), { target: { value: "new body" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mutations.update).toHaveBeenCalledWith({
        id: "note-1",
        input: {
          title: "Deploys go through the release tag",
          content: "new body",
          tags: ["deploy"],
          revision: 3,
        },
      }),
    );
  });

  it("keeps the revision the draft was opened on when the note refreshes while editing", async () => {
    // Regression (JEF-348 recette): a realtime update refetched the note under
    // the open editor and the save carried the NEW revision, so the server
    // accepted a draft built on stale fields and the concurrent edit was lost.
    const { client } = await renderNotes();
    fireEvent.click(await screen.findByText("Deploys go through the release tag"));
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.change(screen.getByLabelText("Content"), { target: { value: "my draft" } });

    data.response = { items: [note({ revision: 4, content: "someone else's body" })], tags: ["deploy"] };
    await client.invalidateQueries();
    await waitFor(() => expect(client.isFetching()).toBe(0));
    // The draft is untouched by the refresh…
    expect((screen.getByLabelText("Content") as HTMLTextAreaElement).value).toBe("my draft");

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    // …and the save still names revision 3, so the server answers 409.
    await waitFor(() =>
      expect(mutations.update).toHaveBeenCalledWith({
        id: "note-1",
        input: expect.objectContaining({ content: "my draft", revision: 3 }),
      }),
    );
  });

  it("tells the user to reload when the server reports a 409", async () => {
    const { ApiError } = await import("@multica/core/api");
    mutations.update.mockRejectedValue(new ApiError("conflict", 409, "Conflict"));
    await renderNotes();
    fireEvent.click(await screen.findByText("Deploys go through the release tag"));
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(toast.info).toHaveBeenCalled());
    expect(toast.error).not.toHaveBeenCalled();
    // The editor stays open with the user's text; a conflict must not discard it.
    expect(screen.getByLabelText("Content")).toBeTruthy();
  });

  it("sends a typed query to the ranked search endpoint, and the tag with it", async () => {
    data.searchResponse = {
      notes: [
        { ...note(), score: 0.7, snippet: "", passage_heading: "", lex_rank: 1, vec_rank: null },
      ],
      vector: false,
    };
    await renderNotes();
    await screen.findByText("Deploys go through the release tag");
    fireEvent.change(screen.getByLabelText("Search notes"), {
      target: { value: "pgbouncer" },
    });
    // Debounced by 250ms, so the endpoint is not asked per keystroke.
    await waitFor(() => expect(data.lastSearch).toMatchObject({ q: "pgbouncer" }));
    fireEvent.click(screen.getByRole("button", { name: "deploy" }));
    await waitFor(() =>
      expect(data.lastSearch).toMatchObject({
        q: "pgbouncer",
        params: { tag: "deploy" },
      }),
    );
    // The plain list keeps its own filters and never receives the query: an
    // empty box is the list, a typed box is the ranked endpoint.
    expect(data.lastParams).toMatchObject({ search: "" });
  });

  it("renders a search snippet as text, marks included, never as HTML", async () => {
    data.searchResponse = {
      notes: [
        {
          ...note({ title: "Injected" }),
          score: 1,
          snippet: '<mark>deploy</mark> <script>alert("x")</script>',
          passage_heading: "",
          lex_rank: 1,
          vec_rank: null,
        },
      ],
      vector: true,
    };
    await renderNotes();
    fireEvent.change(screen.getByLabelText("Search notes"), {
      target: { value: "deploy" },
    });
    // The mark survives as a real element (that is what the server asked for)…
    await waitFor(() => expect(document.querySelector("mark")?.textContent).toBe("deploy"));
    // …and the note's own text does not: no script element, just characters.
    expect(document.querySelector("script")).toBeNull();
    expect(screen.getByText(/<script>alert\("x"\)<\/script>/)).toBeTruthy();
    // A fused vector rank is worth saying; the score is not.
    expect(screen.getByText("semantic")).toBeTruthy();
  });

  it("shows the matching section before the snippet", async () => {
    data.searchResponse = {
      notes: [
        {
          ...note({ title: "Runbook" }),
          score: 1,
          snippet: "run the <mark>revert</mark> pipeline",
          passage_heading: "Deploy › Rollback <b>",
          lex_rank: 1,
          vec_rank: null,
        },
      ],
      vector: false,
    };
    await renderNotes();
    fireEvent.change(screen.getByLabelText("Search notes"), {
      target: { value: "revert" },
    });
    await waitFor(() => expect(document.querySelector("mark")?.textContent).toBe("revert"));
    // The heading is text, even when it looks like markup.
    expect(screen.getByText("Deploy › Rollback <b> ·")).toBeTruthy();
    expect(document.querySelector("b")).toBeNull();
  });

  it("shows the raw capture count on the inbox tab", async () => {
    data.rawCount = 3;
    renderPage();
    expect(await screen.findByLabelText("3 to sort")).toBeTruthy();
  });

  it("deletes a note only after the confirm, and surfaces the server's 403 text", async () => {
    const { ApiError } = await import("@multica/core/api");
    mutations.remove.mockRejectedValueOnce(
      new ApiError("only a workspace admin or the note author can delete this note", 403, "Forbidden"),
    );
    await renderNotes();
    fireEvent.click(await screen.findByText("Deploys go through the release tag"));
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(mutations.remove).not.toHaveBeenCalled();
    // Scoped to the dialog: the icon button on the note carries the same label.
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(mutations.remove).toHaveBeenCalledWith("note-1"));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        "only a workspace admin or the note author can delete this note",
      ),
    );
  });

  it("links an agent-written note to the agent that saved it", async () => {
    data.response = {
      items: [note({ source: "agent", source_agent_id: "agent-1", source_task_id: "task-1" })],
      tags: [],
    };
    await renderNotes();
    const link = await screen.findByRole("link", { name: "Open the run" });
    expect(link.getAttribute("href")).toBe("/acme/agents/agent-1");
  });
});
