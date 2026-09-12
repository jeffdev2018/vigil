import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  BudgetOverrideSchema,
  BudgetPolicyListSchema,
  BudgetPolicySchema,
  BudgetStatusListSchema,
  EMPTY_BUDGET_OVERRIDE,
  EMPTY_BUDGET_POLICY,
} from "./schemas";

describe("budget API schemas", () => {
  it("falls back instead of leaking malformed policy data into settings", () => {
    expect(parseWithFallback([{ id: 7 }], BudgetPolicyListSchema, [], { endpoint: "test" })).toEqual([]);
  });

  it("rejects negative usage totals", () => {
    expect(parseWithFallback([{ spent_usd_ticks: -1 }], BudgetStatusListSchema, [], { endpoint: "test" })).toEqual([]);
  });

  it("degrades a single malformed field instead of discarding the whole policy", () => {
    // revision is garbage, everything else is valid: the object as a whole
    // must not be thrown away, only the bad field replaced.
    const parsed = parseWithFallback(
      { id: "p1", workspace_id: "w1", scope_type: "workspace", scope_id: null, limit_usd_ticks: 100, period: "daily", warn_bps: 500, action: "observe", revision: "nope", created_at: "t", updated_at: "t" },
      BudgetPolicySchema,
      EMPTY_BUDGET_POLICY,
      { endpoint: "test" },
    );
    expect(parsed.id).toBe("p1");
    expect(parsed.revision).toBe(1);
  });

  it("uses a real typed default, not a bare unsafe cast, when a mutation response is unparseable", () => {
    expect(parseWithFallback("garbage", BudgetPolicySchema, EMPTY_BUDGET_POLICY, { endpoint: "test" })).toEqual(EMPTY_BUDGET_POLICY);
    expect(parseWithFallback("garbage", BudgetOverrideSchema, EMPTY_BUDGET_OVERRIDE, { endpoint: "test" })).toEqual(EMPTY_BUDGET_OVERRIDE);
  });
});
