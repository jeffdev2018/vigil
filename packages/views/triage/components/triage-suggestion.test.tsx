// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import type { TriageItem, TriageSuggestion } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// Parsing and query keys: packages/core/triage/suggestions.test.ts.

const state = vi.hoisted(() => ({ reopened: [] as string[] }));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("@multica/core/triage/mutations", () => ({ useReopenTriageItem: () => ({ mutate: (id: string) => state.reopened.push(id), isPending: false }) }));

import { TriageSuggestionChip, TriageSuggestionPanel } from "./triage-suggestion";

const suggestion = (over: Partial<TriageSuggestion> = {}): TriageSuggestion => ({
  item_id: "i1", ready: true, examples: 30, min_examples: 20, suggested: "dismiss", confidence: 0.92,
  neighbors: [{ id: "n1", title: "chore(deps): bump lodash", state: "dismissed", score: 0.4 }], ...over,
});
const item = (state: string) => ({ id: "i1", title: "bump axios", state, payload: {} } as unknown as TriageItem);

describe("triage suggestion", () => {
  it("shows the chip with its confidence and nothing without a suggestion", () => {
    renderWithI18n(<TriageSuggestionChip suggestion={suggestion()} />);
    expect(screen.getByTestId("triage-suggestion-chip").textContent).toContain("92%");
    const { container } = renderWithI18n(<TriageSuggestionChip suggestion={suggestion({ suggested: undefined })} />);
    expect(container.querySelector("[data-testid=triage-suggestion-chip]")).toBeNull();
  });

  it("explains the suggestion, its neighbours and whether auto-apply is on", () => {
    renderWithI18n(<TriageSuggestionPanel item={item("pending")} suggestion={suggestion()} auto={{ enabled: true, threshold: 0.9, min_examples: 20 }} wsId="w" />);
    const panel = screen.getByTestId("triage-suggestion-panel");
    expect(panel.textContent).toContain("92%");
    expect(panel.textContent).toContain("90%");
    expect(screen.getAllByTestId("triage-neighbor")).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Reopen" })).toBeNull();
  });

  it("says when there are not enough examples and reopens a dismissed item", () => {
    renderWithI18n(<TriageSuggestionPanel item={item("dismissed")} suggestion={suggestion({ ready: false, examples: 12 })} auto={{ enabled: false, threshold: 0.9, min_examples: 20 }} wsId="w" />);
    expect(screen.getByTestId("triage-suggestion-panel").textContent).toContain("12");
    fireEvent.click(screen.getByRole("button", { name: "Reopen" }));
    expect(state.reopened).toEqual(["i1"]);
  });
});
