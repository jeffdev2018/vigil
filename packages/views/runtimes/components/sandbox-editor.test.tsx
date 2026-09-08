// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

// `mockUpdateRuntime` receives (runtimeId, patch) and may return a rejected
// promise to simulate a 400 with a plain message.
const mockUpdateRuntime = vi.hoisted(() => vi.fn());
const mockToast = vi.hoisted(() => ({ success: vi.fn(), error: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: mockToast }));
vi.mock("@multica/core/runtimes/mutations", () => ({
  useUpdateRuntime: () => ({
    mutate: (
      args: { runtimeId: string; patch: Record<string, unknown> },
      opts?: { onSuccess?: () => void; onError?: (err: unknown) => void },
    ) => {
      Promise.resolve(mockUpdateRuntime(args.runtimeId, args.patch)).then(
        () => opts?.onSuccess?.(),
        (err: unknown) => opts?.onError?.(err),
      );
    },
    isPending: false,
  }),
}));

import { SandboxEditor } from "./sandbox-editor";

function makeRuntime(overrides: Partial<AgentRuntime>): AgentRuntime {
  return {
    id: "rt-1",
    workspace_id: "ws-1",
    daemon_id: null,
    name: "Local Runtime",
    runtime_mode: "local",
    provider: "claude",
    launch_header: "",
    status: "online",
    device_info: "host.local",
    metadata: {},
    owner_id: "user-me",
    visibility: "private",
    last_seen_at: null,
    created_at: "2026-04-01T00:00:00Z",
    updated_at: "2026-04-01T00:00:00Z",
    ...overrides,
  };
}

function renderEditor(runtime: AgentRuntime, canEdit = true) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <SandboxEditor runtime={runtime} canEdit={canEdit} />
    </I18nProvider>,
  );
}

describe("SandboxEditor", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockUpdateRuntime.mockResolvedValue(undefined);
  });

  it("disables the modes the machine cannot run once capabilities are known", () => {
    renderEditor(
      makeRuntime({
        sandbox_capabilities: { os: "darwin", docker: true, modes: ["none", "container"] },
      }),
    );
    expect(screen.getByRole("radio", { name: "None" })).toBeEnabled();
    expect(screen.getByRole("radio", { name: "Sandbox" })).toBeDisabled();
    expect(screen.getByRole("radio", { name: "Container" })).toBeEnabled();
    expect(screen.queryByText(/has not reported/)).not.toBeInTheDocument();
  });

  it("keeps every mode enabled and says so while the daemon has not reported", () => {
    renderEditor(makeRuntime({}));
    expect(screen.getByRole("radio", { name: "Sandbox" })).toBeEnabled();
    expect(screen.getByRole("radio", { name: "Container" })).toBeEnabled();
    expect(screen.getByText(/has not reported/)).toBeInTheDocument();
  });

  it("saves none/sandbox on click without a form", async () => {
    renderEditor(makeRuntime({ sandbox_mode: "none" }));
    fireEvent.click(screen.getByRole("radio", { name: "Sandbox" }));
    await waitFor(() =>
      expect(mockUpdateRuntime).toHaveBeenCalledWith("rt-1", { sandbox_mode: "sandbox" }),
    );
    expect(mockToast.success).toHaveBeenCalled();
  });

  it("reveals image and hosts when container is picked and saves them together", async () => {
    renderEditor(makeRuntime({ sandbox_mode: "none" }));
    expect(screen.queryByLabelText("Image")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("radio", { name: "Container" }));
    expect(mockUpdateRuntime).not.toHaveBeenCalled();

    fireEvent.change(screen.getByLabelText("Image"), {
      target: { value: " ghcr.io/acme/agent:1 " },
    });
    fireEvent.change(screen.getByLabelText("Extra allowed hosts"), {
      target: { value: "registry.acme.com\n\n  api.acme.com  \n" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(mockUpdateRuntime).toHaveBeenCalledWith("rt-1", {
        sandbox_mode: "container",
        sandbox_image: "ghcr.io/acme/agent:1",
        sandbox_allowed_hosts: ["registry.acme.com", "api.acme.com"],
      }),
    );
  });

  it("renders a server validation error inline", async () => {
    mockUpdateRuntime.mockRejectedValue(new Error('"x y" is not a host name'));
    renderEditor(makeRuntime({ sandbox_mode: "container" }));
    fireEvent.change(screen.getByLabelText("Extra allowed hosts"), {
      target: { value: "x y" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByRole("alert")).toHaveTextContent('"x y" is not a host name');
  });

  it("warns plainly when the last run was degraded below the requested mode", () => {
    renderEditor(makeRuntime({ sandbox_mode: "container", sandbox_effective: "sandbox" }));
    expect(screen.getByText("Last run: Sandbox")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(/degraded to Sandbox/);
  });

  it("shows no warning when the last run matched the requested mode", () => {
    renderEditor(makeRuntime({ sandbox_mode: "sandbox", sandbox_effective: "sandbox" }));
    expect(screen.getByText("Last run: Sandbox")).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("is read-only without canEdit, still showing the effective mode", () => {
    renderEditor(
      makeRuntime({
        sandbox_mode: "container",
        sandbox_image: "ghcr.io/acme/agent:1",
        sandbox_allowed_hosts: ["api.acme.com"],
        sandbox_effective: "none",
      }),
      false,
    );
    expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    expect(screen.getByText("ghcr.io/acme/agent:1")).toBeInTheDocument();
    expect(screen.getByText("api.acme.com")).toBeInTheDocument();
    expect(screen.getByText("Last run: None")).toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent(/degraded to None/);
  });
});
