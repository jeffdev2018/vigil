// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, McpCatalogTool, WorkspaceMcpServer } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The search / filter matrix is tested once, on the pure helper:
// packages/core/workspace/tool-catalog.test.ts. This suite covers the page's
// own wiring — the states, the debounce, the clear button, the attach dialog,
// and the promise that no credential material can reach the DOM.

const state = vi.hoisted(() => ({
  servers: [] as WorkspaceMcpServer[],
  serversLoading: false,
  serversError: false,
  tools: new Map<string, McpCatalogTool[]>(),
  toolsError: false,
  agents: [] as Agent[],
  agentsLoading: false,
  attachPending: false,
  attachFails: false,
}));

const attachCalls = vi.hoisted(() => ({ current: [] as unknown[] }));
const toasts = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceMcpServersOptions: () => ({ queryKey: ["servers"] }),
  workspaceMcpServerToolsOptions: (_ws: string, serverId: string) => ({
    queryKey: ["tools", serverId],
  }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));
vi.mock("@multica/core/workspace/mutations", () => ({
  useAttachMcpServerToAgent: () => ({
    mutate: (
      variables: { agentId: string; serverId: string },
      handlers?: { onSuccess?: () => void; onError?: () => void },
    ) => {
      attachCalls.current.push(variables);
      if (state.attachFails) handlers?.onError?.();
      else handlers?.onSuccess?.();
    },
    isPending: state.attachPending,
    variables: undefined,
  }),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => {
    const [kind, id] = options.queryKey as [string, string?];
    if (kind === "servers")
      return {
        data: state.servers,
        isLoading: state.serversLoading,
        isError: state.serversError,
        refetch: vi.fn(),
      };
    if (kind === "agents")
      return {
        data: state.agents,
        isLoading: state.agentsLoading,
        isError: false,
        refetch: vi.fn(),
      };
    return toolQueryFor(id);
  },
  useQueries: ({ queries }: { queries: { queryKey: readonly unknown[] }[] }) =>
    queries.map((query) => toolQueryFor((query.queryKey as [string, string])[1])),
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ href, children }: { href: string; children: ReactNode }) => (
    <a href={href}>{children}</a>
  ),
}));
vi.mock("sonner", () => ({ toast: toasts }));

function toolQueryFor(serverId: string | undefined) {
  return {
    data: serverId ? { tools: state.tools.get(serverId) ?? [] } : undefined,
    isLoading: false,
    isError: state.toolsError,
    refetch: vi.fn(),
  };
}

import { ToolsPage } from "./tools-page";

const SECRET = "sk-live-must-never-render";

function server(over: Partial<WorkspaceMcpServer>): WorkspaceMcpServer {
  return {
    id: "srv-1",
    workspace_id: "ws-1",
    name: "linear",
    transport: "http",
    tool_count: 0,
    created_at: "",
    updated_at: "",
    ...over,
  };
}

function agent(id: string, name: string, over: Partial<Agent> = {}): Agent {
  return { id, name, workspace_id: "ws-1", ...over } as Agent;
}

const linear = server({ id: "srv-1", name: "linear", agent_count: 2, agent_ids: ["a-1", "a-2"] });
const stripe = server({ id: "srv-2", name: "stripe", agent_count: 0, agent_ids: [] });

function seedTwoServers() {
  state.servers = [linear, stripe];
  state.tools = new Map([
    [
      "srv-1",
      [
        { name: "list_issues", description: "Liste les tâches créées", risk: "read", risk_source: "auto" },
        { name: "create_issue", risk: "internal_write", risk_source: "auto" },
      ] as McpCatalogTool[],
    ],
    [
      "srv-2",
      [
        { name: "refund", description: "Refunds a charge", risk: "external_effect", risk_source: "manual" },
      ] as McpCatalogTool[],
    ],
  ]);
}

beforeEach(() => {
  state.servers = [];
  state.serversLoading = false;
  state.serversError = false;
  state.tools = new Map();
  state.toolsError = false;
  state.agents = [];
  state.agentsLoading = false;
  state.attachPending = false;
  state.attachFails = false;
  attachCalls.current = [];
  toasts.success.mockReset();
  toasts.error.mockReset();
});

afterEach(cleanup);

