// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunHalt } from "@multica/core/run-halt";
import { renderWithI18n } from "../../test/i18n";

const state = vi.hoisted(() => ({
  halt: { halted: false, reason: "", halted_by: "", halted_at: null } as RunHalt,
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
  state.halt = { halted: false, reason: "", halted_by: "", halted_at: null };
  state.setRunHalt.mockReset();
});

describe("RunHaltSetting", () => {
  it("toggling on halts immediately with the current reason", async () => {
    render();
    await screen.findByRole("switch", { name: "Halt all agents" });
    fireEvent.change(screen.getByRole("textbox", { name: "Reason" }), { target: { value: "investigating" } });
    fireEvent.click(screen.getByRole("switch", { name: "Halt all agents" }));
    expect(state.setRunHalt).toHaveBeenCalledWith({ halted: true, reason: "investigating" }, expect.anything());
  });

  it("keeps the reason field disabled while not halted", async () => {
    render();
    expect(await screen.findByRole("textbox", { name: "Reason" })).toBeDisabled();
  });

  it("commits an edited reason on blur while halted", async () => {
    state.halt = { halted: true, reason: "old reason", halted_by: "u1", halted_at: "2026-09-09T09:00:00Z" };
    render();
    const input = screen.getByRole("textbox", { name: "Reason" });
    await waitFor(() => expect(input).not.toBeDisabled());
    fireEvent.change(input, { target: { value: "new reason" } });
    fireEvent.blur(input);
    expect(state.setRunHalt).toHaveBeenCalledWith({ halted: true, reason: "new reason" }, expect.anything());
  });

  it("disables both controls when the reader may not edit workspace settings", () => {
    render(false);
    expect(screen.getByRole("switch", { name: "Halt all agents" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getByRole("textbox", { name: "Reason" })).toBeDisabled();
  });
});
