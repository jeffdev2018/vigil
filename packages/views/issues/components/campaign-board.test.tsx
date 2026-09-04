// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { CampaignShard, RefactorCampaign } from "@multica/core/issues/campaign";
import { renderWithI18n } from "../../test/i18n";

// Parsing, progress and skippability: packages/core/issues/campaign.test.ts.

const state = vi.hoisted(() => ({ campaign: null as RefactorCampaign | null, create: vi.fn(), skip: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({ useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("../../navigation", () => ({ AppLink: ({ href, children, className }: { href: string; children: React.ReactNode; className?: string }) => <a href={href} className={className}>{children}</a> }));
vi.mock("@multica/core/workspace/queries", () => ({ agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "lead", name: "Lead" }, { id: "a", name: "Alpha" }, { id: "b", name: "Beta" }] }) }));
vi.mock("@multica/core/issues/campaign", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/campaign")>()),
  issueCampaignOptions: () => ({ queryKey: ["campaign"], queryFn: async () => state.campaign }),
  useCreateCampaign: () => ({ mutate: state.create, isPending: false }),
  useSkipCampaignShard: () => ({ mutate: state.skip, isPending: false }),
}));

import { CampaignBoard } from "./campaign-board";

const shard = (id: string, n: number, over: Partial<CampaignShard> = {}): CampaignShard => ({ id, child_issue_id: "c-" + id, task_id: "t-" + id, task_status: "completed", run_outcome: "completed", assignee_agent_id: "a", description: "Shard " + id, branch_name: "campaign/x/shard-" + (n + 1), merge_position: n, merge_status: "pending", merge_task_id: null, blockers: [], updated_at: "", ...over });

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CampaignBoard issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.campaign = null;
  state.create.mockReset();
  state.skip.mockReset();
});

describe("CampaignBoard", () => {
  it("launches a campaign with a target branch, a leader and assigned shards", async () => {
    render();
    fireEvent.click(await screen.findByText("Start a refactoring campaign"));
    fireEvent.change(screen.getByLabelText("Campaign name"), { target: { value: "API rename" } });
    fireEvent.change(screen.getByLabelText("Target branch"), { target: { value: "develop" } });
    fireEvent.change(screen.getByLabelText("Leader agent"), { target: { value: "lead" } });
    fireEvent.change(screen.getByLabelText("Shard 1"), { target: { value: "Rename in server/" } });
    fireEvent.change(screen.getByLabelText("Assignee of shard 1"), { target: { value: "a" } });
    fireEvent.click(screen.getByRole("button", { name: "Add a shard" }));
    fireEvent.change(screen.getByLabelText("Shard 2"), { target: { value: "Rename in packages/" } });
    fireEvent.change(screen.getByLabelText("Branch of shard 2"), { target: { value: "feat/pkg" } });
    fireEvent.change(screen.getByLabelText("Assignee of shard 2"), { target: { value: "b" } });
    fireEvent.click(screen.getByRole("button", { name: "Launch campaign" }));
    expect(state.create).toHaveBeenCalledWith({ name: "API rename", target_branch: "develop", leader_agent_id: "lead", shards: [{ description: "Rename in server/", assignee_id: "a" }, { description: "Rename in packages/", assignee_id: "b", branch_name: "feat/pkg" }] }, expect.anything());
  });

  it("shows every shard with its queue position and merge status, a conflict with its skip button, and the rows behind it", async () => {
    state.campaign = { id: "c1", issue_id: "i1", fanout_batch_id: "f", name: "API rename", target_branch: "main", status: "merging", created_at: "", completed_at: null, shards: [
      shard("s1", 0, { merge_status: "merged" }),
      shard("s2", 1, { merge_status: "conflict", blockers: [{ kind: "merge_conflict", label: "The rebase-and-merge run failed" }] }),
      shard("s3", 2, { merge_status: "ready" }),
      shard("s4", 3, { merge_status: "pending", run_outcome: null, task_status: "running" }),
    ] };
    render();
    expect(await screen.findByTestId("campaign")).toBeTruthy();
    expect(screen.getAllByTestId("campaign-shard").map((el) => el.getAttribute("data-merge-status"))).toEqual(["merged", "conflict", "ready", "pending"]);
    expect(screen.getByText("1/4 merged or skipped")).toBeTruthy();
    expect(screen.getByTestId("campaign-conflict").textContent).toContain("The rebase-and-merge run failed");
    expect(screen.queryByRole("button", { name: "Skip shard #1" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Skip shard #2" }));
    expect(state.skip).toHaveBeenCalledWith("s2", expect.anything());
    expect(screen.queryByText("Start a refactoring campaign")).toBeNull();
  });

  it("offers a new campaign once the previous one completed, with no skip buttons left", async () => {
    state.campaign = { id: "c1", issue_id: "i1", fanout_batch_id: "f", name: "Done", target_branch: "main", status: "completed", created_at: "", completed_at: "2026-09-04T10:00:00Z", shards: [shard("s1", 0, { merge_status: "merged" }), shard("s2", 1, { merge_status: "skipped" })] };
    render();
    expect(await screen.findByText("Start a refactoring campaign")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /Skip shard/ })).toBeNull();
    expect(screen.getByText("2/2 merged or skipped")).toBeTruthy();
  });
});
