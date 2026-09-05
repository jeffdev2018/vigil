// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentEffect } from "@multica/core/issues/agent-effects";
import { renderWithI18n } from "../../test/i18n";

// Parsing, state derivation and run grouping: packages/core/issues/agent-effects.test.ts.

const state = vi.hoisted(() => ({ effects: [] as AgentEffect[], undoTask: vi.fn(), undoEffect: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), warning: vi.fn() } }));
vi.mock("@multica/core/issues/agent-effects", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/agent-effects")>()),
  issueAgentEffectsOptions: () => ({ queryKey: ["agent-effects"], queryFn: async () => ({ effects: state.effects, window_hours: 24 }) }),
  useUndoTask: () => ({ mutate: state.undoTask, isPending: false }),
  useUndoAgentEffect: () => ({ mutate: state.undoEffect, isPending: false }),
}));

import { AgentEffectsSection } from "./agent-effects-section";

const effect = (over: Partial<AgentEffect> = {}): AgentEffect => ({
  id: "e1", task_id: "t1", agent_id: "a1", agent_name: "Scout", issue_id: "i1", kind: "issue_status", target_type: "issue", target_id: "i1",
  before: { field: "status", value: "todo" }, after: { field: "status", value: "in_progress" }, reversible: true, reversed_at: null,
  status: "applied", decision_id: null, payload: {},
  reversed_by_type: null, reverse_error: null, within_window: true, expires_at: "2026-09-06T00:00:00Z", created_at: "2026-09-05T00:00:00Z", ...over,
});

function render(canManage = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <AgentEffectsSection issueId="i1" canManage={canManage} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.effects = [];
  state.undoTask.mockReset();
  state.undoEffect.mockReset();
});

describe("AgentEffectsSection", () => {
  it("renders nothing when no run touched the issue", async () => {
    const { container } = render();
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("lists a run's effects with the undo buttons and describes each change", async () => {
    state.effects = [
      effect({ id: "e2", kind: "comment_create", before: {}, after: { excerpt: "the run says hi" } }),
      effect({ id: "e1" }),
      effect({ id: "e0", task_id: "t0", agent_name: "Older", reversed_at: "2026-09-05T01:00:00Z" }),
    ];
    render();
    expect(await screen.findByTestId("agent-effects")).toBeTruthy();
    expect(screen.getAllByTestId("agent-effects-run")).toHaveLength(2);
    expect(screen.getByText("Run by Scout")).toBeTruthy();
    expect(screen.getAllByText("Status todo → in_progress")).toHaveLength(2);
    expect(screen.getByText("Comment: the run says hi")).toBeTruthy();
    expect(screen.getByText("reversed")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Undo this run (2)" }));
    expect(state.undoTask).toHaveBeenCalledWith("t1", expect.anything());
    fireEvent.click(screen.getAllByRole("button", { name: "Undo" })[0]!);
    expect(state.undoEffect).toHaveBeenCalledWith("e2", expect.anything());
    // The older run is fully reversed: no run button for it.
    expect(screen.queryByRole("button", { name: "Undo this run (0)" })).toBeNull();
  });

  it("describes deletions and chat replies from the journal snapshots", async () => {
    state.effects = [
      effect({ id: "e1", kind: "comment_delete", before: { excerpt: "bye" } }),
      effect({ id: "e2", kind: "note_delete", before: { title: "Roadmap" } }),
      effect({ id: "e3", kind: "chat_message", after: { excerpt: "wrong answer" } }),
    ];
    render();
    await screen.findByTestId("agent-effects");
    expect(screen.getByText("Deleted a comment: “bye”")).toBeTruthy();
    expect(screen.getByText("Deleted the note “Roadmap”")).toBeTruthy();
    expect(screen.getByText(/Replied in chat: “wrong answer”/)).toBeTruthy();
  });

  it("shows a preview-mode run's held write as awaiting approval, without an undo button", async () => {
    state.effects = [effect({ id: "e1", kind: "issue_update", status: "pending", payload: { status: "in_progress", priority: "high" } })];
    render();
    await screen.findByTestId("agent-effects");
    expect(screen.getByText("Issue update: status, priority")).toBeTruthy();
    expect(screen.getByText("awaiting approval")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });

  it("marks expired and non-reversible effects and hides the buttons for viewers", async () => {
    state.effects = [effect({ id: "e1", within_window: false }), effect({ id: "e2", kind: "issue_create", reversible: false, after: { title: "child" } })];
    render(false);
    await screen.findByTestId("agent-effects");
    expect(screen.getByText("window expired")).toBeTruthy();
    expect(screen.getByText("not reversible")).toBeTruthy();
    expect(screen.queryByRole("button")).toBeNull();
  });
});
