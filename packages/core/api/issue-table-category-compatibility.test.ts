// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import {
  EMPTY_ISSUE_TABLE_GROUPS_RESPONSE,
  IssueTableGroupsResponseSchema,
} from "./schemas";

describe("table category wire compatibility", () => {
  it.each(["in_review", "in_progress", "cancelled", "started", "closed", "custom_qa"])(
    "preserves %s without changing the corresponding group key",
    (status) => {
      const key = `status_category:${status}`;
      const parsed = IssueTableGroupsResponseSchema.parse({
        query_fingerprint: "query",
        total: 2,
        groups: [{ key, count: 2, value: { kind: "status", status } }],
      });
      expect(parsed.groups[0]).toEqual({ key, count: 2, value: { kind: "status", status } });
    },
  );

  // The fork's unknown-kind branch: a dimension this build does not model
  // keeps its key and count instead of failing the row (and with it the whole
  // grouped board). Gated on the kind being genuinely unknown, which is what
  // the malformed-value case below proves.
  it("keeps a group whose kind this build does not know", () => {
    const parsed = IssueTableGroupsResponseSchema.parse({
      query_fingerprint: "query",
      total: 2,
      groups: [{ key: "cycle:c1", count: 2, value: { kind: "cycle", cycle_id: "c1" } }],
    });
    expect(parsed.groups[0]).toEqual({ key: "cycle:c1", count: 2, value: { kind: "unknown" } });
  });

  it("falls back on malformed group values before UI filtering", () => {
    const parsed = parseWithFallback({
      query_fingerprint: "query", total: 2,
      groups: [{ key: "status_category:started", count: 2, value: { kind: "status", status: null } }],
    }, IssueTableGroupsResponseSchema, EMPTY_ISSUE_TABLE_GROUPS_RESPONSE, {
      endpoint: "POST /api/issues/table/groups",
    });
    expect(parsed).toEqual(EMPTY_ISSUE_TABLE_GROUPS_RESPONSE);
  });
});
