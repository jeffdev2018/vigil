// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { DriftPolicy } from "@multica/core/issues/drift";
import { renderWithI18n } from "../../test/i18n";

// Client parsing: packages/core/issues/drift.test.ts.

const state = vi.hoisted(() => ({ policy: { enabled: true, repeated_action_threshold: 5, file_reread_threshold: 8 } as DriftPolicy, save: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/drift", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/drift")>()),
  driftPolicyOptions: () => ({ queryKey: ["drift"], queryFn: async () => state.policy }),
  useSaveDriftPolicy: () => ({ mutate: state.save, isPending: false }),
}));

import { DriftDetectionSetting } from "./drift-detection-setting";

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DriftDetectionSetting canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.policy = { enabled: true, repeated_action_threshold: 5, file_reread_threshold: 8 };
  state.save.mockReset();
});

describe("DriftDetectionSetting", () => {
  it("shows the thresholds and saves changes", async () => {
    render();
    const repeated = (await screen.findByLabelText("Repeated action threshold")) as HTMLInputElement;
    await waitFor(() => expect(repeated.value).toBe("5"));
    fireEvent.change(repeated, { target: { value: "7" } });
    fireEvent.blur(repeated);
    expect(state.save).toHaveBeenLastCalledWith({ enabled: true, repeated_action_threshold: 7, file_reread_threshold: 8 }, expect.anything());
    fireEvent.click(screen.getByLabelText("Stop a run that goes in circles"));
    expect(state.save).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: false }), expect.anything());
  });

  it("is inert for viewers", async () => {
    render(false);
    expect(((await screen.findByLabelText("File re-read threshold")) as HTMLInputElement).disabled).toBe(true);
  });
});
