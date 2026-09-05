// @vitest-environment jsdom
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { WorkProfile, WorkProfileObservation } from "@multica/core/work-profile";
import { renderWithI18n } from "../../test/i18n";

// Client parsing and pure helpers: packages/core/work-profile/queries.test.ts.

const state = vi.hoisted(() => ({
  profile: null as WorkProfile | null,
  setAuto: vi.fn(),
  forget: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/work-profile", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/work-profile")>()),
  workProfileOptions: () => ({ queryKey: ["wp"], queryFn: async () => state.profile }),
  useSetObservationAuto: () => ({ mutate: state.setAuto, isPending: false }),
  useForgetObservation: () => ({ mutate: state.forget, isPending: false }),
}));

import { LearningTab } from "./learning-tab";

const obs = (over: Partial<WorkProfileObservation> = {}): WorkProfileObservation => ({
  id: "o1", key: "decision:question:no,yes", kind: "decision_rule", value: { option_id: "yes", option_label: "Yes, go", count: 9, total: 10, family: "question" },
  source: "decisions", count: 10, corrections: 1, auto: false, state: "learned", stake: "normal", first_observed_at: "2026-09-01T00:00:00Z", last_observed_at: "2026-09-05T00:00:00Z", ...over,
});
const profile = (over: Partial<WorkProfile> = {}): WorkProfile => ({
  observations: [obs(), obs({ id: "o2", key: "decision:gate:no,yes", stake: "high", state: "proposed", value: { option_id: "yes", option_label: "Approve", count: 6, total: 6, family: "gate" } }), obs({ id: "o3", key: "decision_hour", kind: "decision_hour", value: { "09": 4, "14": 7 }, count: 11 })],
  examples: 17, auto_decided: 2, overturned: 1, review_load_seconds: 675, adaptation_surface: ["decision_rules", "decision_hours"], ...over,
});

function render() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <LearningTab />
    </QueryClientProvider>,
  );
}

describe("LearningTab", () => {
  beforeEach(() => {
    state.profile = profile();
    state.setAuto.mockReset();
    state.forget.mockReset();
  });

  it("shows the counts, the review load, the rules with their state and the decision hours", async () => {
    render();
    await screen.findAllByTestId("learning-rule");
    expect(screen.getByTestId("learning-examples").textContent).toBe("17");
    expect(screen.getByText("11 min")).toBeTruthy();
    const rules = screen.getAllByTestId("learning-rule");
    expect(rules).toHaveLength(2);
    expect(rules[0]?.getAttribute("data-state")).toBe("learned");
    expect(rules[1]?.getAttribute("data-state")).toBe("proposed");
    expect(screen.getByText("high stakes, never automated")).toBeTruthy();
    expect(screen.getByText("Mostly around 14h, 9h")).toBeTruthy();
    expect(screen.getByText("decision rules · decision hours")).toBeTruthy();
  });

  it("lets me switch a normal rule on, keeps a high-stakes one locked, and forgets", async () => {
    render();
    await screen.findAllByTestId("learning-rule");
    const switches = screen.getAllByRole("switch", { name: "Decide alone" });
    expect(switches).toHaveLength(2);
    expect(switches[1]?.hasAttribute("disabled") || switches[1]?.getAttribute("aria-disabled") === "true" || switches[1]?.getAttribute("data-disabled") !== null).toBe(true);
    fireEvent.click(switches[0]!);
    expect(state.setAuto).toHaveBeenCalledWith({ id: "o1", auto: true }, expect.anything());
    fireEvent.click(screen.getAllByText("Forget")[0]!);
    expect(state.forget).toHaveBeenCalledWith("o1", expect.anything());
  });

  it("says when nothing was learned yet", async () => {
    state.profile = profile({ observations: [], examples: 0 });
    render();
    expect(await screen.findByTestId("learning-empty")).toBeTruthy();
  });
});
