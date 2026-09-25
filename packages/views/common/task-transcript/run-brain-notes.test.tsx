// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TaskNoteUsageResponse } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { RunBrainNotes } from "./run-brain-notes";

const TASK = "11111111-2222-4333-8444-555555555555";
const response = vi.hoisted(() => ({ value: { notes: [] } as TaskNoteUsageResponse }));

vi.mock("@multica/core/brain/queries", () => ({
  taskNoteUsageOptions: (_wsId: string, taskId: string) => ({
    queryKey: ["brain", "ws-1", "task-usage", taskId],
    queryFn: async () => response.value,
  }),
}));

function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <RunBrainNotes wsId="ws-1" taskId={TASK} />
    </QueryClientProvider>,
  );
}

describe("RunBrainNotes", () => {
  it("lists the notes a run used, a deleted one by id", async () => {
    response.value = {
      notes: [
        { note_id: "aaaaaaaa-1", title: "Deploys go through the release tag", revision: 2, kinds: ["injected", "opened"], channels: ["daemon_brief", "file_read"], first_at: null, deleted: false },
        { note_id: "bbbbbbbb-2", title: "", revision: 1, kinds: ["injected"], channels: ["daemon_brief"], first_at: null, deleted: true },
      ],
    };
    render();
    expect(await screen.findByText("Deploys go through the release tag")).toBeTruthy();
    expect(screen.getByText(/Injected, Opened/)).toBeTruthy();
    expect(screen.getByText("Deleted note (bbbbbbbb)")).toBeTruthy();
    expect(screen.getByRole("region", { name: "Brain notes" })).toBeTruthy();
  });

  it("labels a cited note and an unknown kind with its raw value", async () => {
    response.value = {
      notes: [
        { note_id: "cccccccc-3", title: "Release tags are cut from main", revision: 1, kinds: ["cited", "some-future-kind"], channels: ["comment"], first_at: null, deleted: false },
      ],
    };
    render();
    expect(await screen.findByText("Release tags are cut from main")).toBeTruthy();
    expect(screen.getByText(/Cited, some-future-kind/)).toBeTruthy();
  });

  it("renders nothing for a run that used no note", async () => {
    response.value = { notes: [] };
    const { container } = render();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(container.textContent).toBe("");
  });
});
