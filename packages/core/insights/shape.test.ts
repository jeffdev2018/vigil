// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  deriveInsightShape,
  insightLabelKey,
  insightQueryHash,
  insightRowValue,
  resolveInsightShape,
} from "./shape";
import type { InsightQuery } from "./schemas";

const q = (over: Partial<InsightQuery>): InsightQuery =>
  ({ entity: "issue", metric: "count", group_by: [], filters: [], ...over }) as InsightQuery;

describe("deriveInsightShape", () => {
  it("reads an ungrouped aggregate as a single figure", () => {
    expect(deriveInsightShape(q({}))).toBe("number");
  });

  it("reads a time grouping as a series", () => {
    expect(deriveInsightShape(q({ group_by: ["time"] }))).toBe("line");
  });

  it("reads a counted category as parts of a whole", () => {
    expect(deriveInsightShape(q({ group_by: ["status"] }))).toBe("donut");
  });

  it("reads a non-count over a category as bars", () => {
    expect(deriveInsightShape(q({ metric: "avg_age_days", group_by: ["status"] }))).toBe("bar");
  });

  it("reads two dimensions as bars", () => {
    expect(deriveInsightShape(q({ group_by: ["status", "priority"] }))).toBe("bar");
  });

  it("survives a missing document", () => {
    expect(deriveInsightShape(null)).toBe("number");
    expect(deriveInsightShape(undefined)).toBe("number");
  });
});

describe("resolveInsightShape", () => {
  it("trusts a shape it knows", () => {
    expect(resolveInsightShape("line", q({ group_by: ["status"] }))).toBe("line");
  });

  it("falls back to the local derivation for a shape a newer backend invented", () => {
    // The card must still render: an unknown shape is a contract the client
    // has not learned yet, not a broken response.
    expect(resolveInsightShape("sankey", q({ group_by: ["time"] }))).toBe("line");
  });

  it("falls back when the server said nothing", () => {
    expect(resolveInsightShape(null, q({}))).toBe("number");
    expect(resolveInsightShape("", q({ group_by: ["status"] }))).toBe("donut");
  });
});

describe("insightLabelKey", () => {
  it("names the column carrying the row label", () => {
    expect(insightLabelKey(q({ group_by: ["status"] }))).toBe("status");
  });

  it("is null for an ungrouped aggregate", () => {
    expect(insightLabelKey(q({}))).toBeNull();
  });
});

describe("insightRowValue", () => {
  it("returns the number", () => {
    expect(insightRowValue({ value: 12 })).toBe(12);
  });

  it("returns null when the aggregate produced none", () => {
    expect(insightRowValue({ value: null })).toBeNull();
    expect(insightRowValue({ value: "12" })).toBeNull();
    expect(insightRowValue(undefined)).toBeNull();
  });
});

describe("insightQueryHash", () => {
  it("is stable across key order, so two identical documents share one cache entry", () => {
    const a = { entity: "issue", metric: "count", group_by: ["status"] } as InsightQuery;
    const b = { group_by: ["status"], metric: "count", entity: "issue" } as InsightQuery;
    expect(insightQueryHash(a)).toBe(insightQueryHash(b));
  });

  it("separates documents that differ", () => {
    expect(insightQueryHash(q({ group_by: ["status"] }))).not.toBe(
      insightQueryHash(q({ group_by: ["priority"] })),
    );
  });

  it("keeps array order, which is the grouping order", () => {
    expect(insightQueryHash(q({ group_by: ["status", "priority"] }))).not.toBe(
      insightQueryHash(q({ group_by: ["priority", "status"] })),
    );
  });

  it("is empty for a missing document, which disables the run query", () => {
    expect(insightQueryHash(null)).toBe("");
  });
});
