// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { RunHaltSchema, EMPTY_RUN_HALT } from "./schemas";

// GET/PUT /api/run-halt return this shape directly (no envelope) — the
// envelope-wrapped case is proven in packages/core/approvals/schemas.test.ts.

describe("RunHaltSchema", () => {
  it("parses a set halt", () => {
    const parsed = RunHaltSchema.parse({ halted: true, reason: "investigating a runaway loop", halted_by: "u1", halted_at: "2026-09-09T10:00:00Z", frozen_count: 3, resumed_count: 0 });
    expect(parsed).toEqual({ halted: true, reason: "investigating a runaway loop", halted_by: "u1", halted_at: "2026-09-09T10:00:00Z", frozen_count: 3, resumed_count: 0 });
  });

  it("falls back to not-halted on a malformed response", () => {
    expect(parseWithFallback("nope", RunHaltSchema, EMPTY_RUN_HALT, { endpoint: "test" })).toEqual(EMPTY_RUN_HALT);
    expect(parseWithFallback(null, RunHaltSchema, EMPTY_RUN_HALT, { endpoint: "test" })).toEqual(EMPTY_RUN_HALT);
  });

  it("tolerates a newer server's extra fields and missing optional ones", () => {
    const parsed = RunHaltSchema.parse({ halted: false, extra_future_field: 1 });
    expect(parsed.halted).toBe(false);
    expect(parsed.reason).toBe("");
    // A legacy backend without the freeze fields (pre-JEF-257) parses to 0.
    expect(parsed.frozen_count).toBe(0);
    expect(parsed.resumed_count).toBe(0);
  });

  it("degrades malformed freeze counts to 0", () => {
    const parsed = RunHaltSchema.parse({ halted: true, frozen_count: "many", resumed_count: null });
    expect(parsed.frozen_count).toBe(0);
    expect(parsed.resumed_count).toBe(0);
  });
});
