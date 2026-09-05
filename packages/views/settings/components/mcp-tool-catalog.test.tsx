// @vitest-environment jsdom

import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import enCommon from "../../locales/en/common.json";
import enSettings from "../../locales/en/settings.json";

const mockApi = vi.hoisted(() => ({
  listWorkspaceMcpServerTools: vi.fn(),
  discoverWorkspaceMcpServerTools: vi.fn(),
  setWorkspaceMcpServerTools: vi.fn(),
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return { ...actual, api: mockApi };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { McpToolCatalog } from "./mcp-tool-catalog";

const catalog = {
  discovered_at: "2026-09-01T00:00:00Z",
  risks: ["read", "internal_write", "external_effect", "sensitive_data", "unknown"],
  tools: [
    { name: "search", description: "Find issues", risk: "read", risk_source: "auto" },
    { name: "send_email", risk: "external_effect", risk_source: "manual" },
  ],
};

function Wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <I18nProvider locale="en" resources={{ en: { common: enCommon, settings: enSettings } }}>
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

function renderCatalog(canManage = true) {
  return render(
    <McpToolCatalog wsId="ws-1" serverId="srv-1" canManage={canManage} />,
    { wrapper: Wrapper },
  );
}

describe("McpToolCatalog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockApi.listWorkspaceMcpServerTools.mockResolvedValue(catalog);
    mockApi.discoverWorkspaceMcpServerTools.mockResolvedValue(catalog);
    mockApi.setWorkspaceMcpServerTools.mockResolvedValue(catalog);
  });

  it("lists the catalogued tools with their risk and its source", async () => {
    renderCatalog();

    expect(await screen.findByText("search")).toBeInTheDocument();
    expect(screen.getByText("Find issues")).toBeInTheDocument();
    expect(screen.getByLabelText("Risk of search")).toHaveValue("read");
    expect(screen.getByLabelText("Risk of send_email")).toHaveValue("external_effect");
    expect(screen.getByText("auto")).toBeInTheDocument();
    expect(screen.getByText("manual")).toBeInTheDocument();
    expect(mockApi.listWorkspaceMcpServerTools).toHaveBeenCalledWith("ws-1", "srv-1");
  });

  it("saves the whole list after reclassifying, adding and removing tools", async () => {
    const user = userEvent.setup();
    renderCatalog();
    await screen.findByText("search");

    await user.selectOptions(screen.getByLabelText("Risk of search"), "sensitive_data");
    await user.click(screen.getByRole("button", { name: "Remove send_email" }));
    await user.type(screen.getByLabelText("Tool name"), "create_issue");
    await user.type(screen.getByLabelText("Description (optional)"), "Files an issue");
    await user.selectOptions(screen.getByLabelText("Risk of create_issue"), "internal_write");
    await user.click(screen.getByRole("button", { name: /Add tool/ }));
    await user.click(screen.getByRole("button", { name: /Save tools/ }));

    await waitFor(() =>
      expect(mockApi.setWorkspaceMcpServerTools).toHaveBeenCalledWith("ws-1", "srv-1", [
        { name: "search", description: "Find issues", risk: "sensitive_data" },
        { name: "create_issue", description: "Files an issue", risk: "internal_write" },
      ]),
    );
  });

  it("discovers tools from the server", async () => {
    const user = userEvent.setup();
    renderCatalog();
    await screen.findByText("search");

    await user.click(screen.getByRole("button", { name: /Discover tools/ }));

    await waitFor(() =>
      expect(mockApi.discoverWorkspaceMcpServerTools).toHaveBeenCalledWith("ws-1", "srv-1"),
    );
  });

  // A stdio server cannot be asked for its tools from the API; the daemon
  // catalogues it at the first run. The 400 carries that explanation, and it
  // belongs next to the button, not only in a toast.
  it("shows the server's 400 message inline when discovery is refused", async () => {
    const user = userEvent.setup();
    const message =
      "a stdio server is catalogued by the daemon at its first run; add its tools by hand meanwhile";
    mockApi.discoverWorkspaceMcpServerTools.mockRejectedValue(
      new ApiError(message, 400, "Bad Request", { error: message }),
    );
    renderCatalog();
    await screen.findByText("search");

    await user.click(screen.getByRole("button", { name: /Discover tools/ }));

    expect(await screen.findByRole("alert")).toHaveTextContent(message);
  });

  it("renders read-only for a plain member", async () => {
    renderCatalog(false);

    expect(await screen.findByLabelText("Risk of search")).toBeDisabled();
    expect(screen.queryByRole("button", { name: /Discover tools/ })).toBeNull();
    expect(screen.queryByRole("button", { name: /Save tools/ })).toBeNull();
    expect(screen.queryByRole("button", { name: "Remove search" })).toBeNull();
  });

  it("explains an empty catalogue", async () => {
    mockApi.listWorkspaceMcpServerTools.mockResolvedValue({ ...catalog, tools: [] });
    renderCatalog();

    expect(await screen.findByText(/No tools catalogued yet/)).toBeInTheDocument();
  });
});
