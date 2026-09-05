// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ContestPreflight } from "@multica/core/issues/contest";
import { renderWithI18n } from "../../test/i18n";

// Cost/pairing helpers and query keys: packages/core/issues/contest.test.ts.

const state = vi.hoisted(() => ({ preflight: null as ContestPreflight | null, create: vi.fn() }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/issues/contest", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/issues/contest")>()),
  contestPreflightOptions: () => ({ queryKey: ["contest-preflight"], queryFn: async () => state.preflight }),
  useCreateContest: () => ({ mutate: state.create, isPending: false }),
}));

import { ContestButton } from "./contest-button";

const preflight = (over: Partial<ContestPreflight> = {}): ContestPreflight => ({
  target_type: "task_result", target_id: "t1", issue_id: "i1", author_agent_id: "a1", author_provider: "claude",
  challenger: { kind: "agent", agent_id: "a2", name: "Codex critic", provider: "codex", same_vendor: false },
  estimated_cost_usd_ticks: 1_230_000, quota_used: 2, quota_limit: 10, max_rounds: 2, existing: 1, ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <ContestButton targetType="task_result" targetId="t1" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.create.mockReset();
});

describe("ContestButton", () => {
  it("shows the challenger, the cost and the quota, then launches with one round by default", async () => {
    state.preflight = preflight();
    render();
    fireEvent.click(screen.getByTestId("contest-button"));
    expect((await screen.findByTestId("contest-challenger")).textContent).toContain("Codex critic");
    expect(screen.getByTestId("contest-challenger").textContent).toContain("codex");
    expect(screen.queryByText("same vendor, another model")).toBeNull();
    expect(screen.getByTestId("contest-cost").textContent).toBe("$1.23");
    expect(screen.getByTestId("contest-quota").textContent).toBe("2/10");
    expect(screen.getByTestId("contest-existing").textContent).toBe("1");
    expect(screen.getByRole("combobox", { name: "Rounds" })).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Launch" }));
    expect(state.create).toHaveBeenCalledWith({ target_type: "task_result", target_id: "t1", max_rounds: 1 }, expect.anything());
  });

  it("names a same-vendor challenger and a service model, and hides the rounds for the latter", async () => {
    state.preflight = preflight({ challenger: { kind: "llm", agent_id: "", name: "", provider: "claude", same_vendor: true }, estimated_cost_usd_ticks: 0 });
    render();
    fireEvent.click(screen.getByTestId("contest-button"));
    expect((await screen.findByText("same vendor, another model"))).toBeTruthy();
    expect(screen.getByText("service model")).toBeTruthy();
    expect(screen.getByTestId("contest-cost").textContent).toBe("—");
    expect(screen.queryByRole("combobox", { name: "Rounds" })).toBeNull();
  });

  it("disables the launch and says why when the daily quota is reached", async () => {
    state.preflight = preflight({ quota_used: 10, quota_limit: 10 });
    render();
    fireEvent.click(screen.getByTestId("contest-button"));
    expect(await screen.findByTestId("contest-quota-reached")).toBeTruthy();
    await waitFor(() => expect((screen.getByRole("button", { name: "Launch" }) as HTMLButtonElement).disabled).toBe(true));
    fireEvent.click(screen.getByRole("button", { name: "Launch" }));
    expect(state.create).not.toHaveBeenCalled();
  });
});
