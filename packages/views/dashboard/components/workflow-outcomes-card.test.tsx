// @vitest-environment jsdom

import { afterEach, describe, expect, it } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import type { WorkflowStats } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { WorkflowOutcomesCard } from "./workflow-outcomes-card";

const ROWS: WorkflowStats[] = [
  {
    task_class: "docs",
    workflow: "critique",
    samples: 3,
    success_rate: 0.67,
    avg_cost_usd: null,
    avg_duration_secs: null,
  },
  {
    task_class: "bugfix",
    workflow: "single",
    samples: 12,
    success_rate: 0.75,
    avg_cost_usd: 0.05,
    avg_duration_secs: 180,
  },
  {
    task_class: "bugfix",
    workflow: "cascade",
    samples: 8,
    success_rate: 0.5,
    avg_cost_usd: 0.21,
    avg_duration_secs: 540,
  },
];

function renderCard(
  props: Partial<React.ComponentProps<typeof WorkflowOutcomesCard>> = {},
) {
  return renderWithI18n(
    <WorkflowOutcomesCard
      rows={ROWS}
      lessThanMinuteLabel="<1m"
      {...props}
    />,
  );
}

describe("WorkflowOutcomesCard", () => {
  afterEach(() => cleanup());

  it("renders the 90-day stats per task class and workflow", () => {
    renderCard();

    expect(screen.getByText("Workflow outcomes")).toBeInTheDocument();
    expect(
      screen.getByText("Last 90 days · per task class and workflow"),
    ).toBeInTheDocument();

    // Most-measured row first, regardless of fixture order.
    const cells = screen.getAllByRole("cell").map((c) => c.textContent);
    expect(cells[0]).toBe("Bug fix");
    expect(cells[1]).toBe("Single");

    expect(screen.getByText("12")).toBeInTheDocument();
    expect(screen.getByText("75%")).toBeInTheDocument();
    expect(screen.getByText("$0.05")).toBeInTheDocument();
    expect(screen.getByText("3m")).toBeInTheDocument();
    expect(screen.getByText("Cascade")).toBeInTheDocument();
    expect(screen.getByText("Critique")).toBeInTheDocument();
  });

  it("renders em dashes for null averages, never zeroes", () => {
    renderCard();

    // The thin critique row has no priced / timed samples.
    const row = screen.getByText("Critique").closest("tr");
    expect(row?.textContent).toContain("—");
    expect(row?.textContent).not.toContain("$0.00");
  });

  it("falls back to the raw value for a workflow it does not know", () => {
    renderCard({
      rows: [
        {
          task_class: "general",
          workflow: "debate",
          samples: 1,
          success_rate: 1,
          avg_cost_usd: 0.01,
          avg_duration_secs: 30,
        },
      ],
    });

    expect(screen.getByText("debate")).toBeInTheDocument();
  });

  it("shows the empty state when the selector has no data yet", () => {
    renderCard({ rows: [] });

    expect(screen.getByText("No workflow data yet")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("renders a skeleton while the stats are loading", () => {
    const { container } = renderCard({ rows: [], loading: true });

    expect(screen.queryByText("No workflow data yet")).not.toBeInTheDocument();
    expect(container.querySelector('[aria-hidden="true"]')).not.toBeNull();
  });

  it("localizes the card", () => {
    renderWithI18n(
      <WorkflowOutcomesCard rows={[]} lessThanMinuteLabel="<1分钟" />,
      { locale: "zh-Hans" },
    );

    expect(screen.getByText("暂无工作流数据")).toBeInTheDocument();
  });
});
