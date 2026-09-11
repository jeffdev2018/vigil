// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import type { RunHalt } from "@multica/core/run-halt";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  halt: { halted: false, reason: "", halted_by: "", halted_at: null, frozen_count: 0, resumed_count: 0 } as RunHalt,
  setRunHalt: vi.fn(),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/run-halt", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/run-halt")>()),
  runHaltOptions: () => ({ queryKey: ["run-halt"], queryFn: async () => state.halt }),
  useSetRunHalt: () => ({ mutate: state.setRunHalt, isPending: false }),
}));

import { RunHaltSetting } from "./run-halt-setting";

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunHaltSetting wsId="ws-1" canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.halt = { halted: false, reason: "", halted_by: "", halted_at: null, frozen_count: 0, resumed_count: 0 };
  state.setRunHalt.mockReset();
  vi.mocked(toast.success).mockClear();
});

describe("RunHaltSetting", () => {
  it("toggling on halts immediately with the current reason", async () => {
    render();
    await screen.findByRole("switch", { name: "Halt all agents" });
    fireEvent.change(screen.getByRole("textbox", { name: "Reason" }), { target: { value: "investigating" } });
    fireEvent.click(screen.getByRole("switch", { name: "Halt all agents" }));
    expect(state.setRunHalt).toHaveBeenCalledWith({ halted: true, reason: "investigating" }, expect.anything());
  });

  // Regression: the reason was seeded before the query resolved, so it read
  // empty, and re-halting sent reason:"" over the stored one.
  it("shows the stored reason once the halt loads and keeps it on re-halt", async () => {
    state.halt = { halted: true, reason: "incident #42", halted_by: "u1", halted_at: "2026-09-09T09:00:00Z", frozen_count: 0, resumed_count: 0 };
    render();
    const input = screen.getByRole("textbox", { name: "Reason" });
    await waitFor(() => expect(input).toHaveValue("incident #42"));
    fireEvent.click(screen.getByRole("switch", { name: "Halt all agents" }));
    expect(state.setRunHalt).toHaveBeenCalledWith({ halted: false, reason: "incident #42" }, expect.anything());
  });

  it("keeps the reason field disabled while not halted", async () => {
    render();
    expect(await screen.findByRole("textbox", { name: "Reason" })).toBeDisabled();
  });

  it("commits an edited reason on blur while halted", async () => {
    state.halt = { halted: true, reason: "old reason", halted_by: "u1", halted_at: "2026-09-09T09:00:00Z", frozen_count: 0, resumed_count: 0 };
    render();
    const input = screen.getByRole("textbox", { name: "Reason" });
    await waitFor(() => expect(input).not.toBeDisabled());
    fireEvent.change(input, { target: { value: "new reason" } });
    fireEvent.blur(input);
    expect(state.setRunHalt).toHaveBeenCalledWith({ halted: true, reason: "new reason" }, expect.anything());
  });

  it("toasts how many in-flight runs the halt froze", async () => {
    state.setRunHalt.mockImplementation((_input: unknown, opts: { onSuccess: (data: RunHalt) => void }) =>
      opts.onSuccess({ halted: true, reason: "", halted_by: "u1", halted_at: null, frozen_count: 4, resumed_count: 0 }),
    );
    render();
    fireEvent.click(await screen.findByRole("switch", { name: "Halt all agents" }));
    expect(toast.success).toHaveBeenCalledWith("Froze 4 in-flight runs");
  });

  it("toasts how many frozen runs lifting the halt resumed", async () => {
    state.halt = { halted: true, reason: "", halted_by: "u1", halted_at: null, frozen_count: 2, resumed_count: 0 };
    state.setRunHalt.mockImplementation((_input: unknown, opts: { onSuccess: (data: RunHalt) => void }) =>
      opts.onSuccess({ halted: false, reason: "", halted_by: "", halted_at: null, frozen_count: 0, resumed_count: 2 }),
    );
    render();
    fireEvent.click(await screen.findByRole("switch", { name: "Halt all agents" }));
    expect(toast.success).toHaveBeenCalledWith("Resumed 2 frozen runs");
  });

  it("keeps to the plain saved toast when the halt froze nothing", async () => {
    state.setRunHalt.mockImplementation((_input: unknown, opts: { onSuccess: (data: RunHalt) => void }) =>
      opts.onSuccess({ halted: true, reason: "", halted_by: "u1", halted_at: null, frozen_count: 0, resumed_count: 0 }),
    );
    render();
    fireEvent.click(await screen.findByRole("switch", { name: "Halt all agents" }));
    expect(toast.success).toHaveBeenCalledTimes(1);
    expect(toast.success).not.toHaveBeenCalledWith(expect.stringContaining("Froze"));
  });

  it("disables both controls when the reader may not edit workspace settings", () => {
    render(false);
    expect(screen.getByRole("switch", { name: "Halt all agents" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("textbox", { name: "Reason" })).toBeDisabled();
  });
});
