// @vitest-environment jsdom

import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import type { WorkspaceMcpServer } from "@multica/core/types";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const mockSetPolicy = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/workspace/mutations", () => ({
  useSetAgentMcpServerPolicy: () => ({ mutateAsync: mockSetPolicy, isPending: false }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

import { effectiveToolClass, McpToolPolicy } from "./mcp-tool-policy";

const server: WorkspaceMcpServer = {
  id: "srv-1",
  workspace_id: "ws-1",
  name: "crm",
  transport: "http",
  created_at: "2026-08-14T00:00:00Z",
  updated_at: "2026-08-14T00:00:00Z",
  tool_count: 2,
  tool_policy: { default: "by_risk", tools: { send_email: "ask" } },
  tools: [
    { name: "search", description: "Find contacts", risk: "read", risk_source: "auto", class: "act_alone" },
    {
      name: "send_email",
      risk: "external_effect",
      risk_source: "manual",
      class: "ask",
      last_used_at: new Date(Date.now() - 3 * 3600_000).toISOString(),
    },
  ],
};

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={{ en: { common: enCommon, agents: enAgents } }}>
      {children}
    </I18nProvider>
  );
}

async function open(canEdit = true, over: Partial<WorkspaceMcpServer> = {}) {
  const user = userEvent.setup();
  render(
    <McpToolPolicy agentId="agent-1" server={{ ...server, ...over }} canEdit={canEdit} />,
    { wrapper: Wrapper },
  );
  await user.click(screen.getByRole("button", { name: "Policy" }));
  return user;
}

describe("effectiveToolClass", () => {
  const read = { name: "a", risk: "read", risk_source: "auto" } as const;
  const external = { name: "b", risk: "external_effect", risk_source: "auto" } as const;

  it("mirrors the gateway rule", () => {
    expect(effectiveToolClass({}, read)).toBe("act_alone");
    expect(effectiveToolClass({}, external)).toBe("ask");
    expect(effectiveToolClass({ default: "ask" }, read)).toBe("ask");
    expect(effectiveToolClass({ default: "never" }, read)).toBe("never");
    expect(effectiveToolClass({ default: "never", tools: { a: "act_alone" } }, read)).toBe(
      "act_alone",
    );
  });
});

describe("McpToolPolicy", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockSetPolicy.mockResolvedValue([]);
  });

  it("shows the catalogue with risk, class in force and last use", async () => {
    await open();

    expect(screen.getByLabelText("Default")).toHaveValue("by_risk");
    expect(screen.getByText("search")).toBeInTheDocument();
    expect(screen.getByText("Read")).toBeInTheDocument();
    expect(screen.getByText("External effect")).toBeInTheDocument();
    expect(screen.getByLabelText("Class of search")).toHaveValue("");
    expect(screen.getByLabelText("Class of send_email")).toHaveValue("ask");
    expect(screen.getByText("3h ago")).toBeInTheDocument();
  });

  it("saves the default and per-tool classes, dropping inherited entries", async () => {
    const user = await open();

    await user.selectOptions(screen.getByLabelText("Default"), "never");
    await user.selectOptions(screen.getByLabelText("Class of search"), "act_alone");
    await user.selectOptions(screen.getByLabelText("Class of send_email"), "");
    await user.click(screen.getByRole("button", { name: /Save policy/ }));

    await waitFor(() =>
      expect(mockSetPolicy).toHaveBeenCalledWith({
        serverId: "srv-1",
        policy: { default: "never", tools: { search: "act_alone" } },
      }),
    );
  });

  // The gateway refuses a class above the trust dial or a Rule of Two
  // violation with a 400 that names the tool; the message belongs under the
  // editor where the select is.
  it("shows the server's 400 message inline", async () => {
    const message =
      'tool send_email is external_effect: the agent\'s trust dial (approval) allows at most "ask"';
    mockSetPolicy.mockRejectedValue(new ApiError(message, 400, "Bad Request", { error: message }));
    const user = await open();

    await user.selectOptions(screen.getByLabelText("Class of send_email"), "act_alone");
    await user.click(screen.getByRole("button", { name: /Save policy/ }));

    expect(await screen.findByRole("alert")).toHaveTextContent(message);
  });

  it("is read-only without edit rights", async () => {
    await open(false);

    expect(screen.getByLabelText("Default")).toBeDisabled();
    expect(screen.getByLabelText("Class of search")).toBeDisabled();
    expect(screen.queryByRole("button", { name: /Save policy/ })).toBeNull();
  });

  it("explains an empty catalogue", async () => {
    await open(true, { tools: [] });

    expect(screen.getByText(/No tools catalogued yet/)).toBeInTheDocument();
  });
});
