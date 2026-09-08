// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";

const mockPut = vi.hoisted(() => vi.fn());
const mockEnforce = vi.hoisted(() => vi.fn());
const mockDelete = vi.hoisted(() => vi.fn());
const mockCreateToken = vi.hoisted(() => vi.fn());
const mockDeleteToken = vi.hoisted(() => vi.fn());

const data = vi.hoisted(() => ({
  sso: undefined as Record<string, unknown> | undefined,
  tokens: { tokens: [] as Record<string, unknown>[] },
  role: "owner" as "owner" | "admin" | "member",
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) =>
    options.queryKey[0] === "workspace-sso"
      ? { data: data.sso, isLoading: false }
      : { data: data.tokens, isLoading: false },
}));

vi.mock("@multica/core/access", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/access")>()),
  ssoConnectionOptions: (wsId: string) => ({ queryKey: ["workspace-sso", wsId] }),
  scimTokensOptions: (wsId: string) => ({ queryKey: ["workspace-scim-tokens", wsId] }),
  usePutSSOConnection: () => ({ mutateAsync: mockPut, isPending: false }),
  useSetSSOEnforced: () => ({ mutateAsync: mockEnforce, isPending: false }),
  useDeleteSSOConnection: () => ({ mutateAsync: mockDelete, isPending: false }),
  useCreateScimToken: () => ({ mutateAsync: mockCreateToken, isPending: false }),
  useDeleteScimToken: () => ({ mutateAsync: mockDeleteToken, isPending: false }),
}));

vi.mock("@multica/core/api", () => ({ api: { getBaseUrl: () => "https://api.example.com" } }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", name: "Acme", slug: "acme" }),
}));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: data.role, isLoading: false }),
}));
const toast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));
vi.mock("sonner", () => ({ toast }));

import { SecurityTab } from "./security-tab";

const connection = {
  provider: "oidc",
  issuer: "https://idp.example.com",
  client_id: "client-1",
  has_secret: true,
  allowed_domains: ["example.com"],
  auto_provision: true,
  enforced: false,
  updated_at: "2026-09-01T00:00:00Z",
};

describe("SecurityTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    data.role = "owner";
    data.sso = { configured: true, connection };
    data.tokens = { tokens: [] };
    mockPut.mockResolvedValue({});
    mockEnforce.mockResolvedValue({});
    mockCreateToken.mockResolvedValue({ id: "tok-1", token_hint: "scim_***abcd", token: "scim_raw_secret_value", active: true, created_at: "2026-09-01T00:00:00Z", last_used_at: null });
  });

  it("shows a callout and hides the form when the server has no secret key", () => {
    data.sso = { configured: false, connection: null };
    renderWithI18n(<SecurityTab />);
    expect(screen.getByText("SSO is not configured on this server")).toBeInTheDocument();
    expect(screen.queryByLabelText("Issuer URL")).toBeNull();
    // SCIM does not depend on the SSO secret key.
    expect(screen.getByDisplayValue("https://api.example.com/scim/v2")).toBeInTheDocument();
  });

  it("saves the connection without echoing the stored secret", async () => {
    const user = userEvent.setup();
    renderWithI18n(<SecurityTab />);

    const secret = screen.getByLabelText("Client secret");
    expect(secret).toHaveAttribute("placeholder", "unchanged");
    expect(secret).toHaveValue("");

    await user.clear(screen.getByLabelText("Allowed email domains"));
    await user.type(screen.getByLabelText("Allowed email domains"), "example.com\nexample.org");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(mockPut).toHaveBeenCalledTimes(1));
    const sent = mockPut.mock.calls[0]![0] as Record<string, unknown>;
    expect(sent).toEqual({
      issuer: "https://idp.example.com",
      client_id: "client-1",
      allowed_domains: ["example.com", "example.org"],
      auto_provision: true,
    });
    expect("client_secret" in sent).toBe(false);
  });

  it("sends a new secret when one is typed", async () => {
    const user = userEvent.setup();
    renderWithI18n(<SecurityTab />);
    await user.type(screen.getByLabelText("Client secret"), "new-secret");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(mockPut).toHaveBeenCalledWith(expect.objectContaining({ client_secret: "new-secret" })));
  });

  it("asks for confirmation before enforcing SSO", async () => {
    const user = userEvent.setup();
    renderWithI18n(<SecurityTab />);

    await user.click(screen.getByRole("switch", { name: "Enforce SSO" }));
    expect(mockEnforce).not.toHaveBeenCalled();
    expect(screen.getByText("Enforce SSO for this workspace?")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Enforce" }));
    await waitFor(() => expect(mockEnforce).toHaveBeenCalledWith(true));
  });

  it("shows a generated SCIM token once, in a copyable box", async () => {
    const user = userEvent.setup();
    renderWithI18n(<SecurityTab />);

    expect(screen.getByText("No SCIM token yet.")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Generate token" }));

    await waitFor(() => expect(mockCreateToken).toHaveBeenCalledTimes(1));
    const box = screen.getByTestId("scim-fresh-token");
    expect(box).toHaveTextContent("This token is shown once");
    expect(screen.getByDisplayValue("scim_raw_secret_value")).toBeInTheDocument();
  });

  it("confirms before replacing an active token, and lists hints only", async () => {
    const user = userEvent.setup();
    data.tokens = { tokens: [{ id: "tok-0", token_hint: "scim_***0000", active: true, created_at: "2026-09-01T00:00:00Z", last_used_at: null }] };
    renderWithI18n(<SecurityTab />);

    expect(screen.getByText("scim_***0000")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Generate token" }));
    expect(mockCreateToken).not.toHaveBeenCalled();
    expect(screen.getByText("Replace the active token?")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cancel" }));

    await user.click(await screen.findByRole("button", { name: "Revoke" }));
    await waitFor(() => expect(mockDeleteToken).toHaveBeenCalledWith("tok-0"));
  });

  it("is read-only for an admin", () => {
    data.role = "admin";
    renderWithI18n(<SecurityTab />);

    expect(screen.getByLabelText("Issuer URL")).toHaveAttribute("readonly");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Generate token" })).toBeNull();
    expect(screen.getByRole("switch", { name: "Enforce SSO" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByText(/Only the workspace owner/)).toBeInTheDocument();
  });
});
