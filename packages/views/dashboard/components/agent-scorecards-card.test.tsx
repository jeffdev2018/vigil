// @vitest-environment jsdom

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";

const mockListWorkspaceScorecards = vi.hoisted(() => vi.fn());
const mockListAgents = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return {
    ...actual,
    api: {
      listWorkspaceScorecards: (...args: unknown[]) =>
        mockListWorkspaceScorecards(...args),
      listAgents: (...args: unknown[]) => mockListAgents(...args),
    },
  };
});

import { AgentScorecardsCard } from "./agent-scorecards-card";

const ROW = {
  agent_id: "agent-1",
  runtime_id: "runtime-1",
  runs_total: 10,
  runs_failed: 1,
  runs_cancelled: 0,
  runs_accepted: 8,
  runs_reopened: 2,
  runs_no_intervention: 6,
  cost_usd_ticks_total: 1e10,
  low_sample: false,
};

function renderCard(locale?: "zh-Hans") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <AgentScorecardsCard wsId="ws-1" days={30} />
    </QueryClientProvider>,
    locale ? { locale } : {},
  );
}

describe("AgentScorecardsCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListWorkspaceScorecards.mockResolvedValue([ROW]);
    mockListAgents.mockResolvedValue([{ id: "agent-1", name: "Ada" }]);
  });
  afterEach(() => cleanup());

  // The rollup this card reads has no project dimension and buckets by UTC
  // day, so the card must never look like it followed the page's project
  // filter or display timezone.
  it("says the numbers are workspace-wide and UTC-bucketed", async () => {
    renderCard();

    expect(await screen.findByText("Ada")).toBeInTheDocument();
    expect(
      screen.getByText("Workspace-wide, all projects · UTC days"),
    ).toBeInTheDocument();
  });

  it("localizes the scope note", async () => {
    renderCard("zh-Hans");

    expect(await screen.findByText("Ada")).toBeInTheDocument();
    expect(
      screen.getByText("覆盖整个工作区的所有项目 · 按 UTC 日期统计"),
    ).toBeInTheDocument();
  });

  it("renders nothing at all when the workspace has no scorecard rows", () => {
    mockListWorkspaceScorecards.mockResolvedValue([]);

    const { container } = renderCard();

    expect(container.querySelector('[data-testid="agent-scorecards"]')).toBeNull();
  });
});
