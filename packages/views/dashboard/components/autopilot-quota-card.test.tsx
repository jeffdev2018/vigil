// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, screen } from "@testing-library/react";
import type { AutopilotQuotaUsage } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockGetAutopilotQuotaUsage = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: {
      getAutopilotQuotaUsage: (...args: unknown[]) =>
        mockGetAutopilotQuotaUsage(...args),
    },
  };
});

import { AutopilotQuotaCard } from "./autopilot-quota-card";

function usage(over: Partial<AutopilotQuotaUsage> = {}): AutopilotQuotaUsage {
  return {
    action: "enforce",
    used: 10,
    reserved: 0,
    total: 10,
    limit: 100,
    reached: false,
    period_start: "2026-09-01T00:00:00Z",
    period_end: "2026-10-01T00:00:00Z",
    reset_at: "2026-10-01T00:00:00Z",
    blocked_counts: null,
    ...over,
  };
}

function renderCard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <AutopilotQuotaCard wsId="ws-1" />
    </QueryClientProvider>,
  );
}

describe("AutopilotQuotaCard", () => {
  beforeEach(() => vi.clearAllMocks());
  afterEach(() => cleanup());

  it("states how much of the period's allowance is spent", async () => {
    mockGetAutopilotQuotaUsage.mockResolvedValue(usage());

    renderCard();

    expect(
      await screen.findByText("Autopilot runs this period: 10 / 100"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("autopilot-quota")).toHaveAttribute(
      "data-state",
      "ok",
    );
  });

  it("counts reserved runs, which the quota already charges against", async () => {
    mockGetAutopilotQuotaUsage.mockResolvedValue(
      usage({ used: 70, reserved: 12, total: 82 }),
    );

    renderCard();

    expect(
      await screen.findByText("Autopilot runs this period: 82 / 100"),
    ).toBeInTheDocument();
    expect(screen.getByTestId("autopilot-quota")).toHaveAttribute(
      "data-state",
      "warning",
    );
  });

  it("warns from 80% of the allowance", async () => {
    mockGetAutopilotQuotaUsage.mockResolvedValue(usage({ used: 80, total: 80 }));

    renderCard();

    expect(await screen.findByText(/Close to the limit/)).toBeInTheDocument();
  });

  it("says autopilots have stopped once the limit is reached", async () => {
    mockGetAutopilotQuotaUsage.mockResolvedValue(
      usage({ used: 100, total: 100, reached: true }),
    );

    renderCard();

    expect(await screen.findByText(/Limit reached/)).toBeInTheDocument();
    expect(screen.getByTestId("autopilot-quota")).toHaveAttribute(
      "data-state",
      "reached",
    );
  });

  it("renders nothing when the workspace has no quota to report", async () => {
    // Enforcement off and an unlimited plan are both "no number to show".
    for (const over of [{ action: "off" as const }, { limit: null }]) {
      mockGetAutopilotQuotaUsage.mockResolvedValue(usage(over));
      const { container } = renderCard();
      await vi.waitFor(() =>
        expect(mockGetAutopilotQuotaUsage).toHaveBeenCalled(),
      );
      expect(container.querySelector('[data-testid="autopilot-quota"]')).toBeNull();
      cleanup();
    }
  });
});
