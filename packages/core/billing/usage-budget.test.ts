// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  DEFAULT_USAGE_BUDGET_ALERT_RATIO,
  deriveUsageBudgetLevel,
} from "./usage-budget";

describe("deriveUsageBudgetLevel", () => {
  it("stays ok below the alert ratio", () => {
    expect(
      deriveUsageBudgetLevel({ used: 3, reserved: 1, limit: 10 }),
    ).toBe("ok");
  });

  it("alerts at the default ratio before the hard ceiling", () => {
    expect(DEFAULT_USAGE_BUDGET_ALERT_RATIO).toBe(0.8);
    expect(
      deriveUsageBudgetLevel({ used: 7, reserved: 1, limit: 10 }),
    ).toBe("alert");
    expect(
      deriveUsageBudgetLevel({ used: 8, reserved: 0, limit: 10 }),
    ).toBe("alert");
  });

  it("blocks at capacity and when the server already marked reached", () => {
    expect(
      deriveUsageBudgetLevel({ used: 9, reserved: 1, limit: 10 }),
    ).toBe("blocked");
    expect(
      deriveUsageBudgetLevel({
        used: 5,
        reserved: 0,
        limit: 10,
        reached: true,
      }),
    ).toBe("blocked");
    expect(deriveUsageBudgetLevel({ used: 0, reserved: 0, limit: 0 })).toBe(
      "blocked",
    );
  });

  it("does not invent a block from non-finite inputs", () => {
    expect(
      deriveUsageBudgetLevel({ used: Number.NaN, reserved: 0, limit: 10 }),
    ).toBe("ok");
  });
});
