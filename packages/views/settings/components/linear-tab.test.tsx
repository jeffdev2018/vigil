// @vitest-environment jsdom

import { type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

// LinearTab's three states and its admin gate. The schema matrix behind the
// installation shape lives in packages/core/linear/schemas.test.ts.

type MemberRole = "owner" | "admin" | "member";

const membersRef = vi.hoisted(() => ({
  current: [{ user_id: "user-1", role: "owner" as MemberRole }],
}));
const agentsRef = vi.hoisted(() => ({ current: [{ id: "agent-1", name: "Bridge" }] }));
const statusesRef = vi.hoisted(() => ({
  current: [
    { key: "todo", name: "Todo", archived_at: null },
    { key: "in_progress", name: "In Progress", archived_at: null },
    { key: "done", name: "Done", archived_at: null },
  ],
}));
const installationRef = vi.hoisted(() => ({
  current: null as Record<string, unknown> | null,
  isLoading: false,
  isError: false,
}));

const mockStartOAuth = vi.hoisted(() => vi.fn());
const mockDisconnect = vi.hoisted(() => vi.fn());
const mockSaveStatusMap = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("members")) return { data: membersRef.current, isLoading: false };
    if (key.includes("agents")) return { data: agentsRef.current, isLoading: false };
    if (key.includes("statuses")) return { data: statusesRef.current, isLoading: false };
    if (key.includes("linear-installation")) {
      return {
        data: installationRef.current,
        isLoading: installationRef.isLoading,
        isError: installationRef.isError,
      };
    }
    return { data: undefined, isLoading: false };
  },
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/issue-statuses/queries", () => ({
  issueStatusListOptions: () => ({ queryKey: ["issue-statuses"], queryFn: vi.fn() }),
}));

vi.mock("@multica/core/linear", () => ({
  linearInstallationOptions: () => ({ queryKey: ["linear-installation"], queryFn: vi.fn() }),
  DEFAULT_LINEAR_STATUS_MAP: {
    triage: "todo",
    backlog: "backlog",
    unstarted: "todo",
    started: "in_progress",
    completed: "done",
    canceled: "cancelled",
  },
  LINEAR_STATE_TYPES: ["triage", "backlog", "unstarted", "started", "completed", "canceled"],
  useStartLinearOAuth: () => ({ mutateAsync: mockStartOAuth, isPending: false }),
  useDisconnectLinear: () => ({ mutateAsync: mockDisconnect, isPending: false }),
  useUpdateLinearStatusMap: () => ({ mutateAsync: mockSaveStatusMap, isPending: false }),
}));

vi.mock("@multica/core/auth", () => {
  const useAuthStore = Object.assign(
    (sel?: (s: { user: { id: string } }) => unknown) =>
      sel ? sel({ user: { id: "user-1" } }) : { user: { id: "user-1" } },
    { getState: () => ({ user: { id: "user-1" } }) },
  );
  return { useAuthStore };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() } }));

import { LinearTab } from "./linear-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

afterEach(cleanup);

function renderUI(children: ReactNode) {
  return render(<I18nProvider locale="en" resources={TEST_RESOURCES}>{children}</I18nProvider>);
}

function connectedInstallation(over: Record<string, unknown> = {}) {
  return {
    id: "inst-1",
    connected: true,
    configured: true,
    linear_org_id: "org_1",
    linear_org_name: "Acme",
    agent_id: "agent-1",
    agent_name: "Bridge",
    status: "active",
    last_error: "",
    status_map: { started: "in_progress", completed: "done" },
    installed_by: "user-1",
    created_at: "",
    updated_at: "",
    linear_state_types: ["started", "completed"],
    ...over,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  membersRef.current = [{ user_id: "user-1", role: "owner" }];
  agentsRef.current = [{ id: "agent-1", name: "Bridge" }];
  installationRef.current = null;
  installationRef.isLoading = false;
  installationRef.isError = false;
});

describe("LinearTab", () => {
  it("says the deployment has no Linear app when nothing is configured", () => {
    installationRef.current = { connected: false, configured: false, status_map: {}, linear_state_types: [] };
    renderUI(<LinearTab />);
    expect(screen.getByText(enSettings.linear.not_enabled_title)).toBeTruthy();
    expect(screen.queryByTestId("linear-connect")).toBeNull();
  });

  it("offers the agent picker and navigates to the authorize URL", async () => {
    installationRef.current = { connected: false, configured: true, status_map: {}, linear_state_types: [] };
    mockStartOAuth.mockResolvedValue("https://linear.app/oauth/authorize?client_id=cid");
    const assign = vi.fn();
    Object.defineProperty(window, "location", { value: { href: "", assign }, writable: true });

    renderUI(<LinearTab />);
    await userEvent.click(screen.getByRole("combobox"));
    await userEvent.click(await screen.findByRole("option", { name: "Bridge" }));
    await userEvent.click(screen.getByRole("button", { name: enSettings.linear.connect }));

    expect(mockStartOAuth).toHaveBeenCalledWith({ agentId: "agent-1", redirect: "/settings/integrations" });
  });

  it("hides the connect controls from a plain member", () => {
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    installationRef.current = { connected: false, configured: true, status_map: {}, linear_state_types: [] };
    renderUI(<LinearTab />);
    expect(screen.getByText(enSettings.linear.admin_only)).toBeTruthy();
    expect(screen.queryByRole("button", { name: enSettings.linear.connect })).toBeNull();
  });

  it("shows the org, the agent and the status map once connected", () => {
    installationRef.current = connectedInstallation();
    renderUI(<LinearTab />);
    expect(screen.getByTestId("linear-connected")).toBeTruthy();
    expect(screen.getByText("Acme")).toBeTruthy();
    expect(screen.getByText("Bridge")).toBeTruthy();
    // The server's own state-type list drives the editor rows.
    expect(screen.getByLabelText("started")).toBeTruthy();
    expect(screen.getByLabelText("completed")).toBeTruthy();
  });

  it("warns and offers a reconnect when Linear rejected the token", () => {
    installationRef.current = connectedInstallation({ status: "broken", last_error: "token rejected" });
    renderUI(<LinearTab />);
    expect(screen.getByText(enSettings.linear.broken_title)).toBeTruthy();
    expect(screen.getByText("token rejected")).toBeTruthy();
    expect(screen.getByRole("button", { name: enSettings.linear.reconnect })).toBeTruthy();
  });

  it("disconnects only after the confirm dialog is accepted", async () => {
    installationRef.current = connectedInstallation();
    mockDisconnect.mockResolvedValue(undefined);
    renderUI(<LinearTab />);

    await userEvent.click(screen.getByRole("button", { name: enSettings.linear.disconnect }));
    expect(mockDisconnect).not.toHaveBeenCalled();

    const confirm = await screen.findByText(enSettings.linear.disconnect_confirm_title);
    expect(confirm).toBeTruthy();
    const buttons = screen.getAllByRole("button", { name: enSettings.linear.disconnect });
    await userEvent.click(buttons[buttons.length - 1]!);
    expect(mockDisconnect).toHaveBeenCalledTimes(1);
  });
});
