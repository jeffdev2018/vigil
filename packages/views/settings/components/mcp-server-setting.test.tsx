// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { MCPServerSettingsEnvelope } from "@multica/core/agents/mcp-server";
import { renderWithI18n } from "../../test/i18n";

// Opens a Select's popup by its trigger accessible name and clicks the
// option whose accessible name matches.
async function pickOption(
  user: ReturnType<typeof userEvent.setup>,
  triggerName: string,
  optionName: string | RegExp,
) {
  await user.click(screen.getByRole("combobox", { name: triggerName }));
  await user.click(await screen.findByRole("option", { name: optionName }));
}

// Schema/client parsing: packages/core/agents/mcp-server.test.ts. This suite
// covers the wiring — read state, per-tool overrides, the dirty gate on
// Save, and the read-only surface for a non-admin.

const state = vi.hoisted(() => ({
  save: vi.fn(),
  envelope: {
    settings: { enabled: true, default_surface: "compound" as const, tools: {} },
    tools: [] as MCPServerSettingsEnvelope["tools"],
    endpoint: "/api/mcp/acme",
  },
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ settings: () => "/acme/settings" }),
}));
// Mocked at the context module rather than the barrel so <AppLink> stays the
// real component and the rendered href is what the test asserts.
vi.mock("../../navigation/context", () => ({
  useNavigation: () => ({
    push: vi.fn(),
    openInNewTab: vi.fn(),
    prefetch: vi.fn(),
    getShareableUrl: (p: string) => `https://app.example${p}`,
  }),
}));
vi.mock("@multica/core/api", () => ({
  api: { getBaseUrl: () => "https://api.example.test" },
}));
vi.mock("@multica/core/agents/mcp-server", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/mcp-server")>()),
  mcpServerSettingsOptions: () => ({
    queryKey: ["mcp-server-settings", JSON.stringify(state.envelope)],
    queryFn: async () => state.envelope,
  }),
  useUpdateMCPServerSettings: () => ({ mutate: state.save, isPending: false }),
}));

import { MCPServerSetting } from "./mcp-server-setting";

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <MCPServerSetting canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.save.mockReset();
  state.envelope = {
    settings: { enabled: true, default_surface: "compound", tools: {} },
    tools: [
      {
        name: "issue_create",
        group: "vigil_issue",
        action: "create",
        risk: "internal_write",
        description: "Create an issue.",
        agent_only: false,
      },
      {
        name: "run_start",
        group: "vigil_run",
        action: "start",
        risk: "external_effect",
        description: "Start a run on an issue.",
        agent_only: true,
      },
    ],
    endpoint: "/api/mcp/acme",
  };
});

describe("MCPServerSetting", () => {
  it("renders the endpoint built from the API base URL and the fetched settings", async () => {
    render();
    await screen.findByText("issue_create");
    const endpoint = screen.getByLabelText("Endpoint") as HTMLInputElement;
    await waitFor(() => expect(endpoint.value).toBe("https://api.example.test/api/mcp/acme"));
    expect(screen.getByText("Agent only")).toBeTruthy();
  });

  it("keeps Save disabled until a control is changed, then saves the edited overrides", async () => {
    render();
    await screen.findByText("issue_create");
    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeDisabled();

    const user = userEvent.setup();
    await pickOption(user, "Override for issue_create", "Ask");
    expect(saveButton).not.toBeDisabled();

    fireEvent.click(saveButton);
    expect(state.save).toHaveBeenCalledWith(
      { enabled: true, default_surface: "compound", tools: { issue_create: "ask" } },
      expect.anything(),
    );
  });

  it("regression: setting an override back to Default clears its key instead of storing an empty value", async () => {
    state.envelope = {
      ...state.envelope,
      settings: { enabled: true, default_surface: "compound", tools: { issue_create: "deny" } },
    };
    render();
    const select = await screen.findByRole("combobox", { name: "Override for issue_create" });
    expect(select.textContent).toContain("Deny");

    const user = userEvent.setup();
    await pickOption(user, "Override for issue_create", "Default");
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.save).toHaveBeenCalledWith(
      { enabled: true, default_surface: "compound", tools: {} },
      expect.anything(),
    );
  });

  it("toggling enabled and switching the default surface marks the form dirty and saves both", async () => {
    render();
    await screen.findByText("issue_create");
    fireEvent.click(screen.getByLabelText("Enabled"));
    fireEvent.click(screen.getByLabelText("Granular"));
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(state.save).toHaveBeenCalledWith(
      { enabled: false, default_surface: "granular", tools: {} },
      expect.anything(),
    );
  });

  it("is read-only for a member who cannot manage the workspace", async () => {
    render(false);
    await screen.findByText("issue_create");
    // Base UI marks a disabled switch with aria-disabled rather than the DOM
    // `disabled` attribute (see plugins-tab.test.tsx / security-tab.test.tsx).
    expect(screen.getByLabelText("Enabled")).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByLabelText("Override for issue_create")).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(screen.getByText("Only workspace owners and admins can change these settings.")).toBeTruthy();
  });
});
