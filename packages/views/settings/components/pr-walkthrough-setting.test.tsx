// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PrWalkthroughSettings } from "@multica/core/pr-walkthrough";
import { renderWithI18n } from "../../test/i18n";

// Client parsing: packages/core/pr-walkthrough/schemas.test.ts.

const state = vi.hoisted(() => ({
  settings: { enabled: false, agent_id: "" } as PrWalkthroughSettings,
  agents: [{ id: "agent-1", name: "Reviewer" }],
  save: vi.fn(),
  fail: false,
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => state.agents }),
}));
vi.mock("@multica/core/pr-walkthrough", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/pr-walkthrough")>()),
  prWalkthroughSettingsOptions: () => ({
    queryKey: ["wt-settings"],
    queryFn: async () => {
      if (state.fail) throw new Error("network down");
      return state.settings;
    },
  }),
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
  state.fail = false;
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
    const user = userEvent.setup();
    await user.click(screen.getByRole("combobox", { name: "Walkthrough agent" }));
    // Wait for the agent list before selecting: an option that is not rendered
    // yet cannot be chosen, and the select would silently stay empty.
    await user.click(await screen.findByRole("option", { name: "Reviewer" }));
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

  // P3 audit finding: a failed fetch fell back to the safe "off" default
  // with no indication anything had gone wrong, and left the controls
  // editable — a save while the real remote state is unknown could clobber
  // whatever it actually was.
  it("blocks editing and shows a retry-able error when the fetch fails", async () => {
    state.fail = true;
    render();

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Couldn't load pull request walkthrough settings",
    );
    expect(screen.getByLabelText("Generate walkthroughs")).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByLabelText("Walkthrough agent")).toBeDisabled();
  });
});
