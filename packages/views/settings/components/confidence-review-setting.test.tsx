// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/issues/confidence-review", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/confidence-review")>()),
  confidenceReviewSettingsOptions: () => ({ queryKey: ["confidence-review-settings"], queryFn: async () => ({ enabled: true, threshold: 0.5 }) }),
  useSaveConfidenceReviewSettings: () => ({ mutate: state.save, isPending: false }),
}));

import { ConfidenceReviewSetting } from "./confidence-review-setting";

describe("ConfidenceReviewSetting", () => {
  it("toggles the feature and persists the threshold", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <ConfidenceReviewSetting canEdit />
      </QueryClientProvider>,
    );
    const threshold = (await screen.findByLabelText("Confidence threshold")) as HTMLInputElement;
    await waitFor(() => expect(threshold.value).toBe("0.5"));
    fireEvent.change(threshold, { target: { value: "0.7" } });
    fireEvent.blur(threshold);
    expect(state.save).toHaveBeenCalledWith({ enabled: true, threshold: 0.7 }, expect.anything());
    fireEvent.click(screen.getByRole("switch", { name: "Send low-confidence runs to review" }));
    expect(state.save).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: false }), expect.anything());
  });

  it("clamps an out-of-contract threshold before persisting", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <ConfidenceReviewSetting canEdit />
      </QueryClientProvider>,
    );
    const threshold = (await screen.findByLabelText("Confidence threshold")) as HTMLInputElement;
    await waitFor(() => expect(threshold.value).toBe("0.5"));
    // The API rejects anything outside 0 < threshold ≤ 1; blur clamps first.
    fireEvent.change(threshold, { target: { value: "2" } });
    fireEvent.blur(threshold);
    expect(state.save).toHaveBeenCalledWith({ enabled: true, threshold: 1 }, expect.anything());
  });
});
