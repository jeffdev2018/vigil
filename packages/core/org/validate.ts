import type { OrgAutonomy, OrgDefinition, OrgModel, OrgProperty, OrgUnit } from "../types";

// Client-side mirror of the server's validateOrg (server/internal/handler/org.go).
// It exists so the canvas can answer while the user is still dragging, not only
// at save: the server stays the authority, this module stays advisory. Every
// invariant here has a counterpart there — when one moves, move both.

/** Merged into every unit's deny list at save; the UI shows them locked.
 *  Mirrors `orgNonNegotiableDeny` in server/internal/handler/org.go. */
export const ORG_NON_NEGOTIABLE_DENY = [
  "delete",
  "bill",
  "send_external_without_approval",
  "touch_secrets",
  "commit_money",
] as const;

/** Mirrors `orgProperties` in server/internal/handler/org.go. */
export const ORG_PROPERTIES: OrgProperty[] = ["untrusted_input", "sensitive_data", "external_effects"];

/** Mirrors `orgAutonomyRank`. The Trust Dial's four notches, in order. */
export const ORG_AUTONOMY_ORDER: OrgAutonomy[] = ["read_only", "draft", "approve_payload", "auto"];

/** Mirrors `trustRank` in server/internal/handler/trust_mode.go. An agent's dial
 *  caps the autonomy a unit may grant it. */
export const ORG_TRUST_ORDER = ["observer", "propose", "approval", "autonomous"] as const;

/** Decision classes a unit with external effects must name a decider for. */
export const ORG_DECIDER_CLASSES = ["money", "outbound_data", "external_message"] as const;

const autonomyRank = (a: OrgAutonomy): number => ORG_AUTONOMY_ORDER.indexOf(a);
const trustRank = (mode: string): number => ORG_TRUST_ORDER.indexOf(mode as (typeof ORG_TRUST_ORDER)[number]);

/** A problem the canvas can point at. `code` names the i18n key, `unit_id` the
 *  card it belongs on; edge problems carry both endpoints and no card. */
export interface OrgProblem {
  code:
    | "unit_name_required"
    | "duplicate_unit_id"
    | "taskforce_unit_model"
    | "hierarchy_roots"
    | "autonomy_over_trust"
    | "rule_of_two_unit"
    | "rule_of_two_edge"
    | "deciders_missing"
    | "committee_termination"
    | "market_price_cap"
    | "squads_needs_agents"
    | "circles_needs_role";
  unit_id?: string;
  params: Record<string, string | number>;
}

/** The three properties a unit is exposed to: every property it does not exclude. */
export function orgUnitProperties(unit: Pick<OrgUnit, "excludes">): OrgProperty[] {
  const excluded = new Set(unit.excludes ?? []);
  return ORG_PROPERTIES.filter((p) => !excluded.has(p));
}

/** True when the unit holds untrusted input, sensitive data and external effects
 *  at once — the Rule of Two trigger. */
export function orgUnitBreaksRuleOfTwo(unit: Pick<OrgUnit, "excludes" | "human_approval">): boolean {
  return orgUnitProperties(unit).length === ORG_PROPERTIES.length && unit.human_approval !== true;
}

/** The unit `unitId` reports to, or undefined at a root. */
function parentOf(def: OrgDefinition, unitId: string): string | undefined {
  return (def.edges ?? []).find((e) => e.from === unitId && e.kind === "reports_to")?.to;
}

/** The model a unit actually runs under: its own, else the nearest ancestor's
 *  along reports_to, else the structure's. Mirrors `orgEffectiveModel`. */
export function orgEffectiveModel(def: OrgDefinition, unitId: string, structureModel: OrgModel): OrgModel {
  const seen = new Set<string>();
  let id: string | undefined = unitId;
  while (id !== undefined && !seen.has(id)) {
    seen.add(id);
    const unit: OrgUnit | undefined = (def.units ?? []).find((u) => u.id === id);
    if (unit === undefined) break;
    if (unit.model !== undefined) return unit.model;
    id = parentOf(def, id);
  }
  return structureModel;
}

export interface OrgValidateContext {
  model: OrgModel;
  /** Agent id → Trust Dial mode, when the agent list is loaded. Unknown agents
   *  are not flagged: only the server can tell a missing agent from a stale cache. */
  agentTrust?: Record<string, string>;
  /** Agent id → display name, for readable messages. */
  agentName?: Record<string, string>;
}

