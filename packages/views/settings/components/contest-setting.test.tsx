// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["projects"], queryFn: async () => [{ id: "p1", title: "Core" }, { id: "p2", title: "Docs" }] }) }));
vi.mock("@multica/core/issues/contest", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/contest")>()),
  contestSettingsOptions: () => ({ queryKey: ["contest-settings"], queryFn: async () => ({ targets: { task_result: true, plan: false, triage_verdict: true, meeting_summary: true }, opt_out_project_ids: ["p2"] }) }),
  useSaveContestSettings: () => ({ mutate: state.save, isPending: false }),
}));

import { ContestSetting } from "./contest-setting";

describe("ContestSetting", () => {
  it("toggles each target type and the opted-out projects", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <ContestSetting canEdit />
      </QueryClientProvider>,
    );
    const docs = (await screen.findByLabelText("Docs")) as HTMLInputElement;
    await waitFor(() => expect(docs.checked).toBe(true));
    await waitFor(() => expect(screen.getByRole("switch", { name: "Plans" }).getAttribute("aria-checked")).toBe("false"));
    fireEvent.click(screen.getByRole("switch", { name: "Plans" }));
    expect(state.save).toHaveBeenCalledWith(expect.objectContaining({ targets: expect.objectContaining({ plan: true, task_result: true }) }), expect.anything());
    fireEvent.click(screen.getByLabelText("Core"));
    expect(state.save).toHaveBeenLastCalledWith(expect.objectContaining({ opt_out_project_ids: ["p2", "p1"] }), expect.anything());
  });
});
