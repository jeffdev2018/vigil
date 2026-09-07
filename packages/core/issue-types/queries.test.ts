// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildIssueTypeCatalog, compareIssueTypeEntries } from "./queries";
import {
  IssueTypeEntrySchema,
  ListIssueTypesResponseSchema,
  EMPTY_LIST_ISSUE_TYPES_RESPONSE,
} from "../api/schemas";
import { parseWithFallback } from "../api/schema";
import type { IssueTypeEntry } from "../types";

function entry(key: string, over: Partial<IssueTypeEntry> = {}): IssueTypeEntry {
  return {
    id: key,
    workspace_id: "ws-1",
    key,
    name: key,
    description: "",
    color: "#123456",
    icon: "",
    is_system: false,
    position: 0,
    archived_at: null,
    created_at: "",
    updated_at: "",
    ...over,
  };
}

describe("buildIssueTypeCatalog", () => {
  it("reports not-loaded and renders nothing rather than inventing types", () => {
    // Unlike a status category, a type key is not a constant of the product —
    // a workspace can rename all four seeded ones — so an unloaded catalogue
    // must resolve to the raw key, never to a guessed label.
    const c = buildIssueTypeCatalog(undefined);
    expect(c.isLoaded).toBe(false);
    expect(c.types).toEqual([]);
    expect(c.labelOf("bug")).toBe("bug");
    expect(c.colorOf("bug")).toBeNull();
  });

  it("resolves label and colour from the catalogue", () => {
    const c = buildIssueTypeCatalog([entry("bug", { name: "Bug", color: "#ef4444" })]);
    expect(c.labelOf("bug")).toBe("Bug");
    expect(c.colorOf("bug")).toBe("#ef4444");
    expect(c.isLoaded).toBe(true);
  });

  // Acceptance 14: an issue carrying a type this client has never heard of —
  // one created moments ago in another session — must not blank the picker or
  // the badge. It falls back to the raw key.
  it("falls back to the raw key for an unknown type", () => {
    const c = buildIssueTypeCatalog([entry("bug")]);
    expect(c.labelOf("invented_elsewhere")).toBe("invented_elsewhere");
    expect(c.colorOf("invented_elsewhere")).toBeNull();
    expect(c.entryOf("invented_elsewhere")).toBeUndefined();
    // The list it does know is untouched.
    expect(c.types.map((t) => t.key)).toEqual(["bug"]);
  });

  it("answers empty for an untyped issue without a special case at the call site", () => {
    const c = buildIssueTypeCatalog([entry("bug")]);
    expect(c.labelOf(null)).toBe("");
    expect(c.labelOf(undefined)).toBe("");
    expect(c.colorOf(null)).toBeNull();
  });

  it("keeps archived entries resolvable but out of activeTypes", () => {
    // An issue left on an archived type must still resolve its real name and
    // colour; only assignment is retired.
    const c = buildIssueTypeCatalog([
      entry("bug", { name: "Bug" }),
      entry("retired", { name: "Retired", archived_at: "2026-01-01T00:00:00Z" }),
    ]);
    expect(c.labelOf("retired")).toBe("Retired");
    expect(c.activeTypes.map((t) => t.key)).toEqual(["bug"]);
    expect(c.types).toHaveLength(2);
  });

  it("treats a failed refetch over a cached catalogue as non-blocking", () => {
    const withData = buildIssueTypeCatalog([entry("bug")], { isError: true });
    expect(withData.isError).toBe(false);
    const withNothing = buildIssueTypeCatalog(undefined, { isError: true });
    expect(withNothing.isError).toBe(true);
  });
});

describe("compareIssueTypeEntries", () => {
  it("orders by position, then key", () => {
    const sorted = [
      entry("z", { position: 1 }),
      entry("a", { position: 0 }),
      entry("b", { position: 0 }),
    ].sort(compareIssueTypeEntries);
    expect(sorted.map((t) => t.key)).toEqual(["a", "b", "z"]);
  });
});

describe("issue type response parsing", () => {
  it("degrades a malformed response to the empty catalogue instead of throwing", () => {
    const parsed = parseWithFallback(
      { types: "not an array" },
      ListIssueTypesResponseSchema,
      EMPTY_LIST_ISSUE_TYPES_RESPONSE,
      { endpoint: "GET /api/issue-types" },
    );
    expect(parsed).toEqual(EMPTY_LIST_ISSUE_TYPES_RESPONSE);
  });

  it("fills in fields an older backend omits", () => {
    const parsed = IssueTypeEntrySchema.parse({
      id: "t1",
      workspace_id: "ws-1",
      key: "bug",
      name: "Bug",
      created_at: "",
      updated_at: "",
    });
    expect(parsed.icon).toBe("");
    expect(parsed.is_system).toBe(false);
    expect(parsed.archived_at).toBeNull();
  });

  it("keeps a type carrying an unrecognized extra field", () => {
    // .loose(): a newer server adding a column must not cost this client the
    // whole catalogue.
    const parsed = ListIssueTypesResponseSchema.parse({
      types: [
        { id: "t1", workspace_id: "ws", key: "bug", name: "Bug", created_at: "", updated_at: "", hierarchy: "parent" },
      ],
      total: 1,
    });
    expect(parsed.types.map((t) => t.key)).toEqual(["bug"]);
  });
});
