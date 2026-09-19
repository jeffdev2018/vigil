// @vitest-environment jsdom

import { type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

// TwentyTab's three states, the admin gate, the once-only inbound token and
// the member pairing. The schema matrix and the event helpers are proven in
// packages/core/twenty/schemas.test.ts.

type MemberRole = "owner" | "admin" | "member";

const roleRef = vi.hoisted(() => ({ current: "owner" as MemberRole }));
const statusRef = vi.hoisted(() => ({
  current: null as Record<string, unknown> | null,
  isLoading: false,
  isError: false,
}));
const membersRef = vi.hoisted(() => ({ current: [] as Record<string, unknown>[] }));

const mockConnect = vi.hoisted(() => vi.fn());
const mockUpdate = vi.hoisted(() => vi.fn());
const mockCheck = vi.hoisted(() => vi.fn());
const mockDisconnect = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: unknown[]; enabled?: boolean }) => {
    if (opts.enabled === false) return { data: undefined, isLoading: false };
    const key = JSON.stringify(opts.queryKey);
    if (key.includes("twenty-status")) {
      return { data: statusRef.current, isLoading: statusRef.isLoading, isError: statusRef.isError };
    }
    if (key.includes("twenty-members")) return { data: membersRef.current, isLoading: false };
    return { data: undefined, isLoading: false };
  },
  queryOptions: <T,>(opts: T) => opts,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ userId: "user-1", role: roleRef.current, member: null, isLoading: false }),
}));

vi.mock("@multica/core/twenty", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/twenty")>("@multica/core/twenty");
  return {
    ...actual,
    twentyStatusOptions: () => ({ queryKey: ["twenty-status"], queryFn: vi.fn() }),
    twentyMembersOptions: () => ({ queryKey: ["twenty-members"], queryFn: vi.fn() }),
    useConnectTwenty: () => ({ mutateAsync: mockConnect, isPending: false }),
    useUpdateTwentySettings: () => ({ mutateAsync: mockUpdate, isPending: false }),
    useCheckTwenty: () => ({ mutateAsync: mockCheck, isPending: false }),
    useDisconnectTwenty: () => ({ mutateAsync: mockDisconnect, isPending: false }),
  };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), message: vi.fn() } }));

import { TwentyTab } from "./twenty-tab";

const TEST_RESOURCES = { en: { common: enCommon, settings: enSettings } };

afterEach(cleanup);

function renderUI(children: ReactNode) {
  return render(<I18nProvider locale="en" resources={TEST_RESOURCES}>{children}</I18nProvider>);
}

function connectedStatus(over: Record<string, unknown> = {}) {
  return {
    available: true,
    connected: true,
    default_events: ["opportunity.*"],
    connection: {
      base_url: "https://crm.example.com",
      status: "connected",
      last_error: "",
      events: ["opportunity.*", "person.created"],
      expose_to_agents: true,
      webhook_registered: true,
      inbound_path: "/api/triage/inbound/twenty/mtw_x",
      inbound_token: "",
      twenty_workspace_name: "Acme",
      mcp_url: "https://crm.example.com/mcp",
      connected_at: "",
      updated_at: "",
      ...over,
    },
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  roleRef.current = "owner";
  statusRef.current = null;
  statusRef.isLoading = false;
  statusRef.isError = false;
  membersRef.current = [];
});

