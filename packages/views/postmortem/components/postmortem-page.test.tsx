// @vitest-environment jsdom
import { describe, expect, it, vi, beforeEach } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PostmortemsResponse, PostmortemStats } from "@multica/core/types";
import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";
import { PostmortemPage } from "./postmortem-page";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const data = vi.hoisted(() => ({
  stats: { draft: 1, approved: 0, discarded: 0 } as PostmortemStats,
  items: {
    items: [
      {
        id: "pm-1",
        source_task_id: "task-1",
        issue_id: "issue-1",
        agent_id: "agent-1",
        trigger: "failed",
        state: "draft",
        failure_reason: "agent_error.context_overflow",
        summary: "The run exhausted the model context.",
        root_cause: "Too many large files were loaded at once.",
        impact: "The intended change was not delivered.",
        preventive_rules: ["Split large tasks into smaller sub-tasks."],
        cost_usd_ticks: 12345,
        llm_generated: true,
        revision: 1,
        created_at: "2026-01-01T00:00:00Z",
      },
    ],
    next_cursor: undefined,
  } as PostmortemsResponse,
}));

vi.mock("@multica/core/postmortem/queries", () => ({
  postmortemStatsOptions: () => ({
    queryKey: ["postmortem", "ws-1", "stats"],
    queryFn: async () => data.stats,
  }),
  postmortemItemsOptions: () => ({
    queryKey: ["postmortem", "ws-1", "items", "draft"],
    queryFn: async () => data.items,
  }),
}));

const mutations = vi.hoisted(() => ({
  approve: vi.fn().mockResolvedValue({ id: "pm-1", state: "approved" }),
  discard: vi.fn().mockResolvedValue({ id: "pm-1", state: "discarded" }),
}));

vi.mock("@multica/core/postmortem/mutations", () => ({
  useApprovePostmortem: () => ({ mutateAsync: mutations.approve, isPending: false }),
  useDiscardPostmortem: () => ({ mutateAsync: mutations.discard, isPending: false }),
}));

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={client}>
      <PostmortemPage />
    </QueryClientProvider>,
  );
}

describe("PostmortemPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.stats = { draft: 1, approved: 0, discarded: 0 };
    data.items.items = [
      {
        id: "pm-1",
        source_task_id: "task-1",
        issue_id: "issue-1",
        agent_id: "agent-1",
        trigger: "failed",
        state: "draft",
        failure_reason: "agent_error.context_overflow",
        summary: "The run exhausted the model context.",
        root_cause: "Too many large files were loaded at once.",
        impact: "The intended change was not delivered.",
        preventive_rules: ["Split large tasks into smaller sub-tasks."],
        cost_usd_ticks: 12345,
        llm_generated: true,
        revision: 1,
        created_at: "2026-01-01T00:00:00Z",
      },
    ];
  });

  it("renders draft postmortems with the failure reason badge", async () => {
    renderPage();
    expect(await screen.findByText("The run exhausted the model context.")).toBeTruthy();
    expect(screen.getByText("agent_error.context_overflow")).toBeTruthy();
  });

  it("shows the empty state when there are no drafts", async () => {
    data.items.items = [];
    data.stats = { draft: 0, approved: 0, discarded: 0 };
    renderPage();
    expect(await screen.findByText("No drafts waiting")).toBeTruthy();
  });

  it("selecting an item shows root cause, impact, and preventive rules", async () => {
    renderPage();
    const row = await screen.findByText("The run exhausted the model context.");
    fireEvent.click(row);
    expect(await screen.findByText("Too many large files were loaded at once.")).toBeTruthy();
    expect(screen.getByText("The intended change was not delivered.")).toBeTruthy();
    expect(screen.getByText("Split large tasks into smaller sub-tasks.")).toBeTruthy();
  });

  it("approving the selected item calls the mutation and toasts", async () => {
    renderPage();
    const row = await screen.findByText("The run exhausted the model context.");
    fireEvent.click(row);
    const approveButton = await screen.findByRole("button", { name: "Approve" });
    fireEvent.click(approveButton);
    await waitFor(() => expect(mutations.approve).toHaveBeenCalledWith("pm-1"));
    expect(toast.success).toHaveBeenCalled();
  });

  it("discarding the selected item calls the mutation and toasts", async () => {
    renderPage();
    const row = await screen.findByText("The run exhausted the model context.");
    fireEvent.click(row);
    const discardButton = await screen.findByRole("button", { name: "Discard" });
    fireEvent.click(discardButton);
    await waitFor(() => expect(mutations.discard).toHaveBeenCalledWith("pm-1"));
    expect(toast.success).toHaveBeenCalled();
  });
});
