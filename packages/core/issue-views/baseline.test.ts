// @vitest-environment node
import { describe, expect, it } from "vitest";
import { baselineFromQuery } from "./baseline";
import { createIssueViewStore } from "../issues/stores/view-store";
import { propertyFilterValueKey } from "../types";

// The property-filter branch of baselineFromQuery: saved-view members must
// survive a round-trip as-is when the client can represent them, and drop
// silently when it cannot (hand-edited blob, operator a future client added).
describe("baselineFromQuery property filters", () => {
  const textId = "prop-note";
  const numId = "prop-estimate";

  it("passes strings and known operator objects through untouched", () => {
    const members = [
      "hello",
      "__none__",
      { op: "contains", value: "foo" },
      { op: "gte", value: "3.5" },
    ];
    const baseline = baselineFromQuery({
      propertyFilters: { [textId]: members },
    });

    expect(baseline.raw.propertyFilters[textId]).toEqual(members);
    // Membership keys: strings are their own key; operators get their
    // canonical key so Set lookups agree with the store.
    expect([...baseline.property.get(textId)!]).toEqual(
      members.map((m) => propertyFilterValueKey(m as never)),
    );
  });

  it("drops members the store cannot represent", () => {
    const baseline = baselineFromQuery({
      propertyFilters: {
        [textId]: [
          "keep",
          { op: "regex", value: "x" }, // unknown op
          { op: 42, value: "x" }, // non-string op
          { op: "contains", value: 7 }, // non-string value
          { nope: true }, // not an operator shape
          7, // not a string
          null,
        ],
        [numId]: [{ op: "regex", value: "x" }], // everything dropped
      },
    });

    expect(baseline.raw.propertyFilters[textId]).toEqual(["keep"]);
    // A definition with no representable members is not a filter at all.
    expect(baseline.raw.propertyFilters[numId]).toBeUndefined();
    expect(baseline.property.has(numId)).toBe(false);
  });

  it("treats a non-array member list as no filter", () => {
    const baseline = baselineFromQuery({
      propertyFilters: { [textId]: "hello" },
    });
    expect(baseline.raw.propertyFilters).toEqual({});
    expect(baseline.property.size).toBe(0);
  });
});

// Dated cycles (F29) added `cycleFilters` to the saved-view query. A view
// saved before it must still open — the whole point of a tolerant parse.
describe("baselineFromQuery cycle filters", () => {
  it("reads a cycle filter into both the membership set and the reset snapshot", () => {
    const baseline = baselineFromQuery({ cycleFilters: ["cycle-1", "cycle-2"] });
    expect([...baseline.cycle]).toEqual(["cycle-1", "cycle-2"]);
    expect(baseline.raw.cycleFilters).toEqual(["cycle-1", "cycle-2"]);
  });

  it("keeps a view saved before cycles valid, with no cycle fixed", () => {
    const baseline = baselineFromQuery({ statusFilters: ["todo"], projectFilters: ["p1"] });
    expect(baseline.cycle.size).toBe(0);
    expect(baseline.raw.cycleFilters).toEqual([]);
    // The rest of the view is untouched: an added dimension must not cost the
    // dimensions the view already had.
    expect(baseline.raw.statusFilters).toEqual(["todo"]);
    expect([...baseline.project]).toEqual(["p1"]);
  });

  it("drops a non-string member a hand-edited query smuggled in", () => {
    const baseline = baselineFromQuery({ cycleFilters: ["cycle-1", 7, null] });
    expect(baseline.raw.cycleFilters).toEqual(["cycle-1"]);
  });
});

// Work item types (F30) added `typeFilters` the same way cycles added theirs.
describe("baselineFromQuery type filters", () => {
  it("reads a type filter into both the membership set and the reset snapshot", () => {
    const baseline = baselineFromQuery({ typeFilters: ["bug", "story"] });
    expect([...baseline.type]).toEqual(["bug", "story"]);
    expect(baseline.raw.typeFilters).toEqual(["bug", "story"]);
  });

  // Acceptance 13 (second half): a view saved before F30 must still open.
  it("keeps a view saved before work item types valid, with no type fixed", () => {
    const baseline = baselineFromQuery({ statusFilters: ["todo"], cycleFilters: ["c1"] });
    expect(baseline.type.size).toBe(0);
    expect(baseline.raw.typeFilters).toEqual([]);
    expect(baseline.raw.statusFilters).toEqual(["todo"]);
    expect(baseline.raw.cycleFilters).toEqual(["c1"]);
  });

  // A type key is WORKSPACE-defined, so there is no constant to validate it
  // against. Filtering here against a fixed list is exactly what deleted every
  // custom status filter on reopen before MUL-6243 — only unrepresentable
  // members (non-strings, empty strings) are dropped.
  it("keeps a custom type key it has never heard of", () => {
    const baseline = baselineFromQuery({ typeFilters: ["spike", 7, "", null] });
    expect(baseline.raw.typeFilters).toEqual(["spike"]);
  });
});

// Goals (JEF-395) were the one dimension the baseline never read: opening a
// saved view left the goal filter untouched, and the active-filter count
// treated a view-fixed goal as a user addition.
describe("baselineFromQuery goal filters", () => {
  it("fixes the view's goals and a reset returns to them", () => {
    const baseline = baselineFromQuery({ goalFilters: ["goal-1", "goal-2"] });
    expect([...baseline.goal]).toEqual(["goal-1", "goal-2"]);

    const store = createIssueViewStore("test:baseline-goal");
    store.getState().toggleGoalFilter("goal-9");
    store.getState().resetFiltersTo(baseline.raw);
    expect(store.getState().goalFilters).toEqual(["goal-1", "goal-2"]);
  });

  it("keeps a view saved without goals valid, with no goal fixed", () => {
    const baseline = baselineFromQuery({ cycleFilters: ["c1"] });
    expect(baseline.goal.size).toBe(0);
    expect(baseline.raw.goalFilters).toEqual([]);
    expect(baseline.raw.cycleFilters).toEqual(["c1"]);
  });

  it("drops a non-string member a hand-edited query smuggled in", () => {
    const baseline = baselineFromQuery({ goalFilters: ["goal-1", 7, null] });
    expect(baseline.raw.goalFilters).toEqual(["goal-1"]);
  });
});