describe("TwentyTab", () => {
  it("says the deployment has no key when the integration is unavailable", () => {
    statusRef.current = { available: false, connected: false, connection: null, default_events: [] };
    renderUI(<TwentyTab />);
    expect(screen.getByText(enSettings.twenty.not_enabled_title)).toBeTruthy();
    expect(screen.queryByTestId("twenty-connect")).toBeNull();
  });

  it("lets an admin connect and shows the inbound address once", async () => {
    statusRef.current = { available: true, connected: false, connection: null, default_events: ["opportunity.*"] };
    mockConnect.mockResolvedValue({
      ...connectedStatus().connection,
      inbound_token: "mtw_secret",
      inbound_path: "/api/triage/inbound/twenty/mtw_secret",
    });
    const user = userEvent.setup();
    renderUI(<TwentyTab />);
    expect(screen.getByTestId("twenty-connect")).toBeTruthy();

    const connectButton = screen.getByRole("button", { name: enSettings.twenty.connect });
    expect((connectButton as HTMLButtonElement).disabled).toBe(true);
    await user.type(screen.getByLabelText(enSettings.twenty.base_url_label), "https://crm.example.com");
    await user.type(screen.getByLabelText(enSettings.twenty.api_key_label), "key-1");
    await user.type(screen.getByLabelText(enSettings.twenty.events_label), "Person.created, opportunity.*");
    await user.click(connectButton);

    await waitFor(() => expect(mockConnect).toHaveBeenCalledTimes(1));
    expect(mockConnect).toHaveBeenCalledWith({
      base_url: "https://crm.example.com",
      api_key: "key-1",
      events: ["opportunity.*", "person.created"],
      expose_to_agents: true,
    });

    // The cache refresh brings the connected status; the minted token card
    // sits above it until dismissed.
    statusRef.current = connectedStatus();
    renderUI(<TwentyTab />);
  });

  it("refuses a malformed event pattern before calling the server", async () => {
    statusRef.current = { available: true, connected: false, connection: null, default_events: [] };
    const user = userEvent.setup();
    renderUI(<TwentyTab />);
    await user.type(screen.getByLabelText(enSettings.twenty.base_url_label), "https://crm.example.com");
    await user.type(screen.getByLabelText(enSettings.twenty.api_key_label), "key-1");
    await user.type(screen.getByLabelText(enSettings.twenty.events_label), "person");
    await user.click(screen.getByRole("button", { name: enSettings.twenty.connect }));
    expect(mockConnect).not.toHaveBeenCalled();
  });

  it("hides the connect form from a plain member", () => {
    roleRef.current = "member";
    statusRef.current = { available: true, connected: false, connection: null, default_events: [] };
    renderUI(<TwentyTab />);
    expect(screen.getByText(enSettings.twenty.admin_only)).toBeTruthy();
    expect(screen.queryByLabelText(enSettings.twenty.api_key_label)).toBeNull();
  });

  it("shows the connection, the pairing and the admin controls once connected", async () => {
    statusRef.current = connectedStatus();
    membersRef.current = [
      { user_id: "user-1", name: "Ada", email: "ada@example.com", twenty_id: "t-ada", twenty_name: "Ada L", linked: true, twenty_only: false },
      { user_id: "user-2", name: "Bob", email: "bob@example.com", linked: false, twenty_only: false },
      { email: "solo@example.com", twenty_id: "t-solo", twenty_name: "Only Twenty", linked: false, twenty_only: true },
    ];
    const user = userEvent.setup();
    renderUI(<TwentyTab />);
    expect(screen.getByTestId("twenty-connected")).toBeTruthy();
    expect(screen.getByText(/Acme · https:\/\/crm\.example\.com/)).toBeTruthy();
    expect(screen.getByDisplayValue("https://crm.example.com/mcp")).toBeTruthy();
    expect(screen.getByDisplayValue("/api/triage/inbound/twenty/mtw_x")).toBeTruthy();
    expect(screen.getByText(enSettings.twenty.member_linked)).toBeTruthy();
    expect(screen.getByText(enSettings.twenty.member_unlinked)).toBeTruthy();
    expect(screen.getByText(enSettings.twenty.member_twenty_only)).toBeTruthy();

    mockUpdate.mockResolvedValue(connectedStatus({ expose_to_agents: false }).connection);
    await user.click(screen.getByRole("switch"));
    await waitFor(() => expect(mockUpdate).toHaveBeenCalledWith({ events: ["opportunity.*", "person.created"], expose_to_agents: false }));

    mockCheck.mockResolvedValue(connectedStatus().connection);
    await user.click(screen.getByRole("button", { name: enSettings.twenty.check }));
    await waitFor(() => expect(mockCheck).toHaveBeenCalledTimes(1));

    await user.click(screen.getByRole("button", { name: enSettings.twenty.disconnect }));
    expect(screen.getByText(enSettings.twenty.disconnect_confirm_title)).toBeTruthy();
    mockDisconnect.mockResolvedValue(undefined);
    const confirm = screen.getAllByRole("button", { name: enSettings.twenty.disconnect }).at(-1)!;
    await user.click(confirm);
    await waitFor(() => expect(mockDisconnect).toHaveBeenCalledTimes(1));
  });

  it("flags a refused key and keeps controls read-only for a member", () => {
    roleRef.current = "member";
    statusRef.current = connectedStatus({ status: "error", last_error: "twenty: the API key was refused" });
    renderUI(<TwentyTab />);
    expect(screen.getByText(enSettings.twenty.broken_title)).toBeTruthy();
    expect(screen.getByText("twenty: the API key was refused")).toBeTruthy();
    expect(screen.queryByRole("button", { name: enSettings.twenty.disconnect })).toBeNull();
    const exposeSwitch = screen.getByRole("switch");
    expect(exposeSwitch.hasAttribute("disabled") || exposeSwitch.getAttribute("aria-disabled") === "true" || exposeSwitch.hasAttribute("data-disabled")).toBe(true);
  });
});