/**
 * Immediate advisory checks, in the order a reader meets them:
 * identity, then the Trust Dial ceiling, then the Rule of Two, then the
 * per-model shape. The server still validates identities, references and writes.
 */
export function validateOrgDefinition(def: OrgDefinition, ctx: OrgValidateContext): OrgProblem[] {
  const problems: OrgProblem[] = [];
  const units = def.units ?? [];
  const seen = new Set<string>();

  for (const u of units) {
    const id = (u.id ?? "").trim();
    const name = (u.name ?? "").trim();
    if (id === "" || name === "") {
      problems.push({ code: "unit_name_required", unit_id: id || undefined, params: {} });
      continue;
    }
    if (seen.has(id)) problems.push({ code: "duplicate_unit_id", unit_id: id, params: { id } });
    seen.add(id);

    if (u.model === "taskforce") {
      problems.push({ code: "taskforce_unit_model", unit_id: id, params: { unit: name } });
    }

    // The structure cannot grant an agent more than its own Trust Dial.
    for (const m of u.members ?? []) {
      if (m.type !== "agent") continue;
      const mode = ctx.agentTrust?.[m.id];
      if (mode === undefined || trustRank(mode) < 0) continue;
      if (autonomyRank(u.autonomy) > trustRank(mode)) {
        problems.push({
          code: "autonomy_over_trust",
          unit_id: id,
          params: { unit: name, autonomy: u.autonomy, trust: mode, agent: ctx.agentName?.[m.id] ?? m.id },
        });
      }
    }

    if (orgUnitBreaksRuleOfTwo(u)) {
      problems.push({ code: "rule_of_two_unit", unit_id: id, params: { unit: name } });
    }

    if (orgUnitProperties(u).includes("external_effects")) {
      const missing = ORG_DECIDER_CLASSES.filter((c) => ((u.deciders ?? {})[c] ?? "").trim() === "");
      if (missing.length > 0) {
        problems.push({ code: "deciders_missing", unit_id: id, params: { unit: name, classes: missing.join(", ") } });
      }
    }
  }

  const unitById = new Map(units.map((u) => [u.id, u]));
  for (const e of def.edges ?? []) {
    const from = unitById.get(e.from);
    const to = unitById.get(e.to);
    if (from === undefined || to === undefined) continue;
    // Rule of Two per path: two units that together cumulate the three properties.
    const union = new Set([...orgUnitProperties(from), ...orgUnitProperties(to)]);
    if (union.size === ORG_PROPERTIES.length && e.human_approval !== true) {
      problems.push({ code: "rule_of_two_edge", params: { from: from.name, to: to.name } });
    }
  }

  if (ctx.model === "hierarchy" && units.length > 0) {
    const roots = units.filter((u) => parentOf(def, u.id) === undefined).length;
    if (roots !== 1) problems.push({ code: "hierarchy_roots", params: { count: roots } });
  }

  for (const c of def.committees ?? []) {
    const size = c.unit_ids?.length ?? 0;
    if (size === 0 || c.quorum < 1 || c.quorum > size || c.max_rounds < 1) {
      problems.push({ code: "committee_termination", params: { decision: c.decision_type, max: size } });
    }
  }

  const anyMarket = units.some((u) => orgEffectiveModel(def, u.id, ctx.model) === "market");
  if (anyMarket && (def.market?.price_cap_usd_ticks ?? 0) <= 0) {
    problems.push({ code: "market_price_cap", params: {} });
  }

  for (const u of units) {
    const effective = orgEffectiveModel(def, u.id, ctx.model);
    const agents = (u.members ?? []).filter((m) => m.type === "agent").length;
    if (effective === "squads" && (u.squad_id ?? "") === "" && agents === 0) {
      problems.push({ code: "squads_needs_agents", unit_id: u.id, params: { unit: u.name } });
    }
    if (effective === "circles" && (u.roles?.length ?? 0) === 0) {
      problems.push({ code: "circles_needs_role", unit_id: u.id, params: { unit: u.name } });
    }
  }

  return problems;
}

/** The problems that belong on one unit's card. */
export function orgProblemsForUnit(problems: OrgProblem[], unitId: string): OrgProblem[] {
  return problems.filter((p) => p.unit_id === unitId);
}
