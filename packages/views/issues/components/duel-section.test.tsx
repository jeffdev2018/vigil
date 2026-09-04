// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentDuel } from "@multica/core/issues/duel";
import { renderWithI18n } from "../../test/i18n";

// Parsing and formatting: packages/core/issues/duel.test.ts.

const state = vi.hoisted(() => ({ duel: null as AgentDuel | null, start: vi.fn(), confirm: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/workspace/queries", () => ({ agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [{ id: "a", name: "Alpha" }, { id: "b", name: "Beta" }] }) }));
vi.mock("@multica/core/issues/duel", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/duel")>()),
  issueDuelOptions: () => ({ queryKey: ["duel"], queryFn: async () => state.duel }),
  useStartDuel: () => ({ mutate: state.start, isPending: false }),
  useConfirmDuel: () => ({ mutate: state.confirm, isPending: false }),
}));

import { DuelSection } from "./duel-section";

const side = (agent: string, over: Partial<AgentDuel["a"]> = {}): AgentDuel["a"] => ({ agent_id: agent, task_id: "t-" + agent, task_status: "completed", outcome: "completed", cost_usd_ticks: 1_200_000_000, duration_seconds: 90, tool_calls: 4, quality_score: null, summary: "", ...over });
const duel = (over: Partial<AgentDuel> = {}): AgentDuel => ({ id: "d1", issue_id: "i1", status: "verdict_ready", a: side("a", { quality_score: 55 }), b: side("b", { quality_score: 85, summary: "Beta ran the suite." }), arbiter_winner: "b", reasoning: "Beta verified its work.", arbiter_error: null, winner: null, confirmed_by: null, confirmed_at: null, created_at: "", settled_at: "", ...over });

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <DuelSection issueId="i1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.duel = null;
  state.start.mockReset();
  state.confirm.mockReset();
});

describe("DuelSection", () => {
  it("launches a duel between two different agents", async () => {
    render();
    fireEvent.click(await screen.findByText("Start a duel"));
    fireEvent.change(screen.getByLabelText("Agent A"), { target: { value: "a" } });
    fireEvent.change(screen.getByLabelText("Agent B"), { target: { value: "a" } });
    expect(screen.getByText("Pick two different agents")).toBeTruthy();
    expect((screen.getByRole("button", { name: "Launch duel" }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByLabelText("Agent B"), { target: { value: "b" } });
    fireEvent.click(screen.getByRole("button", { name: "Launch duel" }));
    expect(state.start).toHaveBeenCalledWith({ agent_a_id: "a", agent_b_id: "b" }, expect.anything());
  });

  it("shows both candidates with metrics and the arbiter's pick, and confirms the human's choice", async () => {
    state.duel = duel();
    render();
    expect(await screen.findByTestId("duel")).toBeTruthy();
    expect(screen.getAllByTestId("duel-candidate").map((el) => el.getAttribute("data-side"))).toEqual(["a", "b"]);
    expect(screen.getAllByText("$0.12")).toHaveLength(2);
    expect(screen.getByText("85/100")).toBeTruthy();
    expect(screen.getByTestId("duel-arbiter").textContent).toContain("Arbiter's pick: Beta");
    expect(screen.getByTestId("duel-arbiter").textContent).toContain("Beta verified its work.");
    fireEvent.click(screen.getByRole("button", { name: "Confirm Alpha" }));
    expect(state.confirm).toHaveBeenCalledWith({ duelId: "d1", winner: "a" }, expect.anything());
    expect(screen.queryByText("Start a duel")).toBeTruthy();
  });

  it("shows a confirmed winner, and the inconclusive state without buttons", async () => {
    state.duel = duel({ status: "confirmed", winner: "tie", confirmed_by: "u1" });
    const { unmount } = render();
    expect((await screen.findByTestId("duel-winner")).textContent).toBe("Declared a tie");
    expect(screen.queryByRole("button", { name: /Confirm/ })).toBeNull();
    unmount();
    state.duel = duel({ status: "inconclusive", arbiter_winner: null, reasoning: "", b: side("b", { outcome: "failed", task_status: "failed" }) });
    render();
    expect(await screen.findByTestId("duel-inconclusive")).toBeTruthy();
    expect(screen.queryByTestId("duel-arbiter")).toBeNull();
    expect(screen.queryByRole("button", { name: /Confirm/ })).toBeNull();
  });

  it("hides the launch form while a duel runs", async () => {
    state.duel = duel({ status: "running", arbiter_winner: null, a: side("a", { outcome: null, task_status: "running" }), b: side("b", { outcome: null, task_status: "queued" }) });
    render();
    expect(await screen.findByTestId("duel")).toBeTruthy();
    expect(screen.queryByText("Start a duel")).toBeNull();
    expect(screen.queryByTestId("duel-arbiter")).toBeNull();
  });
});
