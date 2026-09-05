// @vitest-environment jsdom

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import type { RuntimeRoutingStats } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { RoutingBenchmarksCard } from "./routing-benchmarks-card";

const ROWS: RuntimeRoutingStats[] = [
  {
    runtime_id: "runtime-2",
    runtime_name: "Codex (mbp.local)",
    provider: "codex",
    model: "gpt-5",
    task_class: "review",
    samples: 3,
    success_rate: 0.67,
    avg_cost_usd: null,
    avg_duration_secs: null,
  },
  {
    runtime_id: "runtime-1",
    runtime_name: "Claude (mbp.local)",
    provider: "claude",
    model: "claude-sonnet-4-6",
    task_class: "bugfix",
    samples: 42,
    success_rate: 0.93,
    avg_cost_usd: 0.12,
    avg_duration_secs: 95,
  },
];

function renderCard(
  props: Partial<React.ComponentProps<typeof RoutingBenchmarksCard>> = {},
) {
  return renderWithI18n(
    <RoutingBenchmarksCard
      rows={ROWS}
      lessThanMinuteLabel="<1m"
      {...props}
    />,
  );
}

describe("RoutingBenchmarksCard", () => {
  afterEach(() => cleanup());

  it("renders the 90-day stats per runtime, model and task class", () => {
    renderCard();

    expect(screen.getByText("Smart routing benchmarks")).toBeInTheDocument();
    expect(
      screen.getByText("Last 90 days · per runtime, model and task class"),
    ).toBeInTheDocument();

    // Most-measured row first, regardless of fixture order.
    const cells = screen.getAllByRole("cell").map((c) => c.textContent);
    expect(cells[0]).toBe("Claude (mbp.local)");
    expect(cells).toContain("codex/gpt-5");

    expect(screen.getByText("42")).toBeInTheDocument();
    expect(screen.getByText("93%")).toBeInTheDocument();
    expect(screen.getByText("$0.12")).toBeInTheDocument();
    expect(screen.getByText("1m 35s")).toBeInTheDocument();
  });

  it("labels the task class instead of showing the router's raw token", () => {
    renderCard();

    expect(screen.getByText("Bug fix")).toBeInTheDocument();
    expect(screen.queryByText("bugfix")).not.toBeInTheDocument();
  });

  it("falls back to the raw value for a class it does not know", () => {
    // "review" is not one of the seven classes task_classify.go stamps, so a
    // newer backend's class must stay readable rather than render blank.
    renderCard();

    expect(screen.getByText("review")).toBeInTheDocument();
  });

  it("renders em dashes for null averages, never zeroes", () => {
    renderCard();

    // The thin codex row has no priced / timed samples.
    const row = screen.getByText("Codex (mbp.local)").closest("tr");
    expect(row?.textContent).toContain("—");
    expect(row?.textContent).not.toContain("$0.00");
  });

  it("shows the empty state when the router has no data yet", () => {
    renderCard({ rows: [] });

    expect(screen.getByText("No routing data yet")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("renders a skeleton while the stats are loading", () => {
    const { container } = renderCard({ rows: [], loading: true });

    expect(screen.queryByText("No routing data yet")).not.toBeInTheDocument();
    expect(container.querySelector('[aria-hidden="true"]')).not.toBeNull();
  });

  it("localizes the card", () => {
    renderWithI18n(
      <RoutingBenchmarksCard rows={[]} lessThanMinuteLabel="<1分钟" />,
      { locale: "zh-Hans" },
    );

    expect(screen.getByText("暂无路由数据")).toBeInTheDocument();
  });
});
