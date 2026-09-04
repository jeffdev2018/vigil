// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import type { TrustModeChange, TrustSuggestion } from "@multica/core/agents/trust";
import { renderWithI18n } from "../../../test/i18n";

// Client parsing: packages/core/agents/trust.test.ts.

const state = vi.hoisted(() => ({
  mode: "propose",
  suggestion: null as TrustSuggestion | null,
  history: [] as TrustModeChange[],
  set: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/agents/trust", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents/trust")>()),
  agentTrustModeOptions: () => ({ queryKey: ["mode"], queryFn: async () => ({ agent_id: "a1", mode: state.mode, modes: [] }) }),
  agentTrustSuggestionOptions: () => ({ queryKey: ["sugg"], queryFn: async () => state.suggestion }),
  agentTrustHistoryOptions: () => ({ queryKey: ["hist"], queryFn: async () => state.history }),
  useSetAgentTrustMode: () => ({ mutate: state.set, isPending: false }),
}));

import { TrustTab } from "./trust-tab";

const agent = { id: "a1", name: "Builder", trust_mode: "propose" } as unknown as Agent;
const suggestion = (over: Partial<TrustSuggestion> = {}): TrustSuggestion => ({
  eligible: true, current_mode: "propose", suggested_mode: "approval",
  metrics: { days: 30, runs_total: 12, accepted_rate: 0.92, no_intervention_rate: 0.75, reopen_rate: 0 },
  thresholds: { days: 30, min_runs: 10, min_accepted_rate: 0.8, min_no_intervention_rate: 0.7, max_reopen_rate: 0.1 }, reasons: [], ...over,
});

function render(canEdit = true) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <TrustTab agent={agent} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.mode = "propose";
  state.suggestion = null;
  state.history = [];
  state.set.mockReset();
});

describe("TrustTab", () => {
  it("shows the current mode and changes it only after a confirmed click with a reason", async () => {
    render();
    const current = await screen.findByRole("radio", { checked: true });
    expect(current.getAttribute("data-mode")).toBe("propose");
    fireEvent.click(screen.getByRole("radio", { name: /Observer/ }));
    expect(state.set).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Reason"), { target: { value: "incident on billing" } });
    fireEvent.click(screen.getByRole("button", { name: "Confirm" }));
    expect(state.set).toHaveBeenCalledWith({ mode: "observer", reason: "incident on billing" }, expect.anything());
  });

  it("shows the promotion suggestion with its numbers and the demotion in the history", async () => {
    state.suggestion = suggestion();
    state.history = [
      { id: "c2", from_mode: "approval", to_mode: "observer", reason: "incident", triggered_by_type: "member", triggered_by_id: "u", created_at: "2026-09-04T00:00:00Z", demotion: true },
      { id: "c1", from_mode: "propose", to_mode: "approval", reason: null, triggered_by_type: "member", triggered_by_id: "u", created_at: "2026-09-01T00:00:00Z", demotion: false },
    ];
    render();
    const banner = await screen.findByTestId("trust-suggestion");
    expect(banner.textContent).toContain("12 runs");
    expect(banner.textContent).toContain("92%");
    fireEvent.click(screen.getByRole("button", { name: /Promote to/ }));
    expect(screen.getByTestId("trust-confirm")).toBeTruthy();
    const changes = screen.getAllByTestId("trust-change");
    expect(changes[0]?.getAttribute("data-demotion")).toBe("true");
    expect(changes[0]?.textContent).toContain("incident");
    expect(changes[1]?.getAttribute("data-demotion")).toBe("false");
  });

  it("explains why the agent is not eligible yet and locks the dial for non-managers", async () => {
    state.suggestion = suggestion({ eligible: false, suggested_mode: undefined, reasons: ["3 runs in 30 days, 10 needed"] });
    render(false);
    expect((await screen.findByTestId("trust-not-yet")).textContent).toContain("10 needed");
    expect(screen.queryByTestId("trust-suggestion")).toBeNull();
    expect((screen.getByRole("radio", { name: /Autonomous/ }) as HTMLButtonElement).disabled).toBe(true);
  });
});
