import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import { BudgetPolicyListSchema, BudgetStatusListSchema } from "./schemas";

describe("budget API schemas", () => {
  it("falls back instead of leaking malformed policy data into settings", () => {
    expect(parseWithFallback([{ id: 7 }], BudgetPolicyListSchema, [], { endpoint: "test" })).toEqual([]);
  });

  it("rejects negative usage totals", () => {
    expect(parseWithFallback([{ spent_usd_ticks: -1 }], BudgetStatusListSchema, [], { endpoint: "test" })).toEqual([]);
  });
});
