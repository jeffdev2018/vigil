// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  CriticPolicySchema,
  CriticVerdictListSchema,
  EMPTY_CRITIC_POLICY,
  EMPTY_CRITIC_VERDICTS,
  criticCostTicks,
  criticCostUsd,
  criticPolicyError,
} from "./schemas";

// Canonical parsing + validation matrix for F25. The card and the settings
// section keep the happy path and the wiring; they do not re-run this.

describe("critic verdict parsing", () => {
  it("reads an unknown verdict as concerns", () => {
    // The rule that matters for an installed client against a newer server:
    // a word this build does not know must never read as approval.
    const parsed = CriticVerdictListSchema.parse({
      verdicts: [{ id: "v1", verdict: "escalate", summary: "later word" }],
    });
    expect(parsed.verdicts[0]?.verdict).toBe("concerns");
    expect(parsed.verdicts[0]?.summary).toBe("later word");
  });

  it("keeps the three known verdicts", () => {
    const parsed = CriticVerdictListSchema.parse({
      verdicts: [
        { id: "a", verdict: "pass" },
        { id: "b", verdict: "concerns" },
        { id: "c", verdict: "block" },
      ],
    });
    expect(parsed.verdicts.map((v) => v.verdict)).toEqual(["pass", "concerns", "block"]);
  });

  it("survives a malformed findings blob", () => {
    const parsed = CriticVerdictListSchema.parse({
      verdicts: [{ id: "v1", verdict: "block", findings: "not an array" }],
    });
    expect(parsed.verdicts[0]?.findings).toEqual([]);
    expect(parsed.verdicts[0]?.verdict).toBe("block");
  });

  it("normalizes an unknown finding severity to info", () => {
    const parsed = CriticVerdictListSchema.parse({
      verdicts: [{ id: "v1", verdict: "concerns", findings: [{ severity: "fatal", title: "x" }] }],
    });
    expect(parsed.verdicts[0]?.findings[0]?.severity).toBe("info");
  });

  it("falls back to an empty list on a malformed response", () => {
    expect(
      parseWithFallback("not json at all", CriticVerdictListSchema, EMPTY_CRITIC_VERDICTS, {
        endpoint: "GET /api/issues/{id}/critic-verdicts",
      }),
    ).toEqual(EMPTY_CRITIC_VERDICTS);
  });
});

describe("critic policy parsing", () => {
  it("falls back to the feature OFF on a malformed response", () => {
    // Reading an unparseable answer as "on" would tell a team its deliveries
    // are reviewed when nothing knows whether they are.
    const parsed = parseWithFallback(null, CriticPolicySchema, EMPTY_CRITIC_POLICY, {
      endpoint: "GET /api/critic-policies/{subjectType}/{subjectId}",
    });
    expect(parsed.enabled).toBe(false);
    expect(parsed).toEqual(EMPTY_CRITIC_POLICY);
  });

  it("keeps a partial policy usable", () => {
    const parsed = CriticPolicySchema.parse({ enabled: true, critic_agent_id: "a1" });
    expect(parsed.enabled).toBe(true);
    expect(parsed.require_distinct_provider).toBe(true);
    expect(parsed.max_rounds).toBe(1);
    expect(parsed.phases).toEqual(["change"]);
  });
});

describe("criticPolicyError", () => {
  it("allows a disabled policy with nothing filled in", () => {
    expect(criticPolicyError({ enabled: false }, "agent", "a1")).toBe("");
  });

  it("refuses enabling without a critic", () => {
    expect(criticPolicyError({ enabled: true }, "agent", "a1")).toBe("critic_required");
    expect(criticPolicyError({ enabled: true, critic_agent_id: "  " }, "agent", "a1")).toBe("critic_required");
  });

  it("refuses an agent as its own critic", () => {
    expect(criticPolicyError({ enabled: true, critic_agent_id: "a1" }, "agent", "a1")).toBe("critic_is_author");
  });

  it("lets a squad name any agent, including one of its own members", () => {
    // A squad is not an agent: the leader's own id is a legitimate critic for
    // a squad policy, and only the server can know the roster.
    expect(criticPolicyError({ enabled: true, critic_agent_id: "s1" }, "squad", "s1")).toBe("");
  });
});

describe("cost budget conversion", () => {
  it("renders nothing for no cap", () => {
    expect(criticCostUsd(null)).toBe("");
    expect(criticCostUsd(0)).toBe("");
  });

  it("round-trips a dollar amount", () => {
    const ticks = criticCostTicks("1.50");
    expect(ticks).toBe(15_000_000_000);
    expect(criticCostUsd(ticks)).toBe("$1.50");
  });

  it("keeps four decimals under a cent", () => {
    expect(criticCostUsd(criticCostTicks("0.0025"))).toBe("$0.0025");
  });

  it("reads an unusable amount as no cap", () => {
    expect(criticCostTicks("")).toBeNull();
    expect(criticCostTicks("abc")).toBeNull();
    expect(criticCostTicks("-3")).toBeNull();
  });
});
