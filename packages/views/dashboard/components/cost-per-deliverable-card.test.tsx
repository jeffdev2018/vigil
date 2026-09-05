// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { DashboardCostPerDeliverable } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Schema fallbacks: packages/core/dashboard/cost-per-deliverable.test.ts.

const state = vi.hoisted(() => ({ data: null as DashboardCostPerDeliverable | null }));

vi.mock("@multica/core/dashboard/queries", () => ({
  dashboardCostPerDeliverableOptions: (wsId: string, days: number, projectId: string | null, tz: string) => ({
    queryKey: ["dashboard", wsId, "cost-per-deliverable", days, projectId, tz],
    queryFn: async () => state.data,
  }),
}));
vi.mock("@multica/ui/components/ui/number-flow", () => ({
  CurrencyNumberFlow: ({ value }: { value: number }) => <span>${value.toFixed(2)}</span>,
}));

import { CostPerDeliverableCard } from "./cost-per-deliverable-card";

const stats = (over: Partial<DashboardCostPerDeliverable["issues"]> = {}) => ({
  count: 0, mean_usd_ticks: 0, median_usd_ticks: 0, total_usd_ticks: 0, uncosted_count: 0, trend_pct: null, ...over,
});

function renderCard() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <CostPerDeliverableCard wsId="ws-1" days={30} projectId={null} tz="UTC" locales="en" />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  state.data = null;
});

describe("CostPerDeliverableCard", () => {
  it("says when nothing was delivered instead of showing zero", async () => {
    state.data = { days: 30, issues: stats(), pull_requests: stats() };
    renderCard();
    const card = await screen.findByTestId("cost-per-deliverable");
    expect(card.dataset.empty).toBe("true");
    expect(card.textContent).toContain("No issue closed or pull request merged");
  });

  it("shows the median with the mean, the floor note and the trend", async () => {
    state.data = {
      days: 30,
      issues: stats({ count: 3, median_usd_ticks: 4_000_000_000, mean_usd_ticks: 5_000_000_000, uncosted_count: 1, trend_pct: -50 }),
      pull_requests: stats({ count: 1, median_usd_ticks: 8_000_000_000, mean_usd_ticks: 8_000_000_000, trend_pct: 25 }),
    };
    renderCard();
    const card = await screen.findByTestId("cost-per-deliverable");
    expect(card.textContent).toContain("$0.40");
    expect(card.textContent).toContain("3 closed · mean $0.50");
    expect(card.textContent).toContain("some usage unpriced");
    const trends = screen.getAllByTestId("deliverable-trend").map((el) => el.textContent);
    expect(trends).toEqual(["-50%", "+25%"]);
  });
});
