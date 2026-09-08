// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentTask } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../../../navigation";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// api / paths are only reached from a rendered TaskRow, which never mounts in
// the loading and empty states under test — stub them so the module graph
// resolves without dragging in platform wiring.
vi.mock("@multica/core/api", () => ({ api: {} }));

// The tab reads three data sources. The activity map ("Last 30 days") stays
// empty; snapshot ("Now") and the per-agent task list ("Recent work") are
// swapped per test to stay pending or resolve.
const agentTasksRef = vi.hoisted(() => ({
  current: () => new Promise<unknown>(() => {}),
}));
const snapshotRef = vi.hoisted(() => ({
  current: () => Promise.resolve([] as unknown[]),
}));
// TeachFromRunButton (mounted on terminal rows) pulls in MemoryEditorDialog,
// which unconditionally calls the create/update mutation hooks below even
// while its dialog stays closed — stub them alongside the three options
// this tab itself reads.
vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({
    queryKey: ["snapshot"],
    queryFn: () => snapshotRef.current(),
  }),
  agentTasksOptions: () => ({
    queryKey: ["agent-tasks"],
    queryFn: () => agentTasksRef.current(),
  }),
  useWorkspaceActivityMap: () => ({ byAgent: new Map() }),
  summarizeActivityWindow: () => ({
    totalRuns: 0,
    totalFailed: 0,
    buckets: [],
  }),
  useCreateAgentMemory: () => ({ mutate: vi.fn(), isPending: false }),
  useUpdateAgentMemory: () => ({ mutate: vi.fn(), isPending: false }),
}));

// Every task in these tests belongs to an editable agent; the permission
// gate itself is covered by memory-tab.test.tsx.
vi.mock("@multica/core/permissions", () => ({
  useAgentPermissions: () => ({ canEdit: { allowed: true } }),
}));

// A rendered TaskRow calls useWorkspacePaths() for its "open issue" link.
// That hook throws outside a workspace-scoped route (see
// useRequiredWorkspaceSlug); a simple stub keeps these tests free of
// WorkspaceSlugProvider wiring, same as agent-live-peek-card.test.tsx.
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/issues/${id}` }),
}));

import { ActivityTab } from "./activity-tab";

const baseAgent = {
  id: "agent-1",
  name: "Agent",
} as unknown as Agent;

const EMPTY_RECENT = "This agent hasn't completed anything yet.";

function renderTab() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/agents/agent-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <ActivityTab agent={baseAgent} showPerformance={false} />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
}

beforeEach(() => {
  agentTasksRef.current = () => new Promise<unknown>(() => {});
  snapshotRef.current = () => Promise.resolve([]);
});

describe("ActivityTab Recent work loading state", () => {
  it("shows a skeleton, not the empty state, while the task list is loading", () => {
    // Never-resolving queryFn keeps the per-agent task query pending, which is
    // exactly the first-paint window the skeleton is meant to cover.
    const { container } = renderTab();
    expect(
      container.querySelectorAll('[data-slot="skeleton"]').length,
    ).toBeGreaterThan(0);
    expect(screen.queryByText(EMPTY_RECENT)).not.toBeInTheDocument();
  });

  it("shows the empty state once the task list resolves to no runs", async () => {
    agentTasksRef.current = () => Promise.resolve([]);
    renderTab();
    expect(await screen.findByText(EMPTY_RECENT)).toBeInTheDocument();
    expect(
      document.querySelectorAll('[data-slot="skeleton"]').length,
    ).toBe(0);
  });
});

// Canonical test for TeachFromRunButton's own gating (permission, status,
// chat session) lives in memory-tab.test.tsx. This only proves the host row
// wires it up on the right rows.
describe("ActivityTab Recent work teach-from-run action", () => {
  const teachButton = () =>
    screen.queryByRole("button", { name: "Teach a correction" });

  it("renders the teach action on a completed run of an editable agent", async () => {
    const task: AgentTask = {
      id: "run-1",
      agent_id: "agent-1",
      runtime_id: "runtime-1",
      issue_id: "",
      status: "completed",
      priority: 0,
      dispatched_at: "2026-09-04T00:00:00Z",
      started_at: "2026-09-04T00:00:00Z",
      completed_at: "2026-09-04T00:01:00Z",
      result: null,
      error: null,
      created_at: "2026-09-04T00:00:00Z",
    };
    agentTasksRef.current = () => Promise.resolve([task]);
    renderTab();
    expect(
      await screen.findByRole("button", { name: "Teach a correction" }),
    ).toBeInTheDocument();
  });

  it("does not render the teach action on a run still in progress", async () => {
    // A running task surfaces through the "Now" section (fed by the
    // snapshot), not "Recent work" — recentTasksAll filters to terminal
    // statuses only, so an in-progress task never reaches that list.
    const task: AgentTask = {
      id: "run-2",
      agent_id: "agent-1",
      runtime_id: "runtime-1",
      issue_id: "",
      status: "running",
      priority: 0,
      dispatched_at: "2026-09-04T00:00:00Z",
      started_at: "2026-09-04T00:00:00Z",
      completed_at: null,
      result: null,
      error: null,
      created_at: "2026-09-04T00:00:00Z",
    };
    snapshotRef.current = () => Promise.resolve([task]);
    agentTasksRef.current = () => Promise.resolve([]);
    renderTab();
    // Wait for the active-task row to settle (untracked: no issue, chat
    // session, or autopilot run) before asserting the action is absent.
    expect(await screen.findByText("Untracked")).toBeInTheDocument();
    expect(teachButton()).not.toBeInTheDocument();
  });
});
