// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { RunHalt } from "@multica/core/run-halt";
import { renderWithI18n } from "../test/i18n";

// Parsing (the malformed-response fallback) is proven in
// packages/core/run-halt/schemas.test.ts. This suite proves only the banner:
// nothing while not halted, who/when/why while halted, and the lift button
// gated to owners/admins.

const state = vi.hoisted(() => ({
  halt: { halted: false, reason: "", halted_by: "", halted_at: null, frozen_count: 0, resumed_count: 0 } as RunHalt,
  role: "member" as string,
  setRunHalt: vi.fn(),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (sel: (s: unknown) => unknown) => sel({ user: { id: "u1" } }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [{ user_id: "u1", role: state.role }, { user_id: "u2", name: "Priya", role: "owner" }],
  }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getMemberName: (id: string) => (id === "u2" ? "Priya" : "Unknown") }),
}));
vi.mock("@multica/core/run-halt", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/run-halt")>()),
  runHaltOptions: () => ({ queryKey: ["run-halt"], queryFn: async () => state.halt }),
  useSetRunHalt: () => ({ mutate: state.setRunHalt, isPending: false }),
}));

import { RunHaltBanner } from "./run-halt-banner";

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <RunHaltBanner wsId="ws-1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.halt = { halted: false, reason: "", halted_by: "", halted_at: null, frozen_count: 0, resumed_count: 0 };
  state.role = "member";
  state.setRunHalt.mockReset();
});

describe("RunHaltBanner", () => {
  it("renders nothing while the workspace is not halted", async () => {
    render();
    await waitFor(() => expect(screen.queryByTestId("run-halt-banner")).toBeNull());
  });

  it("names who halted it, when, and why", async () => {
    state.halt = { halted: true, reason: "an agent opened 40 PRs", halted_by: "u2", halted_at: "2026-09-09T09:00:00Z", frozen_count: 0, resumed_count: 0 };
    render();
    const banner = await screen.findByTestId("run-halt-banner");
    expect(banner.textContent).toContain("Agents are halted");
    expect(banner.textContent).toContain("Priya");
    expect(banner.textContent).toContain("an agent opened 40 PRs");
  });

  it("shows how many in-flight runs the halt froze", async () => {
    state.halt = { halted: true, reason: "", halted_by: "u2", halted_at: null, frozen_count: 3, resumed_count: 0 };
    render();
    const banner = await screen.findByTestId("run-halt-banner");
    expect(banner.textContent).toContain("3 runs frozen");
  });

  it("says nothing about frozen runs when the halt froze none", async () => {
    state.halt = { halted: true, reason: "", halted_by: "u2", halted_at: null, frozen_count: 0, resumed_count: 0 };
    render();
    const banner = await screen.findByTestId("run-halt-banner");
    expect(banner.textContent).not.toContain("frozen");
  });

  it("falls back to a generic actor when halted_by is empty", async () => {
    state.halt = { halted: true, reason: "", halted_by: "", halted_at: null, frozen_count: 0, resumed_count: 0 };
    render();
    const banner = await screen.findByTestId("run-halt-banner");
    expect(banner.textContent).toContain("an owner");
  });

  it("hides the lift button from a plain member", async () => {
    state.halt = { halted: true, reason: "", halted_by: "u2", halted_at: null, frozen_count: 0, resumed_count: 0 };
    state.role = "member";
    render();
    await screen.findByTestId("run-halt-banner");
    expect(screen.queryByRole("button", { name: "Lift the halt" })).toBeNull();
  });

  it("lets an owner lift the halt", async () => {
    state.halt = { halted: true, reason: "", halted_by: "u2", halted_at: null, frozen_count: 0, resumed_count: 0 };
    state.role = "owner";
    render();
    await screen.findByTestId("run-halt-banner");
    fireEvent.click(screen.getByRole("button", { name: "Lift the halt" }));
    expect(state.setRunHalt).toHaveBeenCalledWith({ halted: false, reason: "" }, expect.anything());
  });
});
