// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  EMPTY_REVIEW_FLAG_LIST,
  ReviewFlagListSchema,
  ReviewFlagSchema,
  flagLocation,
  isSettled,
  isStale,
  middleTruncatePath,
  normalizeSeverity,
  normalizeState,
  totalOpen,
  type ReviewFlag,
} from "./schemas";
import { parseWithFallback } from "../api/schema";

// Canonical layer for the review flag contract (F06). The component suite
// (packages/views/issues/components/review-flags-section.test.tsx) keeps the
// happy path and the wiring; the parsing matrix lives here.

const flag = (over: Partial<ReviewFlag> = {}): ReviewFlag => ({
  id: "f1",
  issue_id: "i1",
  pr_source: "github",
  pr_id: "pr1",
  head_sha: "abc123",
  file_path: "src/checkout/total.py",
  line_start: 42,
  line_end: 48,
  side: "new",
  severity: "bug",
  confidence: 80,
  title: "the retry path swallows the error",
  body: "",
  author_agent_id: "a1",
  author_user_id: "",
  task_id: "t1",
  state: "open",
  resolved_by_type: "",
  resolved_by_id: "",
  resolved_at: "",
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  ...over,
});

describe("ReviewFlagSchema", () => {
  it("keeps a well-formed flag intact", () => {
    const parsed = ReviewFlagSchema.parse(flag());
    expect(parsed.severity).toBe("bug");
    expect(parsed.confidence).toBe(80);
    expect(parsed.line_end).toBe(48);
  });

  it("keeps an unknown field a newer server sends", () => {
    const parsed = ReviewFlagSchema.parse({ ...flag(), suppressed_by: "rule-7" }) as Record<string, unknown>;
    expect(parsed.suppressed_by).toBe("rule-7");
  });

  it("survives every field arriving as the wrong type", () => {
    const parsed = ReviewFlagSchema.parse({
      id: 7,
      severity: { nope: true },
      confidence: "high",
      line_start: "42",
      state: null,
      title: [],
    });
    expect(parsed.id).toBe("");
    // The catch value, not a throw: an unparseable severity still renders.
    expect(normalizeSeverity(String(parsed.severity))).toBe("info");
    expect(parsed.confidence).toBeNull();
    expect(parsed.line_start).toBe(0);
  });

  it("reads an absent confidence as null, never as zero", () => {
    const { confidence, ...withoutConfidence } = flag();
    void confidence;
    expect(ReviewFlagSchema.parse(withoutConfidence).confidence).toBeNull();
    // 0 is a real answer and must survive as one.
    expect(ReviewFlagSchema.parse(flag({ confidence: 0 })).confidence).toBe(0);
  });
});

describe("ReviewFlagListSchema", () => {
  it("falls back to an empty list and zero counts on a malformed response", () => {
    for (const malformed of [null, "nope", 42, [], { flags: "not-an-array", counts: "nope" }]) {
      const parsed = parseWithFallback(malformed, ReviewFlagListSchema, EMPTY_REVIEW_FLAG_LIST, {
        endpoint: "GET /api/issues/:id/review-flags",
      });
      expect(parsed.flags).toEqual([]);
      expect(parsed.counts).toEqual({ bug: 0, warning: 0, info: 0 });
    }
  });

  it("keeps the flags a partially malformed response did carry", () => {
    const parsed = parseWithFallback({ flags: [flag()], counts: { bug: 1 } }, ReviewFlagListSchema,
      EMPTY_REVIEW_FLAG_LIST, { endpoint: "GET /api/issues/:id/review-flags" });
    expect(parsed.flags).toHaveLength(1);
    expect(parsed.counts).toEqual({ bug: 1, warning: 0, info: 0 });
  });

  it("does not reorder what the server sent", () => {
    // The sort is the server's. A client that re-sorted would be a second,
    // quietly diverging opinion about what a reviewer reads first.
    const parsed = ReviewFlagListSchema.parse({
      flags: [flag({ id: "a", severity: "info" }), flag({ id: "b", severity: "bug" })],
      counts: { bug: 1, warning: 0, info: 1 },
    });
    expect(parsed.flags.map((f) => f.id)).toEqual(["a", "b"]);
  });
});

describe("normalizeSeverity", () => {
  it.each([
    ["bug", "bug"],
    ["WARNING", "warning"],
    ["  info  ", "info"],
    ["critical", "info"],
    ["", "info"],
  ])("reads %s as %s", (input, want) => {
    expect(normalizeSeverity(input)).toBe(want);
  });
});

describe("normalizeState", () => {
  it.each([
    ["open", "open"],
    ["RESOLVED", "resolved"],
    ["dismissed", "dismissed"],
    ["stale", "stale"],
    // An unclassifiable finding is still a finding; hiding it is the one
    // failure mode a review tool must not have.
    ["quarantined", "open"],
    ["", "open"],
  ])("reads %s as %s", (input, want) => {
    expect(normalizeState(input)).toBe(want);
  });
});

describe("flag predicates", () => {
  it("recognises stale and settled states", () => {
    expect(isStale(flag({ state: "stale" }))).toBe(true);
    expect(isStale(flag({ state: "open" }))).toBe(false);
    expect(isSettled(flag({ state: "resolved" }))).toBe(true);
    expect(isSettled(flag({ state: "dismissed" }))).toBe(true);
    // Stale is not settled: nobody answered it, the code moved underneath it.
    expect(isSettled(flag({ state: "stale" }))).toBe(false);
    expect(isSettled(flag({ state: "open" }))).toBe(false);
  });
});

describe("flagLocation", () => {
  it("writes a single line without a range", () => {
    expect(flagLocation(flag({ line_start: 42, line_end: 42 }))).toBe("src/checkout/total.py:42");
  });
  it("writes a range when the flag spans one", () => {
    expect(flagLocation(flag())).toBe("src/checkout/total.py:42-48");
  });
  it("drops the line when there is none", () => {
    expect(flagLocation(flag({ line_start: 0, line_end: 0 }))).toBe("src/checkout/total.py");
  });
});

describe("middleTruncatePath", () => {
  it("leaves a short path alone", () => {
    expect(middleTruncatePath("api/client.go")).toBe("api/client.go");
  });
  it("keeps both ends of a long path", () => {
    const long = "packages/views/issues/components/review-flags-section.tsx";
    const out = middleTruncatePath(long, 30);
    expect(out).toHaveLength(30);
    expect(out.startsWith("packages/views")).toBe(true);
    expect(out.endsWith("section.tsx")).toBe(true);
  });
});

describe("totalOpen", () => {
  it("sums the three severities", () => {
    expect(totalOpen({ bug: 2, warning: 1, info: 3 })).toBe(6);
    expect(totalOpen(EMPTY_REVIEW_FLAG_LIST.counts)).toBe(0);
  });
});
