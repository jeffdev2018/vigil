import type { TFunction } from "i18next";
import { ORG_AUTONOMY_ORDER, ORG_DECIDER_CLASSES, ORG_NON_NEGOTIABLE_DENY, ORG_TRUST_ORDER, type OrgProblem } from "@multica/core/org/validate";
import type { OrgEdgeKind, OrgModel } from "@multica/core/types";
import { tKnown } from "../i18n";

// Human labels for every raw domain value the org feature renders — autonomy,
// capabilities, unit kind, approval risk, edge kind, an agent's Trust Dial
// mode, decider classes, end condition, member role, and the seven org
// models — plus the code → sentence translation for the structured fields the
// server started sending alongside its English text (K75 follow-up, see
// server/internal/handler/org_simulate.go and org_ops.go).
//
// The server is free to add an enum value ahead of a frontend release (see
// CLAUDE.md "API Compatibility"): every lookup here falls back to a readable
// generic label, never to `undefined` or to the raw snake_case value —
// except `orgCapabilityLabel`, where the raw value is text a person typed
// into a unit's allow/deny list, not a server enum; showing it verbatim is
// the correct fallback there.

type T = TFunction<"org">;

const unknown = (t: T): string => t(($) => $.unknown_value);

// --- autonomy ----------------------------------------------------------------

export const ORG_AUTONOMY_VALUES = ORG_AUTONOMY_ORDER;

/** "Lit seulement" / "Prépare des brouillons" / … — the single autonomy
 *  vocabulary. The old `autonomy.*` set duplicated it, and its "draft"
 *  collided with the draft *status*. */
export function orgAutonomyLabel(t: T, autonomy: string): string {
  return tKnown(t, "unit.trust_level", autonomy, unknown(t));
}

/** One short sentence: what a unit at this autonomy notch actually does. */
export function orgAutonomyHint(t: T, autonomy: string): string {
  return tKnown(t, "unit.trust_level_hint", autonomy, "");
}

// --- capabilities (allow / deny verbs) ----------------------------------------

/** Infinitive label for a known verb; an unknown one is free text someone
 *  typed into the unit's allow/deny list (see `unit.verb_placeholder`), so it
 *  is shown as typed rather than replaced. */
export function orgCapabilityLabel(t: T, verb: string): string {
  return tKnown(t, "unit.capability", verb, verb);
}

/** Whether a refusal is actually applied by the MCP gateway (see
 *  `ORG_NON_NEGOTIABLE_DENY` / server/pkg/mcpgov/orgdeny.go) or is only a
 *  standing instruction given to the agent, with nothing technical stopping
 *  it. A deny verb outside the non-negotiable list (e.g. "refund") has no
 *  tool-side match. */
export function orgCapabilityEnforced(verb: string): boolean {
  return (ORG_NON_NEGOTIABLE_DENY as readonly string[]).includes(verb);
}

export function orgCapabilityEnforcementLabel(t: T, verb: string): string {
  return orgCapabilityEnforced(verb) ? t(($) => $.unit.capability_enforcement.tool) : t(($) => $.unit.capability_enforcement.instruction);
}

// --- unit kind -----------------------------------------------------------------

export const ORG_UNIT_KIND_VALUES = ["unit", "pool", "circle", "taskforce", "market"] as const;

export function orgUnitKindLabel(t: T, kind: string | undefined): string {
  return tKnown(t, "unit_kind", kind && kind !== "" ? kind : "unit", unknown(t));
}

// --- approval risk ---------------------------------------------------------------

export const ORG_APPROVAL_RISK_VALUES = ["low", "normal", "high"] as const;

/** Empty means no threshold is set — shown as nothing, not as "unknown". */
export function orgApprovalRiskLabel(t: T, risk: string | undefined): string {
  if (!risk) return "";
  return tKnown(t, "approval_risk", risk, unknown(t));
}

// --- edge kind -------------------------------------------------------------------

export const ORG_EDGE_KIND_VALUES: readonly OrgEdgeKind[] = ["reports_to", "escalates_to", "backs_up", "consults"];

export function orgEdgeKindLabel(t: T, kind: string): string {
  return tKnown(t, "edge_kind", kind, unknown(t));
}

/** Only `reports_to` and `escalates_to` change anything at run time: the
 *  first picks the effective model and the hierarchy chain, the second is
 *  the escalation ladder (see `orgEffectiveModel` and `orgEscalationChain`,
 *  server/internal/handler/org.go around 189-239). `backs_up` and `consults`
 *  are informational only. */
export function orgEdgeKindHasEffect(kind: string): boolean {
  return kind === "reports_to" || kind === "escalates_to";
}

export function orgEdgeKindEffectLabel(t: T, kind: string): string {
  return orgEdgeKindHasEffect(kind) ? t(($) => $.edge_kind_effect.live) : t(($) => $.edge_kind_effect.informational);
}

// --- an agent's Trust Dial mode --------------------------------------------------

export const ORG_AGENT_TRUST_VALUES: readonly string[] = ORG_TRUST_ORDER;

export function orgAgentTrustLabel(t: T, mode: string): string {
  return tKnown(t, "agent_trust", mode, unknown(t));
}

// --- decider classes ---------------------------------------------------------------

export const ORG_DECIDER_CLASS_VALUES: readonly string[] = ORG_DECIDER_CLASSES;

export function orgDeciderClassLabel(t: T, cls: string): string {
  return tKnown(t, "unit.decider", cls, unknown(t));
}

// --- end condition -------------------------------------------------------------------

export const ORG_END_CONDITION_VALUES = ["", "all_issues_done", "budget_spent"] as const;

