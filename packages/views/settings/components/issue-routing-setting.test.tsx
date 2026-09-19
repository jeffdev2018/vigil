// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RoutingSettings } from "@multica/core/issues/routing";
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

// Client parsing: packages/core/issues/routing.test.ts.

const state = vi.hoisted(() => ({
  settings: { enabled: false, pools: {}, escalation_failures: 2 } as RoutingSettings,
  save: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/runtimes/pools", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/runtimes/pools")>()),
  runtimePoolsOptions: () => ({ queryKey: ["pools"], queryFn: async () => [{ id: "p1", name: "cheap", runtime_ids: [], degraded_runtime_id: null, agent_count: 0, created_at: "" }, { id: "p3", name: "capable", runtime_ids: [], degraded_runtime_id: null, agent_count: 0, created_at: "" }] }),
}));
vi.mock("@multica/core/issues/routing", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/routing")>()),
  routingSettingsOptions: () => ({ queryKey: ["routing"], queryFn: async () => state.settings }),
  useSaveRoutingSettings: () => ({ mutate: state.save, isPending: false }),
}));

import { IssueRoutingSetting } from "./issue-routing-setting";

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <IssueRoutingSetting canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.settings = { enabled: false, pools: { high: "p3" }, escalation_failures: 2 };
  state.save.mockReset();
});

describe("IssueRoutingSetting", () => {
  it("shows the policy and saves a pool per risk level, the switch and the threshold", async () => {
    render();
    const high = await screen.findByRole("combobox", { name: "Pool for High risk" });
    await waitFor(() => expect(high.textContent).toContain("capable"));
    const user = userEvent.setup();
    await pickOption(user, "Pool for Low risk", "cheap");
    expect(state.save).toHaveBeenLastCalledWith({ enabled: false, pools: { high: "p3", low: "p1" }, escalation_failures: 2 }, expect.anything());
    fireEvent.click(screen.getByLabelText("Route issues by risk"));
    expect(state.save).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: true }), expect.anything());
    const threshold = screen.getByLabelText("Escalate after consecutive failures") as HTMLInputElement;
    fireEvent.change(threshold, { target: { value: "3" } });
    fireEvent.blur(threshold);
    expect(state.save).toHaveBeenLastCalledWith(expect.objectContaining({ escalation_failures: 3 }), expect.anything());
  });

  it("is inert for viewers", async () => {
    render(false);
    expect(await screen.findByRole("combobox", { name: "Pool for High risk" })).toBeDisabled();
  });
});
