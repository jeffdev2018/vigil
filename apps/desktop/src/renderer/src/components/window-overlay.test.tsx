import { fireEvent, render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

const workspaces = vi.hoisted(() => ({ current: [] as unknown[] }));

vi.mock("@multica/views/onboarding", () => ({
  OnboardingFlow: () => <div>onboarding-flow</div>,
}));
vi.mock("@multica/views/invite", () => ({ InvitePage: () => <div>invite-page</div> }));
vi.mock("@multica/views/invitations", () => ({ InvitationsPage: () => <div>invitations-page</div> }));
vi.mock("@multica/views/navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  workspaceListOptions: () => ({
    queryKey: ["workspaces"],
    queryFn: () => Promise.resolve(workspaces.current),
  }),
}));
vi.mock("../platform/use-local-runtimes-pending", () => ({ useLocalRuntimesPending: () => false }));

import { useWindowOverlayStore } from "@/stores/window-overlay-store";
import { WindowOverlay } from "./window-overlay";

function renderOverlay() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <WindowOverlay />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  useWindowOverlayStore.setState({ overlay: null });
  workspaces.current = [];
});

describe("WindowOverlay", () => {
  // Regression: the overlay is a plain full-window div, not a dialog, so
  // Escape did nothing on "Create workspace" while Back was right there.
  it("closes on Escape when there is a workspace to go back to", async () => {
    workspaces.current = [{ id: "ws-1", slug: "acme" }];
    useWindowOverlayStore.getState().open({ type: "new-workspace" });
    renderOverlay();
    await screen.findByText("onboarding-flow");
    // Let the workspace list settle: Back only exists once it has.
    await vi.waitFor(() => {
      fireEvent.keyDown(window, { key: "Escape" });
      expect(useWindowOverlayStore.getState().overlay).toBeNull();
    });
  });

  it("keeps a zero-workspace user in the flow on Escape", async () => {
    useWindowOverlayStore.getState().open({ type: "new-workspace" });
    renderOverlay();
    await screen.findByText("onboarding-flow");
    await new Promise((resolve) => setTimeout(resolve, 0));
    fireEvent.keyDown(window, { key: "Escape" });
    expect(useWindowOverlayStore.getState().overlay).toEqual({ type: "new-workspace" });
  });
});
