// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { HandoffPacket } from "@multica/core/issues/handoff";
import { renderWithI18n } from "../../test/i18n";

// Parsing and line splitting: packages/core/issues/handoff.test.ts.

const state = vi.hoisted(() => ({ packets: [] as HandoffPacket[], runs: [] as { id: string; created_at: string }[], create: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/handoff", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/handoff")>()),
  issueHandoffPacketsOptions: () => ({ queryKey: ["hp"], queryFn: async () => state.packets }),
  issueRunsOptions: () => ({ queryKey: ["runs"], queryFn: async () => state.runs }),
  useCreateHandoffPacket: () => ({ mutate: state.create, isPending: false }),
}));

import { HandoffPacketCard } from "./handoff-packet-card";

const packet = (over: Partial<HandoffPacket>): HandoffPacket => ({
  id: "p", run_id: "0123456789", issue_id: "i1", objective: "Ship the fix", decisions: [], evidence: [], failed_attempts: [], next_action: "", created_by_type: "agent", created_by_id: "a", created_at: "2026-09-04T10:00:00Z", ...over,
});

function render(latestRunId: string | null = "run-1", canWrite = true) {
  state.runs = latestRunId ? [{ id: latestRunId, created_at: "2026-09-04T10:00:00Z" }] : [];
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <HandoffPacketCard issueId="i1" canWrite={canWrite} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.packets = [];
  state.create.mockReset();
});

describe("HandoffPacketCard", () => {
  it("renders nothing without a packet nor a run to hand off", async () => {
    const { container } = render(null);
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toBe("");
  });

  it("shows the latest packet with its sections and hides older ones behind history", async () => {
    state.packets = [packet({ id: "old", objective: "First try" }), packet({ id: "new", objective: "Ship the fix", failed_attempts: ["dropping the table"], next_action: "Open the PR", created_by_type: "system" })];
    render();
    expect(await screen.findByText("Ship the fix")).toBeTruthy();
    expect(screen.getByText("dropping the table")).toBeTruthy();
    expect(screen.getByText("Open the PR")).toBeTruthy();
    expect(screen.getByText("system")).toBeTruthy();
    expect(screen.queryByText("First try")).toBeNull();
    fireEvent.click(screen.getByText("Show 1 earlier packet"));
    expect(screen.getByText("First try")).toBeTruthy();
  });

  it("lets a member leave a packet against the latest run", async () => {
    render("run-1");
    fireEvent.click(await screen.findByText("Write a handoff packet"));
    fireEvent.change(screen.getByLabelText("Objective"), { target: { value: "Finish the migration" } });
    fireEvent.change(screen.getByLabelText("Failed attempts"), { target: { value: "a\n\nb" } });
    fireEvent.change(screen.getByLabelText("Next action"), { target: { value: "Run the backfill" } });
    fireEvent.click(screen.getByRole("button", { name: "Save packet" }));
    expect(state.create).toHaveBeenCalledWith({ run_id: "run-1", objective: "Finish the migration", decisions: [], evidence: [], failed_attempts: ["a", "b"], next_action: "Run the backfill" }, expect.anything());
  });
});
