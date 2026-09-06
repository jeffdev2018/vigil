// @vitest-environment jsdom

import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import type { RunPlan as RunPlanData } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { RunPlan, runPlanProgress } from "./run-plan";

function plan(items: [string, string][], seq = 1_000_001): RunPlanData {
  return { seq, items: items.map(([text, status]) => ({ text, status })) };
}

afterEach(cleanup);

describe("RunPlan", () => {
  it("marks each status with its own bullet and names it for a screen reader", () => {
    renderWithI18n(
      <RunPlan
        plan={plan([
          ["Read the failing test", "done"],
          ["Fix the parser", "in_progress"],
          ["Update the docs", "pending"],
          // Only a newer server can send this. It must render as a neutral
          // bullet carrying its own name, never be dropped or guessed into
          // one of the three this build knows.
          ["Ship", "blocked"],
        ])}
      />,
    );

    const items = screen.getAllByRole("listitem");
    expect(items).toHaveLength(4);
    expect(items[0]).toHaveTextContent("●");
    expect(items[0]).toHaveTextContent("Done");
    // in_progress renders the live spinner, so it carries neither the filled
    // nor the hollow bullet.
    expect(items[1]?.textContent).not.toContain("●");
    expect(items[1]?.textContent).not.toContain("○");
    expect(items[1]).toHaveTextContent("In progress");
    expect(items[2]).toHaveTextContent("○");
    expect(items[3]).toHaveTextContent("·");
    expect(items[3]).toHaveTextContent("blocked");
  });

  it("labels the list with its progress", () => {
    renderWithI18n(
      <RunPlan plan={plan([["a", "done"], ["b", "done"], ["c", "pending"]])} />,
    );
    expect(
      screen.getByRole("list", { name: "Run plan: 2 of 3 steps done" }),
    ).toBeInTheDocument();
  });

  it("keeps long text reachable through a title rather than truncating it away", () => {
    const long = "x".repeat(180);
    renderWithI18n(<RunPlan plan={plan([[long, "pending"]])} />);
    expect(screen.getByTitle(long)).toBeInTheDocument();
  });

  it("windows a long plan around the active item until asked for all of it", () => {
    renderWithI18n(
      <RunPlan
        plan={plan([
          ["one", "done"],
          ["two", "done"],
          ["three", "done"],
          ["four", "in_progress"],
          ["five", "pending"],
          ["six", "pending"],
          ["seven", "pending"],
        ])}
      />,
    );

    // The active item plus its neighbours — not the three settled steps above
    // it, and not the tail nobody has reached.
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
    expect(screen.getByText("three")).toBeInTheDocument();
    expect(screen.getByText("four")).toBeInTheDocument();
    expect(screen.getByText("five")).toBeInTheDocument();
    expect(screen.queryByText("one")).not.toBeInTheDocument();
    expect(screen.queryByText("seven")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show all 7 steps" }));
    expect(screen.getAllByRole("listitem")).toHaveLength(7);
    expect(screen.getByText("one")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show fewer" }));
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
  });

  it("shows a short plan whole, with no expand affordance", () => {
    renderWithI18n(
      <RunPlan plan={plan([["a", "done"], ["b", "in_progress"], ["c", "pending"]])} />,
    );
    expect(screen.getAllByRole("listitem")).toHaveLength(3);
    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });

  it("renders nothing for an empty checklist", () => {
    const { container } = renderWithI18n(<RunPlan plan={plan([])} />);
    expect(container).toBeEmptyDOMElement();
  });
});

describe("runPlanProgress", () => {
  it("counts done against the whole checklist", () => {
    expect(runPlanProgress(plan([["a", "done"], ["b", "in_progress"], ["c", "pending"]])))
      .toEqual({ done: 1, total: 3 });
    expect(runPlanProgress(plan([]))).toEqual({ done: 0, total: 0 });
  });
});
