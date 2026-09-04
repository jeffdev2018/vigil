// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["projects"], queryFn: async () => [{ id: "p1", title: "Core" }, { id: "p2", title: "Docs" }] }) }));
vi.mock("@multica/core/issues/cross-review", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/cross-review")>()),
  crossReviewSettingsOptions: () => ({ queryKey: ["cross-review-settings"], queryFn: async () => ({ enabled: true, opt_out_project_ids: ["p2"] }) }),
  useSaveCrossReviewSettings: () => ({ mutate: state.save, isPending: false }),
}));

import { CrossReviewSetting } from "./cross-review-setting";

describe("CrossReviewSetting", () => {
  it("toggles the feature and the opted-out projects", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <CrossReviewSetting canEdit />
      </QueryClientProvider>,
    );
    const docs = (await screen.findByLabelText("Docs")) as HTMLInputElement;
    await waitFor(() => expect(docs.checked).toBe(true));
    fireEvent.click(screen.getByLabelText("Core"));
    expect(state.save).toHaveBeenCalledWith({ enabled: true, opt_out_project_ids: ["p2", "p1"] }, expect.anything());
    fireEvent.click(screen.getByRole("switch", { name: "Review every delivered diff with another provider" }));
    expect(state.save).toHaveBeenLastCalledWith(expect.objectContaining({ enabled: false }), expect.anything());
  });
});
