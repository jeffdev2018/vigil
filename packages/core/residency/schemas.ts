import { z } from "zod";

// Data residency routing (K46). A workspace declares where its work may run —
// allowed regions, banned providers, on-prem only — and every run is dispatched
// only to a runtime that satisfies it.
//
// The declarations are operator-supplied and unverified: nothing proves a
// machine really sits in eu-west-1. What the policy guarantees is that a run
// never reaches a runtime the workspace has not vouched for, which is the part
// software can enforce. The settings UI says so out loud.

/** Why a runtime was refused. Mirrors service/residency.go. */
export type ResidencyRejectionReason =
  | "banned_provider"
  | "not_on_prem"
  | "region_not_allowed";

/** Where this workspace's work may run. Every field empty means no constraint. */
export interface DataResidencyPolicy {
  region_allowlist: string[];
  banned_providers: string[];
  require_on_prem: boolean;
}

/** The policy plus the cap the server accepts, so the form has one source. */
export interface DataResidencySettings extends DataResidencyPolicy {
  max_list_length: number;
}

// One runtime's declaration about where it runs. It lives on the runtime type
// because that is where it is served, and is re-exported here so residency
// consumers import the whole vocabulary from one module.
export type { RuntimeCompliance } from "../types/agent";

// Lists stay lenient: a backend that returns null, a scalar, or entries of the
// wrong type must degrade to "no constraint" rather than throw. `.loose()`
// keeps fields a newer server added.
export const DataResidencyPolicySchema = z
  .object({
    region_allowlist: z.array(z.string()).catch([]).default([]),
    banned_providers: z.array(z.string()).catch([]).default([]),
    require_on_prem: z.boolean().catch(false),
    max_list_length: z.number().catch(20),
  })
  .loose();

export const RuntimeComplianceSchema = z
  .object({
    region: z.string().catch(""),
    on_prem: z.boolean().catch(false),
  })
  .loose();

export const DATA_RESIDENCY_DEFAULTS: DataResidencySettings = {
  region_allowlist: [],
  banned_providers: [],
  require_on_prem: false,
  max_list_length: 20,
};

/**
 * Whether the policy constrains anything. Drives the neutral empty banner: a
 * workspace with no policy is not misconfigured, it simply has no constraint,
 * and the UI must not dress that up as a warning.
 */
export function isResidencyRestrictive(policy: DataResidencyPolicy | undefined): boolean {
  if (!policy) return false;
  return (
    (policy.region_allowlist ?? []).length > 0 ||
    (policy.banned_providers ?? []).length > 0 ||
    policy.require_on_prem === true
  );
}

/**
 * The same normalization the server applies, run client-side so the chip the
 * user just typed matches what comes back. Trim, lowercase, drop blanks,
 * deduplicate, sort — and cap, so the form refuses locally instead of
 * bouncing off a 400.
 */
export function normalizeResidencyTokens(tokens: string[], max: number): string[] {
  const seen = new Set<string>();
  for (const raw of tokens) {
    const token = raw.trim().toLowerCase();
    if (token) seen.add(token);
  }
  return [...seen].sort().slice(0, max);
}

/**
 * Whether a runtime satisfies the policy, for the runtimes list badge. The
 * authority is the server (service/residency.go) — this only decides how a row
 * is labelled — but the rules must stay identical or the badge lies: an
 * undeclared runtime is non-compliant as soon as the policy is restrictive.
 */
export function runtimeSatisfiesResidency(
  policy: DataResidencyPolicy | undefined,
  runtime: {
    runtime_mode?: string;
    provider?: string;
    compliance?: { region: string; on_prem: boolean } | null;
  },
): { ok: true } | { ok: false; reason: ResidencyRejectionReason } {
  if (!isResidencyRestrictive(policy) || !policy) return { ok: true };
  const provider = (runtime.provider ?? "").trim().toLowerCase();
  if ((policy.banned_providers ?? []).includes(provider)) {
    return { ok: false, reason: "banned_provider" };
  }
  if (policy.require_on_prem === true) {
    if (runtime.runtime_mode === "cloud" || runtime.compliance?.on_prem !== true) {
      return { ok: false, reason: "not_on_prem" };
    }
  }
  const regions = policy.region_allowlist ?? [];
  if (regions.length > 0) {
    const region = (runtime.compliance?.region ?? "").trim().toLowerCase();
    if (!region || !regions.includes(region)) {
      return { ok: false, reason: "region_not_allowed" };
    }
  }
  return { ok: true };
}