export function orgEndConditionLabel(t: T, condition: string): string {
  return tKnown(t, "form", `end_${condition === "" ? "none" : condition}`, unknown(t));
}

// --- member role in a unit -----------------------------------------------------------

export const ORG_MEMBER_ROLE_VALUES = ["owner", "member", "lead"] as const;

export function orgMemberRoleLabel(t: T, role: string): string {
  return tKnown(t, "member_role", role, unknown(t));
}

// --- org model -------------------------------------------------------------------------

// A local literal, not `ORG_MODELS` from `@multica/core/org` — that module is
// the hook-heavy queries barrel most org tests mock wholesale (see
// `org-wizard.test.tsx`), so importing a plain constant from it would make
// every such mock responsible for re-exporting it. `labels.test.ts`'s
// compile-time exhaustiveness check keeps this list honest against `OrgModel`.
export const ORG_MODEL_VALUES: readonly OrgModel[] = ["hierarchy", "squads", "matrix", "circles", "owner_network", "taskforce", "market"];

export function orgModelLabel(t: T, model: string): string {
  return tKnown(t, "model", model, unknown(t));
}

/** One sentence: what this model actually changes in how work circulates
 *  (see server/internal/handler/org.go 348-401 and core/org/templates.ts). */
export function orgModelDescription(t: T, model: string): string {
  return tKnown(t, "wizard.model_line", model, "");
}

// --- budget / price cap: ticks → USD --------------------------------------------------

/** `budget_usd_ticks` / `price_cap_usd_ticks`: 1,000,000 ticks = $1 — see
 *  `WIZARD_PRICE_CAP_USD_TICKS` (core/org/templates.ts) and the server's
 *  `orgTemplate` market default (server/internal/handler/org.go), both
 *  5,000,000 ticks for a $5 cap. This is a different scale from task/run
 *  cost ticks (`usdFromTicks` in core/issues/cockpit.ts, 1e10 ticks = $1) —
 *  never mix the two. */
const ORG_BUDGET_TICKS_PER_USD = 1_000_000;

export function orgUsdFromTicks(ticks: number): number {
  return ticks / ORG_BUDGET_TICKS_PER_USD;
}

export function orgFormatUsd(ticks: number, locale?: string): string {
  return new Intl.NumberFormat(locale, { style: "currency", currency: "USD", maximumFractionDigits: 2 }).format(orgUsdFromTicks(ticks));
}

// --- validation problem params: labels instead of raw enum values --------------------

/** `validateOrgDefinition` (core/org/validate.ts) puts raw domain values into
 *  a problem's `params` for `{{interpolation}}`. Route them through the
 *  matching label before handing them to `t()`, so the canvas never shows a
 *  bare `approve_payload` or `outbound_data` to a reader. */
export function orgProblemParams(t: T, problem: Pick<OrgProblem, "code" | "params">): Record<string, string | number> {
  switch (problem.code) {
    case "autonomy_over_trust":
      return {
        ...problem.params,
        autonomy: orgAutonomyLabel(t, String(problem.params.autonomy ?? "")),
        trust: orgAgentTrustLabel(t, String(problem.params.trust ?? "")),
      };
    case "deciders_missing":
      return {
        ...problem.params,
        classes: String(problem.params.classes ?? "")
          .split(",")
          .map((c) => orgDeciderClassLabel(t, c.trim()))
          .filter((c) => c !== "")
          .join(", "),
      };
    default:
      return problem.params;
  }
}

// --- server-coded text: simulator notes, restructuring proposals, activation ---------
//
// The server keeps sending its English sentence (installed desktop clients
// read it) and now sends a `code` (+ `params`) beside it. These resolve the
// code to a translated sentence, falling back to the server's own text when
// the code is missing or the client is ahead of a release that added one.

/** Like `tKnown`, but also forwards interpolation params — `tKnown` only
 *  forwards `defaultValue`, which these coded messages need alongside their
 *  own `{{unit}}` / `{{risk}}` / `{{days}}` placeholders. */
function tCoded(t: T, pathPrefix: string, leaf: string, params: Record<string, string | number>, fallback: string): string {
  const loose = t as unknown as (key: string, opts: Record<string, unknown>) => string;
  const missing = " labels:missing ";
  const result = loose(`${pathPrefix}.${leaf}`, { ...params, defaultValue: missing });
  return result === missing ? fallback : result;
}

export function orgSimulateNoteText(t: T, fallbackText: string, code?: string, params?: Record<string, string | number>): string {
  if (!code) return fallbackText;
  return tCoded(t, "simulate_note", code, params ?? {}, fallbackText);
}

export function orgProposalTitle(t: T, fallbackTitle: string, code?: string, params?: Record<string, string | number>): string {
  if (!code) return fallbackTitle;
  return tCoded(t, `proposal.${code}`, "title", params ?? {}, fallbackTitle);
}

export function orgProposalBody(t: T, fallbackBody: string, code?: string, params?: Record<string, string | number>): string {
  if (!code) return fallbackBody;
  return tCoded(t, `proposal.${code}`, "body", params ?? {}, fallbackBody);
}

export function orgProposalMeasure(t: T, fallbackMeasure: string, code?: string, params?: Record<string, string | number>): string {
  if (!code) return fallbackMeasure;
  return tCoded(t, `proposal.${code}`, "measure", params ?? {}, fallbackMeasure);
}

export function orgActivationRequirementText(t: T, fallbackText: string, code?: string): string {
  if (!code) return fallbackText;
  return tKnown(t, "activation_requirement", code, fallbackText);
}
