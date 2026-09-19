// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  RunsResponseSchema,
  EMPTY_RUNS_RESPONSE,
  RunSchema,
  CancelRunsResponseSchema,
  isRunSilent,
  runCostUsd,
  runCostKnown,
  blockerHash,
  RUN_SILENCE_THRESHOLD_MS,
} from "./fleet-schemas";

describe("runs fleet response parsing", () => {
  it("falls back to the empty envelope when the whole payload is not an object", () => {
    const parsed = parseWithFallback("nope", RunsResponseSchema, EMPTY_RUNS_RESPONSE, {
      endpoint: "GET /api/runs",
    });
    expect(parsed).toEqual(EMPTY_RUNS_RESPONSE);
  });

  it("degrades a malformed run row without losing the rest of the page", () => {
    const parsed = RunsResponseSchema.parse({
      runs: [
        { id: "task-1", agent_name: 42, issue: "not-an-object", cost_usd_ticks: "oops", blocked_on: { kind: "gate" } },
      ],
      summary: { active: "3" },
    });
    const run = parsed.runs[0]!;
    expect(run.id).toBe("task-1");
    // Field-level .catch() keeps the row instead of dropping it.
    expect(run.agent_name).toBe("");
    expect(run.issue).toBeNull();
    expect(run.cost_usd_ticks).toBe(0);
    expect(run.blocked_on?.kind).toBe("gate");
    // A malformed summary field must not cost the page its run rows.
    expect(parsed.summary.active).toBe(0);
  });

  it("keeps an unrecognized status verbatim so a newer server's state still shows", () => {
    const run = RunSchema.parse({ id: "t1", status: "hibernating" });
    expect(run.status).toBe("hibernating");
  });

  it("degrades an unrecognized cancel outcome to its own row rather than dropping it", () => {
    const parsed = CancelRunsResponseSchema.parse({
      results: [{ task_id: "t1", outcome: "reclaimed_by_a_newer_server" }],
      cancelled: 0,
    });
    expect(parsed.results[0]?.outcome).toBe("reclaimed_by_a_newer_server");
  });
});

describe("runCostKnown", () => {
  // A zero is only a figure when the server says so: a run with no priceable
  // usage must read "unknown", never "$0.00" (audit UX, sept. 2026).
  it("trusts cost_known, and on an older backend only a positive amount", () => {
    const parse = (row: Record<string, unknown>) => RunSchema.parse({ id: "t", ...row });
    expect(runCostKnown(parse({ cost_usd_ticks: 0, cost_known: true }))).toBe(true);
    expect(runCostKnown(parse({ cost_usd_ticks: 5, cost_known: false }))).toBe(false);
    expect(runCostKnown(parse({ cost_usd_ticks: 0 }))).toBe(false);
    expect(runCostKnown(parse({ cost_usd_ticks: 5 }))).toBe(true);
    expect(runCostKnown(parse({ cost_usd_ticks: 0, cost_known: "yes" }))).toBe(false);
    expect(RunsResponseSchema.parse({ summary: { cost_unknown_since: "x" } }).summary.cost_unknown_since).toBe(0);
  });
});

describe("isRunSilent", () => {
  it("is true only for a running run past the threshold", () => {
    expect(isRunSilent({ status: "running", silence_ms: RUN_SILENCE_THRESHOLD_MS + 1 })).toBe(true);
    expect(isRunSilent({ status: "running", silence_ms: RUN_SILENCE_THRESHOLD_MS })).toBe(false);
    expect(isRunSilent({ status: "queued", silence_ms: RUN_SILENCE_THRESHOLD_MS + 1 })).toBe(false);
  });
});

describe("runCostUsd", () => {
  it("converts ticks (1e10 per USD) to a plain dollar figure", () => {
    expect(runCostUsd(25_000_000_000)).toBe(2.5);
    expect(runCostUsd(0)).toBe(0);
  });
});

describe("blockerHash", () => {
  it("links to the issue timeline's approval anchor when a decision is present", () => {
    expect(blockerHash({ kind: "gate", decision_id: "d1", summary: "", since: null })).toBe("#approval-d1");
  });

  it("is undefined for a blocker with no decision to anchor to", () => {
    expect(blockerHash({ kind: "paused", summary: "paused", since: null })).toBeUndefined();
    expect(blockerHash(null)).toBeUndefined();
  });
});
