// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, keepPreviousData } from "@tanstack/react-query";
import { toast } from "sonner";
import type { WorkspaceNote, WorkspaceNotesResponse } from "@multica/core/types";
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
  lastParams: undefined as unknown,
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
}));

const mutations = vi.hoisted(() => ({
  create: vi.fn(),
  update: vi.fn(),
  archive: vi.fn(),
}));

vi.mock("@multica/core/brain/mutations", () => ({
  useCreateWorkspaceNote: () => ({ mutateAsync: mutations.create, isPending: false }),
  useUpdateWorkspaceNote: () => ({ mutateAsync: mutations.update, isPending: false }),
  useSetWorkspaceNoteArchived: () => ({ mutateAsync: mutations.archive, isPending: false }),
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
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <QueryClientProvider client={client}>
        <BrainPage />
      </QueryClientProvider>
    </NavigationProvider>,
  );
}

describe("BrainPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mutations.create.mockResolvedValue(note({ id: "note-new" }));
    mutations.update.mockResolvedValue(note({ revision: 4 }));
    mutations.archive.mockResolvedValue(note({ archived_at: "2026-01-03T00:00:00Z" }));
    data.response = { items: [note()], tags: ["deploy", "release"] };
  });

  it("lists notes with their source badge and tags", async () => {
    renderPage();
    expect(await screen.findByText("Deploys go through the release tag")).toBeTruthy();
    expect(screen.getByText("Manual")).toBeTruthy();
    // The tag facets come from the server, not from the notes on screen, so a
    // tag with no match in the current filter is still offered.
    expect(screen.getByRole("button", { name: "release" })).toBeTruthy();
  });

  it("shows a note's body as rendered markdown once selected", async () => {
    renderPage();
    fireEvent.click(await screen.findByText("Deploys go through the release tag"));
    expect(await screen.findByRole("heading", { name: "Deploys go through the release tag" })).toBeTruthy();
    // The body is markdown: the backticks become a code element, not literal text.
    await waitFor(() => expect(document.querySelector("code")?.textContent).toBe("v0.x.x"));
  });

  it("creates a note from the header action", async () => {
    renderPage();
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
    renderPage();
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

  it("tells the user to reload when the server reports a 409", async () => {
    const { ApiError } = await import("@multica/core/api");
    mutations.update.mockRejectedValue(new ApiError("conflict", 409));
    renderPage();
    fireEvent.click(await screen.findByText("Deploys go through the release tag"));
    fireEvent.click(screen.getByRole("button", { name: "Edit" }));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(toast.info).toHaveBeenCalled());
    expect(toast.error).not.toHaveBeenCalled();
    // The editor stays open with the user's text; a conflict must not discard it.
    expect(screen.getByLabelText("Content")).toBeTruthy();
  });

  it("passes the search box and the tag chip to the server", async () => {
    renderPage();
    await screen.findByText("Deploys go through the release tag");
    fireEvent.change(screen.getByLabelText("Search notes"), {
      target: { value: "pgbouncer" },
    });
    await waitFor(() =>
      expect(data.lastParams).toMatchObject({ search: "pgbouncer", tag: "" }),
    );
    fireEvent.click(screen.getByRole("button", { name: "deploy" }));
    await waitFor(() =>
      expect(data.lastParams).toMatchObject({ search: "pgbouncer", tag: "deploy" }),
    );
  });

  it("links an agent-written note to the agent that saved it", async () => {
    data.response = {
      items: [note({ source: "agent", source_agent_id: "agent-1", source_task_id: "task-1" })],
      tags: [],
    };
    renderPage();
    const link = await screen.findByRole("link", { name: "Open the run" });
    expect(link.getAttribute("href")).toBe("/acme/agents/agent-1");
  });
});
