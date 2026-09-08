// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "../api/schema";
import {
  DATA_RESIDENCY_DEFAULTS,
  DataResidencyPolicySchema,
  RuntimeComplianceSchema,
  isResidencyRestrictive,
  normalizeResidencyTokens,
  runtimeSatisfiesResidency,
} from "./schemas";

// Data residency (K46). Installed desktop builds talk to newer servers, so a
// drifted or malformed response must degrade to "no constraint" rather than
// throw — an exception here would white-screen the settings tab, and a policy
// that failed to parse must never be read as a stricter one.

describe("DataResidencyPolicySchema", () => {
  it("fills in every field a sparse response omits", () => {
    const parsed = DataResidencyPolicySchema.parse({});
    expect(parsed.region_allowlist).toEqual([]);
    expect(parsed.banned_providers).toEqual([]);
    expect(parsed.require_on_prem).toBe(false);
    expect(parsed.max_list_length).toBe(20);
  });

  it("keeps fields a newer server added", () => {
    const parsed = DataResidencyPolicySchema.parse({ require_on_prem: true, future_field: "x" });
    expect(parsed.require_on_prem).toBe(true);
    expect((parsed as Record<string, unknown>).future_field).toBe("x");
  });

  it("degrades a malformed list to no constraint instead of throwing", () => {
    const parsed = DataResidencyPolicySchema.parse({
      region_allowlist: "eu-west-1",
      banned_providers: null,
      require_on_prem: "yes",
    });
    expect(parsed.region_allowlist).toEqual([]);
    expect(parsed.banned_providers).toEqual([]);
    expect(parsed.require_on_prem).toBe(false);
  });

  it("falls back rather than throwing on a wholly malformed response", () => {
    for (const bad of [null, "nope", 42, []]) {
      expect(
        parseWithFallback(bad, DataResidencyPolicySchema, DATA_RESIDENCY_DEFAULTS, {
          endpoint: "GET /api/data-residency",
        }),
      ).toEqual(DATA_RESIDENCY_DEFAULTS);
    }
  });
});

describe("RuntimeComplianceSchema", () => {
  it("degrades a malformed declaration to an undeclared one", () => {
    const parsed = RuntimeComplianceSchema.parse({ region: 12, on_prem: "true" });
    expect(parsed.region).toBe("");
    expect(parsed.on_prem).toBe(false);
  });
});

describe("isResidencyRestrictive", () => {
  it.each([
    ["undefined", undefined, false],
    ["empty", { region_allowlist: [], banned_providers: [], require_on_prem: false }, false],
    ["regions", { region_allowlist: ["eu-west-1"], banned_providers: [], require_on_prem: false }, true],
    ["providers", { region_allowlist: [], banned_providers: ["codex"], require_on_prem: false }, true],
    ["on-prem", { region_allowlist: [], banned_providers: [], require_on_prem: true }, true],
  ])("%s", (_name, policy, want) => {
    expect(isResidencyRestrictive(policy)).toBe(want);
  });
});

describe("normalizeResidencyTokens", () => {
  it("trims, lowercases, deduplicates and sorts", () => {
    expect(normalizeResidencyTokens([" EU-West-1 ", "eu-west-1", "", "af-south-1"], 20)).toEqual([
      "af-south-1",
      "eu-west-1",
    ]);
  });

  it("caps the list so the form refuses locally instead of bouncing off a 400", () => {
    const many = Array.from({ length: 25 }, (_, i) => `r${String(i).padStart(2, "0")}`);
    expect(normalizeResidencyTokens(many, 20)).toHaveLength(20);
  });
});

describe("runtimeSatisfiesResidency", () => {
  const rt = (over: Record<string, unknown> = {}) => ({
    runtime_mode: "local",
    provider: "claude",
    compliance: { region: "eu-west-1", on_prem: true },
    ...over,
  });

  it("admits anything when the workspace declares no policy", () => {
    expect(runtimeSatisfiesResidency(undefined, rt({ compliance: null, runtime_mode: "cloud" }))).toEqual({
      ok: true,
    });
  });

  it("refuses a banned provider whatever it declared", () => {
    const policy = { region_allowlist: [], banned_providers: ["codex"], require_on_prem: false };
    expect(runtimeSatisfiesResidency(policy, rt({ provider: "Codex" }))).toEqual({
      ok: false,
      reason: "banned_provider",
    });
  });

  it("refuses an undeclared runtime as soon as the policy is restrictive", () => {
    const policy = { region_allowlist: ["eu-west-1"], banned_providers: [], require_on_prem: false };
    expect(runtimeSatisfiesResidency(policy, rt({ compliance: null }))).toEqual({
      ok: false,
      reason: "region_not_allowed",
    });
  });

  it("never lets a cloud runtime satisfy require_on_prem", () => {
    const policy = { region_allowlist: [], banned_providers: [], require_on_prem: true };
    expect(runtimeSatisfiesResidency(policy, rt({ runtime_mode: "cloud" }))).toEqual({
      ok: false,
      reason: "not_on_prem",
    });
  });

  it("admits a runtime that satisfies every rule", () => {
    const policy = {
      region_allowlist: ["eu-west-1"],
      banned_providers: ["codex"],
      require_on_prem: true,
    };
    expect(runtimeSatisfiesResidency(policy, rt())).toEqual({ ok: true });
  });
});

// The declaration echoed by PUT/DELETE /api/runtimes/:id/compliance is the one
// field those calls assert something about, so a drifted value must read as
// "not declared" rather than as a region the policy might accidentally match.
describe("a drifted declaration never becomes a matchable region", () => {
  it("parses to an empty region, which no allowlist admits", () => {
    const parsed = RuntimeComplianceSchema.parse({ region: { nested: true }, on_prem: 1 });
    expect(
      runtimeSatisfiesResidency(
        { region_allowlist: ["eu-west-1"], banned_providers: [], require_on_prem: false },
        { runtime_mode: "local", provider: "claude", compliance: parsed },
      ),
    ).toEqual({ ok: false, reason: "region_not_allowed" });
  });
});
