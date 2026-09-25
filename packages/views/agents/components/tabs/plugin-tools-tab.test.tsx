// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import type { ReactNode } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, AgentPluginTool } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

// PluginToolsTab reads its hook list from one query and writes through
// bind/unbind mutations. Stub all three at the module boundary so the tests
// assert the tab's own logic (grouping, toggle direction, disabled state)
// rather than the query/mutation plumbing, which is covered in
// packages/core/plugins.
const toolsRef = vi.hoisted(() => ({ current: [] as AgentPluginTool[] }));
const queryStateRef = vi.hoisted(() => ({ isLoading: false, isError: false }));
const bindSpy = vi.hoisted(() => vi.fn());
const unbindSpy = vi.hoisted(() => vi.fn());
const isPendingRef = vi.hoisted(() => ({ current: false }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => {
    if (queryStateRef.isLoading) return { data: undefined, isLoading: true, isError: false };
    if (queryStateRef.isError) return { data: undefined, isLoading: false, isError: true };
    return { data: toolsRef.current, isLoading: false, isError: false };
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/plugins", () => ({
  agentPluginToolsOptions: (wsId: string, agentId: string) => ({
    queryKey: ["workspaces", wsId, "plugins", "agent-tools", agentId],
  }),
  useBindAgentPluginTool: () => ({ mutate: bindSpy, isPending: isPendingRef.current }),
  useUnbindAgentPluginTool: () => ({ mutate: unbindSpy, isPending: isPendingRef.current }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ settings: () => "/ws/settings" }),
}));

vi.mock("../../../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: ReactNode }) => (
    <a href={href} data-testid="app-link">
      {children}
    </a>
  ),
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { PluginToolsTab } from "./plugin-tools-tab";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const baseAgent: Agent = {
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
  permission_mode: "private",
  invocation_targets: [],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-06-30T00:00:00Z",
  updated_at: "2026-06-30T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderTab(canEdit = true) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <PluginToolsTab agent={baseAgent} canEdit={canEdit} />
    </I18nProvider>,
  );
}

function tool(overrides: Partial<AgentPluginTool> = {}): AgentPluginTool {
  return {
    installation_id: "installation-1",
    plugin_key: "com.example.hello",
    hook_key: "digest",
    name: "Digest",
    description: "Summarizes the issue",
    transport: "http",
    bound: false,
    ...overrides,
  };
}

describe("PluginToolsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    toolsRef.current = [];
    queryStateRef.isLoading = false;
    queryStateRef.isError = false;
    isPendingRef.current = false;
  });

  it("renders tools grouped by plugin with their bound state", () => {
    toolsRef.current = [
      tool({ hook_key: "digest", name: "Digest", bound: true }),
      tool({ hook_key: "notify", name: "Notify", bound: false }),
    ];

    renderTab();

    expect(screen.getByText("com.example.hello")).toBeTruthy();
    const digest = screen.getByLabelText(/Grant Digest to this agent/i);
    const notify = screen.getByLabelText(/Grant Notify to this agent/i);
    expect(digest.getAttribute("aria-checked")).toBe("true");
    expect(notify.getAttribute("aria-checked")).toBe("false");
  });

  it("toggling an unbound tool calls bind, not unbind", async () => {
    const user = userEvent.setup();
    toolsRef.current = [tool({ bound: false })];

    renderTab();
    await user.click(screen.getByLabelText(/Grant Digest to this agent/i));

    expect(bindSpy).toHaveBeenCalledTimes(1);
    expect(bindSpy.mock.calls[0]?.[0]).toEqual({
      installationId: "installation-1",
      hookKey: "digest",
    });
    expect(unbindSpy).not.toHaveBeenCalled();
  });

  it("toggling a bound tool calls unbind, not bind", async () => {
    const user = userEvent.setup();
    toolsRef.current = [tool({ bound: true })];

    renderTab();
    await user.click(screen.getByLabelText(/Grant Digest to this agent/i));

    expect(unbindSpy).toHaveBeenCalledTimes(1);
    expect(bindSpy).not.toHaveBeenCalled();
  });

  it("disables every checkbox when canEdit is false", () => {
    toolsRef.current = [tool()];
    renderTab(false);

    expect(
      screen.getByLabelText(/Grant Digest to this agent/i).getAttribute("aria-disabled"),
    ).toBe("true");
  });

  it("shows an empty state with a Settings link when there are no tools", () => {
    renderTab();

    expect(screen.getByText(/No plugin tools available/i)).toBeTruthy();
    const link = screen.getByTestId("app-link");
    expect(link.getAttribute("href")).toBe("/ws/settings?tab=plugins");
  });

  it("shows the loading state", () => {
    queryStateRef.isLoading = true;
    renderTab();
    expect(screen.getByText(/Loading plugin tools/i)).toBeTruthy();
  });

  it("shows the error state", () => {
    queryStateRef.isError = true;
    renderTab();
    expect(screen.getByText(/Couldn't load plugin tools/i)).toBeTruthy();
  });
});
