// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/workflow-policy", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/workflow-policy")>()),
  workflowPolicySettingsOptions: () => ({ queryKey: ["workflow-policy-settings"], queryFn: async () => ({ mode: "auto" }) }),
  useSaveWorkflowPolicySettings: () => ({ mutate: state.save, isPending: false }),
}));

import { WorkflowPolicySetting } from "./workflow-policy-setting";

describe("WorkflowPolicySetting", () => {
  it("reflects the saved mode and persists the toggle as a mode payload", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <WorkflowPolicySetting canEdit />
      </QueryClientProvider>,
    );
    const toggle = await screen.findByRole("switch", { name: "Learn the workflow from run history" });
    // The loaded "auto" settings hydrate the draft.
    await waitFor(() => expect(toggle).toBeChecked());
    fireEvent.click(toggle);
    expect(state.save).toHaveBeenCalledWith({ mode: "off" }, expect.anything());
  });

  it("persists auto when the toggle is switched on", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <WorkflowPolicySetting canEdit />
      </QueryClientProvider>,
    );
    const toggle = await screen.findByRole("switch", { name: "Learn the workflow from run history" });
    await waitFor(() => expect(toggle).toBeChecked());
    // Off then on again ships the auto payload.
    fireEvent.click(toggle);
    fireEvent.click(toggle);
    expect(state.save).toHaveBeenLastCalledWith({ mode: "auto" }, expect.anything());
  });
});
