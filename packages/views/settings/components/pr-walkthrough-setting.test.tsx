// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PrWalkthroughSettings } from "@multica/core/pr-walkthrough";
import { renderWithI18n } from "../../test/i18n";

// Client parsing: packages/core/pr-walkthrough/schemas.test.ts.

const state = vi.hoisted(() => ({
  settings: { enabled: false, agent_id: "" } as PrWalkthroughSettings,
  agents: [{ id: "agent-1", name: "Reviewer" }],
  save: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => state.agents }),
}));
vi.mock("@multica/core/pr-walkthrough", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/pr-walkthrough")>()),
  prWalkthroughSettingsOptions: () => ({ queryKey: ["wt-settings"], queryFn: async () => state.settings }),
  useSavePrWalkthroughSettings: () => ({ mutate: state.save, isPending: false }),
}));

import { PrWalkthroughSetting } from "./pr-walkthrough-setting";

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <PrWalkthroughSetting canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.settings = { enabled: false, agent_id: "" };
  state.save.mockReset();
});

describe("PrWalkthroughSetting", () => {
  it("cannot be switched on before an agent is picked", async () => {
    render();
    // The server refuses to enable without an agent; the switch says so up
    // front instead of letting the save fail.
    const toggle = await screen.findByLabelText("Generate walkthroughs");
    await waitFor(() => expect(toggle).toHaveAttribute("aria-disabled", "true"));
    expect(state.save).not.toHaveBeenCalled();
  });

  it("saves the agent, then the switch", async () => {
    render();
    // Wait for the agent list before selecting: an option that is not rendered
    // yet cannot be chosen, and the select would silently stay empty.
    await screen.findByRole("option", { name: "Reviewer" });
    fireEvent.change(screen.getByLabelText("Walkthrough agent"), { target: { value: "agent-1" } });
    expect(state.save).toHaveBeenLastCalledWith({ enabled: false, agent_id: "agent-1" }, expect.anything());

    const toggle = screen.getByLabelText("Generate walkthroughs");
    await waitFor(() => expect(toggle).not.toHaveAttribute("aria-disabled", "true"));
    fireEvent.click(toggle);
    expect(state.save).toHaveBeenLastCalledWith({ enabled: true, agent_id: "agent-1" }, expect.anything());
  });

  it("is inert for viewers", async () => {
    state.settings = { enabled: true, agent_id: "agent-1" };
    render(false);
    expect(await screen.findByLabelText("Walkthrough agent")).toBeDisabled();
    expect(screen.getByLabelText("Generate walkthroughs")).toHaveAttribute("aria-disabled", "true");
  });
});
