// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/agents/competency", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/competency")>()),
  competencySettingsOptions: () => ({ queryKey: ["competency-settings"], queryFn: async () => ({ min_sample: 5 }) }),
  useSaveCompetencySettings: () => ({ mutate: state.save, isPending: false }),
}));

import { CompetencySetting } from "./competency-setting";

describe("CompetencySetting", () => {
  it("shows the threshold and saves a clamped value on blur", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={qc}>
        <CompetencySetting canEdit />
      </QueryClientProvider>,
    );
    const input = (await screen.findByLabelText("Minimum sample")) as HTMLInputElement;
    await waitFor(() => expect(input.value).toBe("5"));
    fireEvent.change(input, { target: { value: "0" } });
    fireEvent.blur(input);
    expect(state.save).toHaveBeenCalledWith({ min_sample: 1 }, expect.anything());
    fireEvent.change(input, { target: { value: "12" } });
    fireEvent.blur(input);
    expect(state.save).toHaveBeenLastCalledWith({ min_sample: 12 }, expect.anything());
  });
});
