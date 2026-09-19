// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// Node power actions (start / stop / reboot) and the per-node status refresh.
// The action-result parsing matrix lives in packages/core/api/schemas.test.ts.
const state = vi.hoisted(() => ({
  listCloudRuntimeNodes: vi.fn(),
  actOnCloudRuntimeNode: vi.fn(),
  getCloudRuntimeNodeStatus: vi.fn(),
  createCloudRuntimeNode: vi.fn(),
  deleteCloudRuntimeNode: vi.fn(),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));
vi.mock("@multica/core/api", () => ({ api: state }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

import { CloudRuntimeDialog } from "./cloud-runtime-dialog";

function node(over: Record<string, unknown> = {}) {
  return {
    id: "n-1",
    owner_id: "u-1",
    instance_id: "i-0abc",
    region: "eu-west-3",
    instance_type: "t4g.medium",
    image_id: "ami-1",
    subnet_id: "subnet-1",
    name: "worker-1",
    status: "running",
    tags: {},
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

function renderDialog() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <CloudRuntimeDialog onClose={() => {}} />
    </QueryClientProvider>,
  );
}

describe("CloudRuntimeDialog node power actions", () => {
  it("offers stop and reboot on a running node and sends the action", async () => {
    state.listCloudRuntimeNodes.mockResolvedValue([node()]);
    state.actOnCloudRuntimeNode.mockResolvedValue({
      instance_id: "i-0abc",
      status: "stopping",
    });
    renderDialog();

    const stop = await screen.findByRole("button", { name: "Stop" });
    expect(screen.queryByRole("button", { name: "Start" })).toBeNull();
    fireEvent.click(stop);
    await waitFor(() =>
      expect(state.actOnCloudRuntimeNode).toHaveBeenCalledWith(
        "stop",
        "i-0abc",
      ),
    );
  });

  // P3 audit finding: reboot and delete used the native window.confirm(),
  // which cannot be themed and (unlike every other destructive action in
  // this app) is not driven by the same controlled AlertDialog component.
  it("confirms through the AlertDialog before rebooting, and does nothing on cancel", async () => {
    state.listCloudRuntimeNodes.mockResolvedValue([node()]);
    state.actOnCloudRuntimeNode.mockResolvedValue({
      instance_id: "i-0abc",
      status: "rebooting",
    });
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Reboot" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("Anything running on it is interrupted");

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("alertdialog")).toBeNull();
    // This file's mocks are not reset between tests (pre-existing, out of
    // scope here) — assert on the reboot-specific call rather than "never
    // called at all".
    expect(state.actOnCloudRuntimeNode).not.toHaveBeenCalledWith(
      "reboot",
      "i-0abc",
    );

    fireEvent.click(screen.getByRole("button", { name: "Reboot" }));
    fireEvent.click(
      within(await screen.findByRole("alertdialog")).getByRole("button", {
        name: "Reboot",
      }),
    );
    await waitFor(() =>
      expect(state.actOnCloudRuntimeNode).toHaveBeenCalledWith(
        "reboot",
        "i-0abc",
      ),
    );
  });

  it("confirms through the AlertDialog before deleting the node", async () => {
    state.listCloudRuntimeNodes.mockResolvedValue([node()]);
    state.deleteCloudRuntimeNode.mockResolvedValue(undefined);
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Delete node" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("cannot be undone");

    fireEvent.click(
      within(dialog).getByRole("button", { name: "Delete node" }),
    );
    await waitFor(() =>
      expect(state.deleteCloudRuntimeNode).toHaveBeenCalledWith("i-0abc"),
    );
  });

  it("offers start on a stopped node", async () => {
    state.listCloudRuntimeNodes.mockResolvedValue([node({ status: "stopped" })]);
    renderDialog();

    expect(await screen.findByRole("button", { name: "Start" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Reboot" })).toBeNull();
  });

  it("offers no power action while the node is transitioning", async () => {
    state.listCloudRuntimeNodes.mockResolvedValue([
      node({ status: "stopping" }),
    ]);
    renderDialog();

    await screen.findByText("stopping");
    expect(screen.queryByRole("button", { name: "Start" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Stop" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Reboot" })).toBeNull();
  });

  it("refreshes one node's status into the list", async () => {
    state.listCloudRuntimeNodes.mockResolvedValue([node()]);
    state.getCloudRuntimeNodeStatus.mockResolvedValue({
      instance_id: "i-0abc",
      status: "stopped",
    });
    renderDialog();

    fireEvent.click(await screen.findByRole("button", { name: "Refresh status" }));
    await waitFor(() =>
      expect(state.getCloudRuntimeNodeStatus).toHaveBeenCalledWith("i-0abc"),
    );
    await waitFor(() => expect(screen.getByText("stopped")).toBeTruthy());
  });
});
