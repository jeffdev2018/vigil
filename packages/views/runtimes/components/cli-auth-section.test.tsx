// @vitest-environment jsdom

import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentRuntime, RuntimeCliAuthRequest } from "@multica/core/types";

const { mutate } = vi.hoisted(() => ({ mutate: vi.fn() }));

vi.mock("@multica/core/runtimes/cli-auth", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/runtimes/cli-auth")>();
  return {
    ...actual,
    useRuntimeCliAuth: () => ({ mutate, isPending: false }),
  };
});

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (select: (bundle: Record<string, unknown>) => string) =>
      select({
        cli_auth: {
          authenticated: "Authenticated",
          not_authenticated: "Not authenticated",
          unknown: "Unknown",
          connect: "Connect",
          disconnect: "Disconnect",
          title: "Connect",
          disconnecting_title: "Disconnect",
          machine_scope: "This changes authentication for the whole machine.",
          waiting: "Waiting",
          expires_in: "Expires soon",
          open_url: "Open URL",
          copy_url: "Copy URL",
          code: "Code",
          copy_code: "Copy code",
          completed: "Connected",
          disconnected: "Disconnected",
          failed: "Failed",
          offline: "Offline",
          help: "Help",
        },
      }),
    i18n: { language: "en" },
  }),
}));

import { CliAuthSection } from "./cli-auth-section";

function runtime(overrides: Partial<AgentRuntime> = {}): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: "daemon-1",
    name: "Codex",
    runtime_mode: "local",
    provider: "codex",
    launch_header: "",
    status: "online",
    device_info: "dev.local",
    metadata: {},
    owner_id: "user-1",
    visibility: "private",
    last_seen_at: "2026-09-03T00:00:00Z",
    created_at: "2026-09-03T00:00:00Z",
    updated_at: "2026-09-03T00:00:00Z",
    ...overrides,
  };
}

function progress(overrides: Partial<RuntimeCliAuthRequest> = {}): RuntimeCliAuthRequest {
  return {
    id: "req-1",
    runtime_id: "rt-1",
    action: "login",
    status: "running",
    verification_url: "https://auth.openai.com/device",
    user_code: "ABCD-EFGH",
    created_at: "2026-09-03T00:00:00Z",
    updated_at: "2026-09-03T00:00:00Z",
    expires_at: "2099-09-03T00:10:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  mutate.mockReset();
});

describe("CliAuthSection", () => {
  it("keeps an unknown state neutral and displays device-code progress", () => {
    mutate.mockImplementation((input: { onProgress: (value: RuntimeCliAuthRequest) => void }) => {
      input.onProgress(progress());
    });
    render(<CliAuthSection runtime={runtime()} />);

    expect(screen.getByText(/codex: unknown/i)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));

    expect(screen.getByText("ABCD-EFGH")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "https://auth.openai.com/device" })).toHaveAttribute(
      "href",
      "https://auth.openai.com/device",
    );
  });

  it("disables authentication while the runtime is offline", () => {
    render(<CliAuthSection runtime={runtime({ status: "offline" })} />);
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();
  });
});
