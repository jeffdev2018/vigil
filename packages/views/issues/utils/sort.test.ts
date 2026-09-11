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
});
