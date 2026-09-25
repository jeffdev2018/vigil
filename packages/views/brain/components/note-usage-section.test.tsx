// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WorkspaceNoteUsage } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NoteUsageSection, useRecordNoteViewOnce } from "./note-usage-section";

const state = vi.hoisted(() => ({
  fetch: vi.fn(),
  record: vi.fn(),
}));

vi.mock("@multica/core/brain/queries", () => ({
  noteUsageOptions: (_wsId: string, id: string) => ({
    queryKey: ["brain", "ws-1", "usage", id],
    queryFn: () => state.fetch(id),
  }),
}));
vi.mock("@multica/core/brain/mutations", () => ({
  useRecordNoteView: () => ({ mutate: state.record }),
}));
vi.mock("../../common/task-transcript/open-run-button", () => ({
  OpenRunButton: ({ taskId, label }: { taskId: string; label: string }) => (
    <button type="button" data-task={taskId}>{label}</button>
  ),
}));

const usage = (over: Partial<WorkspaceNoteUsage> = {}): WorkspaceNoteUsage => ({
  counts: { injected: 4, retrieved: 2, opened: 1, viewed: 3, cited: 5 },
  runs_count: 2,
  viewers_count: 2,
  last_used_at: new Date().toISOString(),
  runs: [
    { task_id: "task-1", agent_id: "agent-1", agent_name: "Ada", issue_id: "issue-1", issue_identifier: "HAN-7", kinds: ["injected", "opened"], first_at: new Date().toISOString(), private: false },
    { task_id: "", agent_id: "agent-1", agent_name: "Ada", issue_id: "", issue_identifier: "", kinds: ["retrieved"], first_at: new Date().toISOString(), private: true },
  ],
  ...over,
});

function render(ui: React.ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  state.fetch.mockReset();
  state.record.mockReset();
});

describe("NoteUsageSection", () => {
  it("shows a loading placeholder, then runs, counts and a masked private run", async () => {
    state.fetch.mockResolvedValue(usage());
    const { container } = render(<NoteUsageSection wsId="ws-1" noteId="note-1" />);
    expect(container.querySelector('[data-slot="skeleton"], .animate-pulse')).not.toBeNull();
    expect(await screen.findByText("Used by 2 runs")).toBeTruthy();
    expect(
      screen.getByText(/Injected 4 · Retrieved 2 · Opened 1 · Cited 5 · Read by 2 people/),
    ).toBeTruthy();
    expect(screen.getByText("HAN-7")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Open run" }).getAttribute("data-task")).toBe("task-1");
    expect(screen.getByText("Private chat run")).toBeTruthy();
    expect(screen.getAllByRole("button")).toHaveLength(1);
  });

  it("labels a cited run and an unknown kind with its raw value", async () => {
    state.fetch.mockResolvedValue(
      usage({
        runs: [
          {
            task_id: "task-2",
            agent_id: "agent-1",
            agent_name: "Ada",
            issue_id: "",
            issue_identifier: "",
            kinds: ["cited", "some-future-kind"],
            first_at: new Date().toISOString(),
            private: false,
          },
        ],
      }),
    );
    render(<NoteUsageSection wsId="ws-1" noteId="note-1" />);
    expect(await screen.findByText(/Cited, some-future-kind/)).toBeTruthy();
  });

  it("says so when nothing used the note", async () => {
    state.fetch.mockResolvedValue(usage({ runs_count: 0, viewers_count: 0, runs: [], last_used_at: null, counts: { injected: 0, retrieved: 0, opened: 0, viewed: 0, cited: 0 } }));
    render(<NoteUsageSection wsId="ws-1" noteId="note-1" />);
    expect(await screen.findByText("No run has used this note yet.")).toBeTruthy();
  });

  it("shows an error state", async () => {
    state.fetch.mockRejectedValue(new Error("boom"));
    render(<NoteUsageSection wsId="ws-1" noteId="note-1" />);
    expect(await screen.findByText("Usage could not be loaded.")).toBeTruthy();
  });
});

function Reader({ noteId }: { noteId: string }) {
  useRecordNoteViewOnce("ws-1", noteId);
  return null;
}

describe("useRecordNoteViewOnce", () => {
  it("records one view per note shown, not per render", async () => {
    const { rerender } = render(<Reader noteId="note-1" />);
    rerender(<QueryClientProvider client={new QueryClient()}><Reader noteId="note-1" /></QueryClientProvider>);
    await waitFor(() => expect(state.record).toHaveBeenCalledTimes(1));
    expect(state.record).toHaveBeenCalledWith("note-1");
  });
});
