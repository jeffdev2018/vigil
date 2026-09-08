// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, AgentTask } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockListAgentMemories = vi.hoisted(() => vi.fn());
const mockCreateAgentMemory = vi.hoisted(() => vi.fn());
const mockUpdateAgentMemory = vi.hoisted(() => vi.fn());
const mockDeleteAgentMemory = vi.hoisted(() => vi.fn());
const mockHistory = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      getAgentMemoryUsage: vi.fn().mockResolvedValue({since:"2026-08-06T00:00:00Z",until:"2026-09-05T00:00:00Z",started_runs:0,recorded_runs:0,unrecorded_runs:0,load_failed_runs:0,runs_with_agent_memory:0,versions:[]}),
      getAgentMemoryHistory: (...args: unknown[]) => mockHistory(...args),
      listAgentTasks: vi.fn().mockResolvedValue([]),
      listAgentMemories: (...args: unknown[]) => mockListAgentMemories(...args),
      createAgentMemory: (...args: unknown[]) => mockCreateAgentMemory(...args),
      updateAgentMemory: (...args: unknown[]) => mockUpdateAgentMemory(...args),
      deleteAgentMemory: (...args: unknown[]) => mockDeleteAgentMemory(...args),
    },
  };
});

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@multica/core/permissions", () => ({ useAgentPermissions: () => ({ canEdit: { allowed: true } }) }));
import { MemoryTab, TeachFromRunButton, TeachFromReviewButton } from "./memory-tab";

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderMemoryTab(canEdit = true) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <MemoryTab agent={agent} canEdit={canEdit} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("MemoryTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListAgentMemories.mockResolvedValue({ memories: [], briefed_count: 0, extraction_enabled: false });
    mockHistory.mockResolvedValue({ versions: [], next_before_revision: null });
    mockCreateAgentMemory.mockResolvedValue({
      id: "mem-new",
      agent_id: "agent-1",
      content: "New fact",
      source: "manual",
      source_task_id: null,
      created_at: "2026-09-04T00:00:00Z",
      updated_at: "2026-09-04T00:00:00Z",
    });
    mockDeleteAgentMemory.mockResolvedValue(undefined);
    mockUpdateAgentMemory.mockResolvedValue({});
  });

  it("renders the empty state when the agent has no memories", async () => {
    renderMemoryTab();

    expect(await screen.findByText("No memories yet")).toBeInTheDocument();
    expect(screen.getByText("0 / 200")).toBeInTheDocument();
  });

  it("keeps a correction candidate draft on failure and preserves evidence when the run is gone", async () => {
    const user = userEvent.setup();
    const wrapper = ({children}: {children: React.ReactNode}) => <I18nProvider locale="en" resources={TEST_RESOURCES}><QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}>{children}</QueryClientProvider></I18nProvider>;
    const proposal = render(<TeachFromReviewButton wsId="ws-1" agentId="agent-1" sourceTaskId="run-1" review={{id:"review-1",feedback:"The form lost the project."}} />, {wrapper});
    await user.click(screen.getByRole("button",{name:"Propose a memory"}));
    expect(screen.getByText("The form lost the project.")).toBeInTheDocument();
    const content = screen.getByRole("textbox",{name:"Memory"});
    await user.type(content,"Preserve the project in new forms.");
    mockCreateAgentMemory.mockRejectedValueOnce(new Error("temporary"));
    await user.click(screen.getByRole("button",{name:"Save"}));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(content).toHaveValue("Preserve the project in new forms.");
    await user.click(screen.getByRole("button",{name:"Save"}));
    expect(await screen.findByRole("status")).toHaveTextContent("Memory saved");
    expect(mockCreateAgentMemory).toHaveBeenLastCalledWith("agent-1","Preserve the project in new forms.","run-1",null,"review-1");
    expect(mockUpdateAgentMemory).not.toHaveBeenCalled();
    proposal.unmount();
    mockListAgentMemories.mockResolvedValue({ memories: [{id:"memory-1",agent_id:"agent-1",content:"Preserve the project in new forms.",source:"manual",source_task_id:"run-1",status:"pending",revision:1,
      source_review:{review_id:"review-1",issue_id:"issue-1",task_id:"run-1",feedback:"The form lost the project.",criteria:["Keep context"],assessments:[{passed:false,evidence:"Project field was empty."}],reviewed_by:"Jeff",reviewed_at:"2026-09-05T00:00:00Z"}}], briefed_count: 0, extraction_enabled: false })
    renderMemoryTab();
    await user.click(await screen.findByRole("button",{name:"View source run"}));
    expect(await screen.findByText("Project field was empty.")).toBeInTheDocument();
    expect(screen.getByText("The form lost the project.")).toBeInTheDocument();
  });

  it("lists memories with their source badge", async () => {
    mockListAgentMemories.mockResolvedValue({ memories: [
      {
        id: "mem-1",
        agent_id: "agent-1",
        content: "Prefers terse summaries",
        source: "manual",
        source_task_id: null,
        created_at: "2026-09-01T00:00:00Z",
        updated_at: "2026-09-01T00:00:00Z",
      },
      {
        id: "mem-2",
        agent_id: "agent-1",
        content: "Staging deploys on Fridays",
        source: "run",
        source_task_id: "task-9",
        created_at: "2026-09-02T00:00:00Z",
        updated_at: "2026-09-02T00:00:00Z",
      },
    ], briefed_count: 0, extraction_enabled: false })

    renderMemoryTab();

    expect(await screen.findByText("Prefers terse summaries")).toBeInTheDocument();
    expect(screen.getByText("Staging deploys on Fridays")).toBeInTheDocument();
    expect(screen.getByText("Manual")).toBeInTheDocument();
    expect(screen.getByText("From a run")).toBeInTheDocument();
    expect(screen.getByText("2 / 200")).toBeInTheDocument();
  });

  it("creates a memory from the add dialog", async () => {
    const user = userEvent.setup();
    renderMemoryTab();

    await user.click(
      await screen.findByRole("button", { name: /Add memory/i }),
    );
    const textarea = await screen.findByRole("textbox", { name: /Memory/i });
    await user.type(textarea, "Always run pnpm typecheck first");
    await user.click(screen.getByRole("button", { name: /^Save$/i }));

    expect(mockCreateAgentMemory).toHaveBeenCalledWith(
      "agent-1",
      "Always run pnpm typecheck first",
      undefined,
      null,
      undefined,
    );
  });

  it("deletes a memory after confirmation", async () => {
    const user = userEvent.setup();
    mockListAgentMemories.mockResolvedValue({ memories: [
      {
        id: "mem-1",
        agent_id: "agent-1",
        content: "Prefers terse summaries",
        source: "manual",
        source_task_id: null,
        created_at: "2026-09-01T00:00:00Z",
        updated_at: "2026-09-01T00:00:00Z",
      },
    ], briefed_count: 0, extraction_enabled: false })

    renderMemoryTab();

    await user.click(
      await screen.findByRole("button", { name: /Memory actions/i }),
    );
    await user.click(await screen.findByRole("menuitem", { name: /Delete/i }));
    await user.click(
      await screen.findByRole("button", { name: /^Delete$/i }),
    );

    expect(mockDeleteAgentMemory).toHaveBeenCalledWith("agent-1", "mem-1");
  });

  it("hides the add button and row actions when canEdit is false", async () => {
    mockListAgentMemories.mockResolvedValue({ memories: [
      {
        id: "mem-1",
        agent_id: "agent-1",
        content: "Prefers terse summaries",
        source: "manual",
        source_task_id: null,
        created_at: "2026-09-01T00:00:00Z",
        updated_at: "2026-09-01T00:00:00Z",
      },
    ], briefed_count: 0, extraction_enabled: false })

    renderMemoryTab(false);

    expect(await screen.findByText("Prefers terse summaries")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Add memory/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Memory actions/i }),
    ).not.toBeInTheDocument();
  });
  it("approves the displayed revision and lets a reader inspect without approving", async () => {
    const suggestion = { id: "mem-1", agent_id: "agent-1", content: "Use pnpm.", source: "run", status: "pending", revision: 7, source_task_id: null };
    mockListAgentMemories.mockResolvedValue({ memories: [suggestion], briefed_count: 0, extraction_enabled: false });
    const user = userEvent.setup();
    const view = renderMemoryTab();
    await user.click(await screen.findByRole("button", { name: /^Approve$/ }));
    expect(mockUpdateAgentMemory).toHaveBeenCalledWith("agent-1", "mem-1", { status: "active", expected_revision: 7 });
    view.unmount();
    renderMemoryTab(false);
    expect(await screen.findByText("Awaiting review")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Approve$/ })).not.toBeInTheDocument();
  });

  it("shows a load failure instead of claiming there are no memories", async () => {
    mockListAgentMemories.mockRejectedValue(new Error("offline"));
    renderMemoryTab();
    expect(await screen.findByRole("alert")).toHaveTextContent("Memories could not be loaded");
    expect(screen.queryByText("No memories yet")).not.toBeInTheDocument();
  });

  it("records a human correction with its source run", async () => {
    const user = userEvent.setup();
    const queryClient = new QueryClient();
    const task: AgentTask = {
      id: "run-1",
      agent_id: agent.id,
      runtime_id: "runtime-1",
      issue_id: "issue-1",
      status: "failed",
      priority: 0,
      dispatched_at: null,
      started_at: null,
      completed_at: "2026-09-04T00:00:00Z",
      result: null,
      error: "Wrong timezone",
      created_at: "2026-09-04T00:00:00Z",
    };
    render(<I18nProvider locale="en" resources={TEST_RESOURCES}><QueryClientProvider client={queryClient}>
      <TeachFromRunButton agent={agent} task={task} />
    </QueryClientProvider></I18nProvider>);
    await user.click(screen.getByRole("button", { name: "Teach a correction" }));
    await user.type(await screen.findByRole("textbox", { name: "Memory" }), "Use the project timezone.");
    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(mockCreateAgentMemory).toHaveBeenCalledWith("agent-1", "Use the project timezone.", "run-1", null, undefined);
  });

  it("previews a restoration, keeps it on error, and makes history readable without edit permission", async () => {
    const current = { id: "mem-1", agent_id: agent.id, content: "Current", source: "manual", status: "active", revision: 3, expires_at: null };
    const previous = { ...current, content: "Previous expired rule", status: "pending", revision: 1, expired: true, expires_at: "2026-01-02T00:00:00Z" };
    mockListAgentMemories.mockResolvedValue({ memories: [current], briefed_count: 0, extraction_enabled: false });
    mockHistory.mockResolvedValue({ versions: [current, previous], next_before_revision: null });
    mockUpdateAgentMemory.mockRejectedValueOnce(new Error("Unavailable"));
    const user = userEvent.setup(); const view = renderMemoryTab();
    await user.click(await screen.findByRole("button", { name: "History" }));
    await user.click(await screen.findByRole("button", { name: "Restore version 1" }));
    expect(mockUpdateAgentMemory).not.toHaveBeenCalled();
    expect(screen.getByText("Previous expired rule")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /^Restore$/ }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(screen.getByText("Previous expired rule")).toBeInTheDocument();
    expect(mockUpdateAgentMemory).toHaveBeenCalledWith("agent-1", "mem-1", { restore_revision: 1, expected_revision: 3 });
    await user.click(screen.getByRole("button", { name: /^Restore$/ }));
    expect(await screen.findByRole("button", { name: "Restore version 1" })).toBeInTheDocument();
    view.unmount(); renderMemoryTab(false);
    await user.click(await screen.findByRole("button", { name: "History" }));
    expect(await screen.findByText("Previous expired rule")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Restore/ })).not.toBeInTheDocument();
  });

  it("keeps an expired lesson inactive when its text is edited without changing the date", async () => {
    mockListAgentMemories.mockResolvedValue({ memories: [{ id: "mem-expired", agent_id: agent.id, content: "Old endpoint", source: "manual", source_task_id: null,
      created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z", status: "active", revision: 3, expires_at: "2026-01-02T00:00:00Z", expired: true }], briefed_count: 0, extraction_enabled: false })
    const user = userEvent.setup(); renderMemoryTab();
    expect(await screen.findByText("Expired")).toBeInTheDocument();
    expect(screen.queryByText("Active", { exact: true })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Stop using" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /actions/i }));
    await user.click(await screen.findByRole("menuitem", { name: /Edit/i }));
    const input=screen.getByRole("textbox", { name: /Memory/i });
    await user.clear(input); await user.type(input,"Corrected endpoint description");
    await user.click(screen.getByRole("button", { name: /^Save$/i }));
    expect(mockUpdateAgentMemory).toHaveBeenCalledWith("agent-1", "mem-expired", { content: "Corrected endpoint description", expected_revision: 3, expires_at: undefined });
  });

});
