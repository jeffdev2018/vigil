// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderWithI18n } from "../../test/i18n";

// Off-peak hours (K45). The window validation matrix lives beside the helper in
// packages/core/batch-window/schemas.test.ts; this suite covers the two states
// the reader must tell apart — no window vs a live one — and the wiring that
// turns an edited time into a saved window.

const state = vi.hoisted(() => ({
  save: vi.fn(),
  error: vi.fn(),
  window: {
    enabled: false,
    start_local_time: "",
    end_local_time: "",
    timezone: "UTC",
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: state.error } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/batch-window", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/batch-window")>()),
  batchWindowOptions: () => ({
    queryKey: ["batch-window", JSON.stringify(state.window)],
    queryFn: async () => state.window,
  }),
  useUpdateBatchWindow: () => ({ mutate: state.save, isPending: false }),
}));

import { BatchWindowSetting } from "./batch-window-setting";

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <BatchWindowSetting canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

function reset(window: Partial<typeof state.window> = {}) {
  state.save.mockClear();
  state.error.mockClear();
  state.window = { enabled: false, start_local_time: "", end_local_time: "", timezone: "UTC", ...window };
}

describe("BatchWindowSetting", () => {
  it("says nothing waits when no window is configured", async () => {
    reset();
    render();
    expect(
      await screen.findByText(
        "No off-peak window: every autopilot run is dispatched as soon as it fires.",
      ),
    ).toBeTruthy();
  });

  it("says what is affected once a window is live", async () => {
    reset({ enabled: true, start_local_time: "22:00", end_local_time: "06:00", timezone: "Europe/Paris" });
    render();
    // The note must name the boundary: only SCHEDULED runs of opted-in
    // autopilots wait, so nobody reads the window as a global slowdown.
    expect(
      await screen.findByText(
        "Only scheduled runs of autopilots marked as able to wait are affected. Running one now is never delayed.",
      ),
    ).toBeTruthy();
    expect((screen.getByLabelText("Window start") as HTMLInputElement).value).toBe("22:00");
    expect((screen.getByLabelText("Window end") as HTMLInputElement).value).toBe("06:00");
  });

  it("enabling with no times offers a usable window instead of a validation error", async () => {
    reset();
    render();
    fireEvent.click(await screen.findByLabelText("Enable off-peak hours"));
    await waitFor(() => expect(state.save).toHaveBeenCalled());
    expect(state.save.mock.calls[0]?.[0]).toMatchObject({
      enabled: true,
      start_local_time: "22:00",
      end_local_time: "06:00",
    });
    expect(state.error).not.toHaveBeenCalled();
  });

  it("commits an edited time as soon as the field reports a complete one", async () => {
    reset({ enabled: true, start_local_time: "22:00", end_local_time: "06:00" });
    render();
    const start = await screen.findByLabelText("Window start");
    // The fields render from the query, so wait for the stored window to land:
    // editing the pre-load blank would only prove the empty-field guard.
    await waitFor(() => expect((start as HTMLInputElement).value).toBe("22:00"));
    fireEvent.change(start, { target: { value: "23:30" } });
    await waitFor(() => expect(state.save).toHaveBeenCalled());
    expect(state.save.mock.calls[0]?.[0]).toMatchObject({
      start_local_time: "23:30",
      end_local_time: "06:00",
    });
  });

  it("holds the save while a field is cleared, and refuses an equal-bounds window without a round trip", async () => {
    reset({ enabled: true, start_local_time: "22:00", end_local_time: "06:00" });
    render();
    const endField = await screen.findByLabelText("Window end");
    await waitFor(() => expect((endField as HTMLInputElement).value).toBe("06:00"));
    // Clearing is a step on the way to retyping, not a window to save or warn about.
    fireEvent.change(endField, { target: { value: "" } });
    expect(state.save).not.toHaveBeenCalled();
    expect(state.error).not.toHaveBeenCalled();
    const end = await screen.findByLabelText("Window end");
    fireEvent.change(end, { target: { value: "22:00" } });
    await waitFor(() => expect(state.error).toHaveBeenCalled());
    expect(state.save).not.toHaveBeenCalled();
  });

  it("is read-only for a member who cannot manage the workspace", async () => {
    reset({ enabled: true, start_local_time: "22:00", end_local_time: "06:00" });
    render(false);
    expect((await screen.findByLabelText("Window start")).hasAttribute("disabled")).toBe(true);
    const toggle = screen.getByLabelText("Enable off-peak hours") as HTMLButtonElement;
    expect(toggle.disabled || toggle.getAttribute("aria-disabled") === "true").toBe(true);
  });
});