describe("ToolsPage catalogue", () => {
  it("lists every server's tools with its origin, risk and agent count", () => {
    seedTwoServers();
    renderWithI18n(<ToolsPage />);

    const rows = screen.getAllByRole("row").slice(1);
    expect(rows).toHaveLength(3);
    expect(within(rows[0]!).getByText("list_issues")).toBeTruthy();
    expect(within(rows[0]!).getByText("linear")).toBeTruthy();
    expect(within(rows[0]!).getByText("Read")).toBeTruthy();
    expect(within(rows[0]!).getByText("2")).toBeTruthy();
    // A tool with no description renders its row anyway.
    expect(within(rows[1]!).getByText("create_issue")).toBeTruthy();
    expect(within(rows[2]!).getByText("External effect")).toBeTruthy();
    expect(within(rows[2]!).getByText("0")).toBeTruthy();
  });

  it("never renders credential material, even when the API regresses", () => {
    seedTwoServers();
    // A backend that started echoing the stored entry must not reach the DOM.
    state.servers = [
      { ...linear, url: SECRET, headers: { Authorization: SECRET } } as WorkspaceMcpServer,
      stripe,
    ];
    const { container } = renderWithI18n(<ToolsPage />);

    expect(container.textContent).not.toContain(SECRET);
    expect(container.textContent).not.toContain("Authorization");
  });

  it("shows a skeleton while loading", () => {
    state.serversLoading = true;
    renderWithI18n(<ToolsPage />);

    expect(screen.getByRole("status")).toHaveAttribute("aria-label", "Loading tools…");
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("reports a failure with a retry instead of a partial catalogue", async () => {
    seedTwoServers();
    state.toolsError = true;
    renderWithI18n(<ToolsPage />);

    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.getByText("Could not load the workspace tools.")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy();
    expect(screen.queryByRole("table")).toBeNull();
  });

  it("points at MCP settings when the workspace has no server", () => {
    renderWithI18n(<ToolsPage />);

    expect(screen.getByText("No MCP server in this workspace")).toBeTruthy();
    expect(screen.getByRole("link")).toHaveAttribute("href", "/acme/settings?tab=mcp");
  });

  it("distinguishes an empty catalogue from an empty search", async () => {
    state.servers = [stripe];
    state.tools = new Map([["srv-2", []]]);
    const user = userEvent.setup();
    renderWithI18n(<ToolsPage />);

    expect(screen.getByText("No tool catalogued yet")).toBeTruthy();

    cleanup();
    seedTwoServers();
    renderWithI18n(<ToolsPage />);
    await user.type(screen.getByLabelText("Search tools"), "nothing matches this");
    expect(await screen.findByText("No tool matches")).toBeTruthy();
  });
});

describe("ToolsPage search and filters", () => {
  // Real timers on purpose: driving Base UI + userEvent through vitest's fake
  // timers hangs the interaction, and the debounce window (200ms) is long
  // enough to observe directly.
  it("debounces the search: the table only follows once typing settles", async () => {
    seedTwoServers();
    const user = userEvent.setup();
    renderWithI18n(<ToolsPage />);

    await user.type(screen.getByLabelText("Search tools"), "refund");
    // The keystrokes landed but the debounce has not elapsed, so the table is
    // still the whole catalogue.
    expect(screen.getAllByRole("row").slice(1)).toHaveLength(3);

    await waitFor(() =>
      expect(screen.getAllByRole("row").slice(1)).toHaveLength(1),
    );
    expect(screen.getByText("refund")).toBeTruthy();
  });

  it("matches an accented description from unaccented input", async () => {
    seedTwoServers();
    const user = userEvent.setup();
    renderWithI18n(<ToolsPage />);

    await user.type(screen.getByLabelText("Search tools"), "taches creees");
    await waitFor(() =>
      expect(screen.getAllByRole("row").slice(1)).toHaveLength(1),
    );
    expect(screen.getByText("list_issues")).toBeTruthy();
  });

  it("cumulates the server and risk filters, and clears them all at once", async () => {
    seedTwoServers();
    const user = userEvent.setup();
    renderWithI18n(<ToolsPage />);

    await user.click(screen.getByRole("combobox", { name: "Filter by server" }));
    await user.click(await screen.findByRole("option", { name: "linear" }));
    await waitFor(() =>
      expect(screen.getAllByRole("row").slice(1)).toHaveLength(2),
    );

    await user.click(screen.getByRole("combobox", { name: "Filter by risk" }));
    await user.click(await screen.findByRole("option", { name: "Internal write" }));
    await waitFor(() =>
      expect(screen.getAllByRole("row").slice(1)).toHaveLength(1),
    );
    expect(screen.getByText("create_issue")).toBeTruthy();

    await user.click(screen.getByRole("button", { name: "Clear filters" }));
    await waitFor(() =>
      expect(screen.getAllByRole("row").slice(1)).toHaveLength(3),
    );
  });
});

describe("ToolsPage add-to-agent dialog", () => {
  const openDialog = async () => {
    seedTwoServers();
    state.agents = [
      agent("a-1", "Ada"),
      agent("a-3", "Grace"),
      agent("a-4", "Retired", { archived_at: "2026-01-01T00:00:00Z" }),
    ];
    const user = userEvent.setup();
    renderWithI18n(<ToolsPage />);
    await user.click(
      screen.getAllByRole("button", { name: "Add linear to an agent" })[0]!,
    );
    return user;
  };

  it("offers the workspace's live agents and marks the one that already has it", async () => {
    await openDialog();

    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByText("Ada")).toBeTruthy();
    // Already bound (agent_ids), so there is nothing to click for Ada.
    expect(within(dialog).getByText("Already has it")).toBeTruthy();
    expect(
      within(dialog).queryByRole("button", { name: "Add linear to Ada" }),
    ).toBeNull();
    expect(
      within(dialog).getByRole("button", { name: "Add linear to Grace" }),
    ).toBeTruthy();
    // An archived agent cannot run, so it is not offered.
    expect(within(dialog).queryByText("Retired")).toBeNull();
  });

  it("attaches the server, toasts, and stays open to do the next one", async () => {
    const user = await openDialog();

    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Add linear to Grace" }),
    );

    expect(attachCalls.current).toEqual([{ agentId: "a-3", serverId: "srv-1" }]);
    expect(toasts.success).toHaveBeenCalledWith("linear added to Grace");
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("keeps the dialog and the list on a failure", async () => {
    state.attachFails = true;
    const user = await openDialog();

    const dialog = await screen.findByRole("dialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Add linear to Grace" }),
    );

    expect(toasts.error).toHaveBeenCalledWith("Could not add linear to Grace");
    expect(toasts.success).not.toHaveBeenCalled();
    expect(
      within(screen.getByRole("dialog")).getByRole("button", {
        name: "Add linear to Grace",
      }),
    ).toBeTruthy();
  });

  it("searches the agent list", async () => {
    const user = await openDialog();

    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByLabelText("Search agents"), "grac");
    expect(within(dialog).queryByText("Ada")).toBeNull();
    expect(within(dialog).getByText("Grace")).toBeTruthy();
  });
});
