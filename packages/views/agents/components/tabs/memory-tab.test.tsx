// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockListAgentMemories = vi.hoisted(() => vi.fn());
const mockCreateAgentMemory = vi.hoisted(() => vi.fn());
const mockUpdateAgentMemory = vi.hoisted(() => vi.fn());
const mockDeleteAgentMemory = vi.hoisted(() => vi.fn());

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

import { MemoryTab } from "./memory-tab";

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
    mockListAgentMemories.mockResolvedValue([]);
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
  });

  it("renders the empty state when the agent has no memories", async () => {
    renderMemoryTab();

    expect(await screen.findByText("No memories yet")).toBeInTheDocument();
    expect(screen.getByText("0 / 200")).toBeInTheDocument();
  });

  it("lists memories with their source badge", async () => {
    mockListAgentMemories.mockResolvedValue([
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
    ]);

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
    );
  });

  it("deletes a memory after confirmation", async () => {
    const user = userEvent.setup();
    mockListAgentMemories.mockResolvedValue([
      {
        id: "mem-1",
        agent_id: "agent-1",
        content: "Prefers terse summaries",
        source: "manual",
        source_task_id: null,
        created_at: "2026-09-01T00:00:00Z",
        updated_at: "2026-09-01T00:00:00Z",
      },
    ]);

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
    mockListAgentMemories.mockResolvedValue([
      {
        id: "mem-1",
        agent_id: "agent-1",
        content: "Prefers terse summaries",
        source: "manual",
        source_task_id: null,
        created_at: "2026-09-01T00:00:00Z",
        updated_at: "2026-09-01T00:00:00Z",
      },
    ]);

    renderMemoryTab(false);

    expect(await screen.findByText("Prefers terse summaries")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Add memory/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /Memory actions/i }),
    ).not.toBeInTheDocument();
  });
});
