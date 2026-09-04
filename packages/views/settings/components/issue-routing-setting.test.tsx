// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RoutingSettings } from "@multica/core/issues/routing";
import { renderWithI18n } from "../../test/i18n";

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
    const high = (await screen.findByLabelText("Pool for High risk")) as HTMLSelectElement;
    await waitFor(() => expect(high.value).toBe("p3"));
    fireEvent.change(screen.getByLabelText("Pool for Low risk"), { target: { value: "p1" } });
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
    expect(((await screen.findByLabelText("Pool for High risk")) as HTMLSelectElement).disabled).toBe(true);
  });
});
