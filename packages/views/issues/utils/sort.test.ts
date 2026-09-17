import { describe, expect, it } from "vitest";
import type { Issue } from "@multica/core/types";
import { sortFieldI18nKey, sortIssues } from "./sort";

// Regression: board-view.tsx/list-view.tsx's ternary only remapped
// created_at→created and passed every other SortField straight through —
// updated_at (a real, selectable SORT_OPTIONS entry) built the selector path
// "display.sort_updated_at", a key that doesn't exist (issues.json only has
// display.sort_updated), so the "sorted by…" badge rendered the raw
// untranslated key instead of "Updated date".
describe("sortFieldI18nKey", () => {
  it("drops the _at suffix for created_at and updated_at", () => {
    expect(sortFieldI18nKey("created_at")).toBe("created");
    expect(sortFieldI18nKey("updated_at")).toBe("updated");
  });

  it("passes every other SORT_OPTIONS field through unchanged", () => {
    for (const field of ["position", "status", "priority", "start_date", "due_date", "title"] as const) {
      expect(sortFieldI18nKey(field)).toBe(field);
    }
  });
});

const propertyId = "prop-effort";

function issueWith(id: string, value?: number | string, position = 0): Issue {
  return {
    id,
    position,
    properties: value === undefined ? {} : { [propertyId]: value },
  } as unknown as Issue;
}

function staticIssue(id: string, overrides: Partial<Issue> = {}): Issue {
  return {
    id,
    title: id,
    status: "todo",
    priority: "none",
    position: 0,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  } as Issue;
}

describe("sortIssues property sorts", () => {
  it("sorts number values numerically, missing values last", () => {
    const sorted = sortIssues(
      [issueWith("big", 10), issueWith("none"), issueWith("small", 2)],
      `property:${propertyId}`,
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["small", "big", "none"]);
  });

  it("desc reverses values but keeps missing values last", () => {
    const sorted = sortIssues(
      [issueWith("none"), issueWith("small", 2), issueWith("big", 10)],
      `property:${propertyId}`,
      "desc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["big", "small", "none"]);
  });

  it("sorts date-only strings chronologically via lexical compare", () => {
    const sorted = sortIssues(
      [issueWith("later", "2026-08-01"), issueWith("earlier", "2026-07-13")],
      `property:${propertyId}`,
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["earlier", "later"]);
  });

  it("falls back to position order for the static fields", () => {
    const sorted = sortIssues(
      [issueWith("b", undefined, 2), issueWith("a", undefined, 1)],
      "position",
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["a", "b"]);
  });

  it("keeps manual position ascending even with a stale desc direction", () => {
    const sorted = sortIssues(
      [issueWith("b", undefined, 2), issueWith("a", undefined, 1)],
      "position",
      "desc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["a", "b"]);
  });

  it("sorts updated timestamps newest first", () => {
    const sorted = sortIssues(
      [
        staticIssue("older", { updated_at: "2026-01-01T00:00:00Z" }),
        staticIssue("newer", { updated_at: "2026-02-01T00:00:00Z" }),
      ],
      "updated_at",
      "desc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["newer", "older"]);
  });

  // MUL-7379: ranking on the four lifecycle categories made every status in one
  // category tie, so a list the user sorted by status came back ordered by the
  // created_at tiebreak instead.
  it("keeps the built-ins distinct inside one lifecycle category", () => {
    const sorted = sortIssues(
      [
        staticIssue("blocked", { status: "blocked" }),
        staticIssue("todo", { status: "todo" }),
        staticIssue("in_progress", { status: "in_progress" }),
        staticIssue("backlog", { status: "backlog" }),
        staticIssue("in_review", { status: "in_review" }),
      ],
      "status",
      "asc",
    );
    expect(sorted.map((i) => i.id)).toEqual([
      "backlog",
      "todo",
      "in_progress",
      "in_review",
      "blocked",
    ]);
  });

  it("ranks custom statuses where the catalog puts them", () => {
    const sorted = sortIssues(
      [
        staticIssue("done", { status: "done" }),
        staticIssue("gate", { status: "waiting_on_vendor" }),
        staticIssue("in_progress", { status: "in_progress" }),
      ],
      "status",
      "asc",
      // Catalog order: the gate sits between in_progress and done, which no
      // ordering of the seven built-ins alone could produce.
      ["backlog", "todo", "in_progress", "waiting_on_vendor", "in_review", "blocked", "done", "cancelled"],
    );
    expect(sorted.map((i) => i.id)).toEqual(["in_progress", "gate", "done"]);
  });

  it("falls back to built-in order when the catalog has not loaded", () => {
    const sorted = sortIssues(
      [
        staticIssue("custom", { status: "waiting_on_vendor" }),
        staticIssue("in_review", { status: "in_review" }),
      ],
      "status",
      "asc",
    );
    // Unknown keys rank last rather than being guessed into a category.
    expect(sorted.map((i) => i.id)).toEqual(["in_review", "custom"]);
  });

  it("keeps missing dates last in descending order", () => {
    const sorted = sortIssues(
      [
        staticIssue("missing", { due_date: null }),
        staticIssue("earlier", { due_date: "2026-01-01" }),
        staticIssue("later", { due_date: "2026-02-01" }),
      ],
      "due_date",
      "desc",
    );
    expect(sorted.map((i) => i.id)).toEqual(["later", "earlier", "missing"]);
  });
});
