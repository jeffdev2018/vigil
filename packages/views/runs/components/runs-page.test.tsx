// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Run, RunsSummary } from "@multica/core/runs";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

// Heavy sub-surfaces are out of scope here — the page only needs to render
// their presence, not exercise the transcript/replay dialogs themselves.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => <span data-testid="actor-avatar">{name}</span>,
}));
vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: () => <span data-testid="transcript-button" />,
  ReplayButton: () => <span data-testid="replay-button" />,
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (sel: (s: unknown) => unknown) => sel({ user: { id: "u1" } }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "agent-1", name: "Walt" }] }),
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [{ user_id: "u1", role: state.role }],
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getMemberName: () => "Priya" }),
}));
vi.mock("@multica/core/runtimes", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes")>()),
  runtimeListOptions: () => ({ queryKey: ["runtimes"], queryFn: async () => [] }),
}));

const state = vi.hoisted(() => ({
  role: "member" as string,
  halt: { halted: false, reason: "", halted_by: "", halted_at: null } as {
    halted: boolean;
    reason: string;
    halted_by: string;
    halted_at: string | null;
  },
  setRunHalt: vi.fn(),
  killSwitch: vi.fn(async (_reason: string) => ({ run_halt: state.halt, cancelled: 0, results: [] })),
  cancelRuns: vi.fn(async (ids: string[]) => ({
    results: ids.map((id) => ({ task_id: id, outcome: "cancelled" as const })),
    cancelled: ids.length,
  })),
  infiniteOptionsCalls: [] as unknown[],
}));

vi.mock("@multica/core/run-halt", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/run-halt")>()),
  runHaltOptions: () => ({ queryKey: ["run-halt"], queryFn: async () => state.halt }),
  useSetRunHalt: () => ({ mutate: state.setRunHalt, isPending: false }),
}));

function run(overrides: Partial<Run> & { id: string }): Run {
  return {
    agent_id: "agent-1",
    runtime_id: "",
    issue_id: overrides.issue?.id ?? "",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-09-09T09:00:00Z",
    leg_role: "",
    workflow_root_task_id: "",
    agent_name: "Walt",
    issue: null,
    cost_usd_ticks: 0,
    duration_ms: 0,
    silence_ms: 0,
    blocked_on: null,
    ...overrides,
  } as Run;
}

const runningRun = run({
  id: "task-1",
  status: "running",
  agent_name: "Walt",
  issue: { id: "issue-1", identifier: "ACM-1", title: "Payment gateway timeout", status: "in_progress" },
  cost_usd_ticks: 25_000_000_000,
  duration_ms: 65_000,
});
const blockedRun = run({
  id: "task-2",
  status: "waiting_local_directory",
  agent_name: "Ada",
  issue: { id: "issue-2", identifier: "ACM-2", title: "Flaky test", status: "in_progress" },
  blocked_on: { kind: "gate", id: "gate-1", decision_id: "dec-1", summary: "git push", since: null },
});

const summary: RunsSummary = {
  active: 2,
  queued: 0,
  running: 1,
  blocked: 1,
  completed_since: 3,
  failed_since: 0,
  cancelled_since: 0,
  cost_since_usd_ticks: 25_000_000_000,
  since: "2026-09-09T00:00:00Z",
  run_halt: { halted: false, reason: "", halted_by: "", halted_at: null },
};

vi.mock("@multica/core/runs", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/runs")>();
  const workspaceRunsInfiniteOptions = vi.fn((_wsId: string, filter: unknown) => {
    state.infiniteOptionsCalls.push(filter);
    return {
      queryKey: ["runs", "ws-1", JSON.stringify(filter)],
      queryFn: async () => ({ runs: [runningRun, blockedRun], next_cursor: undefined, summary }),
      initialPageParam: "",
      getNextPageParam: (last: { next_cursor?: string }) => last.next_cursor || undefined,
    };
  });
  return {
    ...actual,
    workspaceRunsInfiniteOptions,
    useCancelRuns: () => ({ mutateAsync: state.cancelRuns, isPending: false }),
    useKillSwitch: () => ({ mutate: state.killSwitch, isPending: false }),
  };
});

import { RunsPage } from "./runs-page";

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/runs",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <QueryClientProvider client={client}>
        <RunsPage />
      </QueryClientProvider>
    </NavigationProvider>,
  );
}

beforeEach(() => {
  state.role = "member";
  state.halt = { halted: false, reason: "", halted_by: "", halted_at: null };
  state.infiniteOptionsCalls.length = 0;
  state.setRunHalt.mockClear();
  state.killSwitch.mockClear();
  state.cancelRuns.mockClear();
});

describe("RunsPage", () => {
  it("renders the fleet rows from the runs list", async () => {
    renderPage();
    await screen.findByText("ACM-1 · Payment gateway timeout");
    expect(screen.getByText("ACM-2 · Flaky test")).toBeInTheDocument();
    expect(screen.getAllByTestId("actor-avatar")).toHaveLength(2);
    // Blocker chip renders the server-provided summary.
    expect(screen.getByText("git push")).toBeInTheDocument();
  });

  it("calls the runs list with the updated filter when the state segment changes", async () => {
    renderPage();
    await screen.findByText("ACM-1 · Payment gateway timeout");
    const before = state.infiniteOptionsCalls.length;
    fireEvent.click(screen.getByRole("button", { name: "Finished" }));
    await waitFor(() => expect(state.infiniteOptionsCalls.length).toBeGreaterThan(before));
    const last = state.infiniteOptionsCalls.at(-1) as { state: string };
    expect(last.state).toBe("terminal");
  });

  it("reports per-row outcomes when cancelling a selection", async () => {
    const { toast } = await import("sonner");
    renderPage();
    await screen.findByText("ACM-1 · Payment gateway timeout");

    const checkboxes = screen.getAllByRole("checkbox");
    fireEvent.click(checkboxes[0]!);
    fireEvent.click(checkboxes[1]!);

    fireEvent.click(screen.getByRole("button", { name: "Cancel selected" }));
    fireEvent.click(screen.getByRole("button", { name: "Cancel", hidden: true }));

    await waitFor(() => expect(state.cancelRuns).toHaveBeenCalledWith(["task-1", "task-2"]));
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
  });

  it("hides the kill switch from a plain member", async () => {
    state.role = "member";
    renderPage();
    await screen.findByText("ACM-1 · Payment gateway timeout");
    expect(screen.queryByRole("button", { name: "Kill switch" })).toBeNull();
  });

  it("lets an owner confirm the kill switch with a reason", async () => {
    state.role = "owner";
    renderPage();
    await screen.findByText("ACM-1 · Payment gateway timeout");

    fireEvent.click(screen.getByRole("button", { name: "Kill switch" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("Reason"), { target: { value: "40 stray PRs" } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Halt and cancel" }));

    await waitFor(() => expect(state.killSwitch).toHaveBeenCalledWith("40 stray PRs", expect.anything()));
  });
});
