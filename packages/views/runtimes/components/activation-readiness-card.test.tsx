// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import runtimes from "../../locales/en/runtimes.json";
import common from "../../locales/en/common.json";
import { ActivationReadinessCard } from "./activation-readiness-card";

const api = vi.hoisted(() => ({
  listRuntimes: vi.fn(),
  listAgents: vi.fn(),
  listChatSessions: vi.fn(),
  listGitHubInstallations: vi.fn(),
}));

const captureEvent = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async (original) => ({
  ...(await original<object>()),
  api,
}));

vi.mock("@multica/core/analytics", () => ({
  captureEvent,
}));

const authState = { user: { onboarded_at: "2026-09-07T00:00:00Z" } };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (s: typeof authState) => unknown) => selector(authState),
    { getState: () => authState },
  ),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", repos: [] }),
  useWorkspacePaths: () => ({
    runtimes: () => "/ws/runtimes",
    settings: () => "/ws/settings",
  }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({
    href,
    children,
    className,
  }: {
    href: string;
    children: React.ReactNode;
    className?: string;
  }) => (
    <a href={href} className={className}>
      {children}
    </a>
  ),
}));

function mount() {
  return render(
    <I18nProvider locale="en" resources={{ en: { runtimes, common } }}>
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <ActivationReadinessCard machines={[]} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("ActivationReadinessCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    api.listRuntimes.mockResolvedValue([]);
    api.listAgents.mockResolvedValue([]);
    api.listChatSessions.mockResolvedValue([]);
    api.listGitHubInstallations.mockResolvedValue({ installations: [] });
  });

  it("shows blocked preparation steps until a first result path is ready", async () => {
    mount();
    expect(
      await screen.findByRole("region", { name: "Ready for a first result?" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Machine online")).toBeInTheDocument();
    expect(
      screen.getByText("Start or reconnect a desktop daemon, or add a computer."),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "No online Claude or Codex runtime yet — sign-in applies when one is connected.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText("Optional")).toBeInTheDocument();
    // The repository step is the only one fixed off this page, so it is the
    // only row that links anywhere. The others are resolved by the machine
    // list and the Mika card directly below the checklist.
    expect(screen.getByRole("link", { name: /Repository linked/i })).toHaveAttribute(
      "href",
      "/ws/settings?tab=repositories",
    );
    expect(screen.getAllByRole("link")).toHaveLength(1);
    await waitFor(() => {
      expect(captureEvent).toHaveBeenCalledWith(
        "activation_checklist_viewed",
        expect.objectContaining({
          workspace_id: "ws-1",
          blocked_required_count: expect.any(Number),
          blocked_steps: expect.arrayContaining(["machine_online"]),
          minutes_since_onboarding: expect.any(Number),
        }),
      );
    });
  });

  it("hides when every required step is ready", async () => {
    api.listRuntimes.mockResolvedValue([
      {
        id: "rt-1",
        status: "online",
        provider: "claude",
        metadata: { cli_auth: { authenticated: true } },
      },
    ]);
    api.listAgents.mockResolvedValue([{ id: "mika", system_key: "mika" }]);
    api.listChatSessions.mockResolvedValue([
      { agent_id: "mika", last_message: { id: "msg-1" } },
    ]);
    api.listGitHubInstallations.mockResolvedValue({
      installations: [{ id: "inst-1" }],
    });
    const { container } = mount();
    await waitFor(() => {
      expect(container.querySelector("section")).toBeNull();
    });
  });
});
