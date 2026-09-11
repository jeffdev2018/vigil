// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import type { Workspace } from "@multica/core/types";
import type { DeadBranchPlan } from "@multica/core/runs";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  plan: { entries: [] } as DeadBranchPlan,
  updateWorkspace: vi.fn(),
  discard: vi.fn(),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }));

vi.mock("@multica/core/api", () => ({
  api: { updateWorkspace: (...args: unknown[]) => state.updateWorkspace(...args) },
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ href, children, ...rest }: { href: string; children?: React.ReactNode }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
}));

vi.mock("@multica/core/runs", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runs")>()),
  deadBranchesOptions: () => ({
    queryKey: ["dead-branches"],
    queryFn: async () => state.plan,
  }),
  useDiscardDeadBranches: () => ({ mutate: state.discard, isPending: false }),
}));

import { BranchCleanupSetting } from "./branch-cleanup-setting";

const workspace = (settings: Record<string, unknown> = {}): Workspace =>
  ({
    id: "ws-1",
    name: "Acme",
    slug: "acme",
    issue_prefix: "JEF",
    settings,
  }) as Workspace;

const entry = (over: Record<string, unknown> = {}) => ({
  task_id: "t1",
  issue_id: "i1",
  issue_identifier: "JEF-388",
  issue_title: "Branch GC",
  branch_name: "agent/jef-388/t1",
  runtime_id: "r1",
  runtime_name: "macbook",
  finished_at: "2026-09-01T00:00:00Z",
  actionable: true,
  skip_reason: null,
  ...over,
});

function render(ws: Workspace = workspace(), canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <BranchCleanupSetting workspace={ws} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.plan = { entries: [] };
  state.updateWorkspace.mockReset().mockImplementation(async (id: string, body: { settings: Record<string, unknown> }) => ({
    ...workspace(),
    id,
    settings: body.settings,
  }));
  state.discard.mockReset();
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.info).mockClear();
});

describe("BranchCleanupSetting auto-GC", () => {
  it("persists the toggle through the workspace settings blob, off by default", async () => {
    render();
    fireEvent.click(
      await screen.findByRole("switch", { name: "Automatically discard dead run branches" }),
    );
    await waitFor(() => expect(state.updateWorkspace).toHaveBeenCalled());
    expect(state.updateWorkspace).toHaveBeenCalledWith("ws-1", {
      settings: { branch_gc: { enabled: true, ttl_days: 30 } },
    });
  });

  it("keeps other settings keys when merging branch_gc", async () => {
    render(workspace({ triage_auto: { enabled: true }, branch_gc: { enabled: true, ttl_days: 7 } }));
    const ttl = await screen.findByRole("spinbutton", {
      name: "Discard branches older than (days)",
    });
    expect(ttl).toHaveValue(7);
    fireEvent.change(ttl, { target: { value: "14" } });
    fireEvent.blur(ttl);
    await waitFor(() => expect(state.updateWorkspace).toHaveBeenCalled());
    expect(state.updateWorkspace).toHaveBeenCalledWith("ws-1", {
      settings: {
        triage_auto: { enabled: true },
        branch_gc: { enabled: true, ttl_days: 14 },
      },
    });
  });

  it("clamps the TTL to 1..365 on blur", async () => {
    render();
    const ttl = await screen.findByRole("spinbutton", {
      name: "Discard branches older than (days)",
    });
    fireEvent.change(ttl, { target: { value: "0" } });
    fireEvent.blur(ttl);
    await waitFor(() => expect(state.updateWorkspace).toHaveBeenCalled());
    expect(state.updateWorkspace).toHaveBeenCalledWith("ws-1", {
      settings: { branch_gc: { enabled: false, ttl_days: 1 } },
    });
  });

  it("disables the controls when the reader may not edit workspace settings", async () => {
    render(workspace(), false);
    expect(
      await screen.findByRole("switch", { name: "Automatically discard dead run branches" }),
    ).toHaveAttribute("aria-disabled", "true");
    expect(
      screen.getByRole("spinbutton", { name: "Discard branches older than (days)" }),
    ).toBeDisabled();
  });
});

describe("BranchCleanupSetting retroactive pass", () => {
  it("shows the empty state when no dead branches exist", async () => {
    render();
    expect(await screen.findByTestId("dead-branches-empty")).toHaveTextContent("No dead branches");
    expect(screen.queryByRole("button", { name: /Discard all/ })).toBeNull();
  });

  it("groups entries by runtime and greys out non-actionable rows with a reason", async () => {
    state.plan = {
      entries: [
        entry({ task_id: "t1" }),
        entry({
          task_id: "t2",
          branch_name: "agent/other/t2",
          runtime_id: "r2",
          runtime_name: "ci-box",
          issue_id: null,
          issue_identifier: null,
          issue_title: null,
          actionable: false,
          skip_reason: "runtime_offline",
        }),
      ],
    };
    render();
    expect(await screen.findByText("macbook")).toBeInTheDocument();
    expect(screen.getByText("ci-box")).toBeInTheDocument();
    expect(screen.getByText("agent/jef-388/t1")).toBeInTheDocument();
    const link = screen.getByRole("link", { name: /JEF-388/ });
    expect(link).toHaveAttribute("href", "/acme/issues/i1");
    expect(screen.getByText("Machine offline")).toBeInTheDocument();
    // Only the actionable row offers a per-row discard.
    expect(screen.getAllByRole("button", { name: "Discard" })).toHaveLength(1);
  });

  it("discards every actionable entry after confirming", async () => {
    state.plan = {
      entries: [
        entry({ task_id: "t1" }),
        entry({ task_id: "t2", branch_name: "agent/other/t2" }),
        entry({ task_id: "t3", actionable: false, skip_reason: "action_pending" }),
      ],
    };
    state.discard.mockImplementation((_vars: unknown, opts: { onSuccess: (d: unknown) => void }) =>
      opts.onSuccess({ enqueued: 2, skipped: [{ task_id: "t3", reason: "action_pending" }] }),
    );
    render();
    fireEvent.click(await screen.findByRole("button", { name: "Discard all (2)" }));
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Discard" }));
    expect(state.discard).toHaveBeenCalledWith(
      { taskIds: ["t1", "t2"] },
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    );
    expect(toast.success).toHaveBeenCalledWith("Cleanup enqueued for 2 branches");
    expect(toast.info).toHaveBeenCalledWith("1 branches were skipped");
  });

  it("discards a single branch after confirming", async () => {
    state.plan = { entries: [entry({ task_id: "t1" })] };
    render();
    const rows = await screen.findAllByTestId("dead-branch-row");
    fireEvent.click(within(rows[0]!).getByRole("button", { name: "Discard" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("agent/jef-388/t1");
    fireEvent.click(within(dialog).getByRole("button", { name: "Discard" }));
    expect(state.discard).toHaveBeenCalledWith({ taskIds: ["t1"] }, expect.anything());
  });

  it("never fires the mutation when the dialog is cancelled", async () => {
    state.plan = { entries: [entry({ task_id: "t1" })] };
    render();
    fireEvent.click(await screen.findByRole("button", { name: "Discard all (1)" }));
    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    expect(state.discard).not.toHaveBeenCalled();
  });
});
