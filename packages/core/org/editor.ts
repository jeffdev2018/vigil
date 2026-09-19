import { z } from "zod";

import type { OrgDefinition, OrgMember, OrgUnit } from "../types";

/** Display reporting parents above children; cycles stay finite and visible. */
export function orgLayout(def: OrgDefinition) {
  const parents = new Map<string, string[]>();
  const ids = new Set(def.units.map(u => u.id));
  for (const edge of def.edges) if (edge.kind === "reports_to" && ids.has(edge.to)) parents.set(edge.from, [...(parents.get(edge.from) ?? []), edge.to]);
  const depths = new Map<string, number>();
  const depth = (id: string, seen = new Set<string>()): number => {
    const cached = depths.get(id);
    if (cached !== undefined) return cached;
    if (seen.has(id)) return 0;
    const path = new Set(seen).add(id);
    const value = Math.max(0, ...(parents.get(id) ?? []).map(parent => 1 + depth(parent, path)));
    depths.set(id, value);
    return value;
  };
  const rows = new Map<number, OrgUnit[]>();
  for (const unit of def.units) { const n = depth(unit.id); rows.set(n, [...(rows.get(n) ?? []), unit]); }
  const width = Math.max(640, ...Array.from(rows.values(), r => r.length * 312 + 48));
  const nodes = Array.from(rows.entries()).flatMap(([row, units]) => units.map((unit, column) => ({ unit, x: (width - units.length * 312) / 2 + column * 312 + 16, y: row * 248 + 56 })));
  return { nodes, width, height: Math.max(300, (Math.max(0, ...rows.keys()) + 1) * 248 + 40) };
}

/** Remove dependent references together, including role committees. */
export function removeOrgUnit(def: OrgDefinition, id: string): OrgDefinition {
  if (orgUnitRemovalBlockers(def, id).length > 0) return def;
  return { ...def, units: def.units.filter(u => u.id !== id), edges: def.edges.filter(e => e.from !== id && e.to !== id), rules: def.rules.filter(r => r.target_unit !== id), committees: def.committees.map(c => ({ ...c, unit_ids: c.unit_ids.filter(u => u !== id) })) };
}

/** Changing the approval threshold must be a separate, explicit edit. */
export function orgUnitRemovalBlockers(def: OrgDefinition, id: string) {
  return def.committees.filter(c => c.unit_ids.includes(id) && c.unit_ids.filter(u => u !== id).length < c.quorum);
}

/** Membership only: never creates a team or an agent, and never changes a manager. */
export function addOrgMembers(def: OrgDefinition, unitId: string, members: OrgMember[]): OrgDefinition {
  return { ...def, units: def.units.map(u => {
    if (u.id !== unitId) return u;
    const seen = new Set(u.members.map(m => `${m.type}:${m.id}`));
    return { ...u, members: [...u.members, ...members.filter(m => {
      const key = `${m.type}:${m.id}`;
      if (seen.has(key)) return false;
      seen.add(key);
      return true;
    })] };
  }) };
}

export function removeOrgMember(def: OrgDefinition, unitId: string, member: OrgMember): OrgDefinition {
  return { ...def, units: def.units.map(u => u.id === unitId ? { ...u, members: u.members.filter(m => m.id !== member.id || m.type !== member.type) } : u) };
}

/** Local roles and routing-lead status do not move to an unrelated team. */
export function moveOrgMember(def: OrgDefinition, from: string, to: string, member: OrgMember): OrgDefinition {
  if (from === to || !def.units.some(u => u.id === to) || !def.units.find(u => u.id === from)?.members.some(m => m.id === member.id && m.type === member.type)) return def;
  return addOrgMembers(removeOrgMember(def, from, member), to, [{ id: member.id, type: member.type }]);
}

export function orgWouldCycle(def: OrgDefinition, from: string, to: string): boolean {
  const pending = [to], seen = new Set<string>();
  while (pending.length) {
    const id = pending.pop()!;
    if (id === from) return true;
    if (seen.has(id)) continue;
    seen.add(id);
    pending.push(...def.edges.filter(e => e.from === id && e.kind === "reports_to").map(e => e.to));
  }
  return false;
}

// Editing is strict: tolerant response schemas must never silently drop user input.
const strings = z.array(z.string());
const unitInput = z.object({
  id: z.string().min(1), name: z.string(), kind: z.string().optional(), owner_id: z.string().optional(),
  model: z.enum(["hierarchy", "squads", "matrix", "circles", "owner_network", "taskforce", "market"]).optional(),
  mission: z.string().max(240).optional(), squad_id: z.string().optional(), mission_goal_id: z.string().optional(),
  human_approval: z.boolean().optional(), deciders: z.record(z.string(), z.string()).optional(),
  budget_usd_ticks: z.number().int().nonnegative().optional(),
  autonomy: z.enum(["read_only", "draft", "approve_payload", "auto"]),
  excludes: z.array(z.enum(["untrusted_input", "sensitive_data", "external_effects"])),
  allow: strings, deny: strings, escalation_quota_per_day: z.number().int().nonnegative(),
  members: z.array(z.object({ id: z.string(), type: z.enum(["agent", "member"]), role: z.string().optional(), role_id: z.string().optional() }).loose()),
  roles: z.array(z.object({ id: z.string(), name: z.string(), responsibilities: z.string().optional(), keywords: strings.optional() }).loose()),
}).loose();
const definitionInput = z.object({
  units: z.array(unitInput),
  edges: z.array(z.object({ from: z.string(), to: z.string(), kind: z.enum(["reports_to", "backs_up", "escalates_to", "consults"]) }).loose()),
  rules: z.array(z.object({ id: z.string(), target_unit: z.string(), priority: z.number(), keywords: strings.optional(), labels: strings.optional(), paths: strings.optional() }).loose()),
  committees: z.array(z.object({ decision_type: z.string(), unit_ids: strings, quorum: z.number(), max_rounds: z.number() }).loose()),
  market: z.object({ price_cap_usd_ticks: z.number(), offers_per_agent_per_day: z.number(), min_offers: z.number() }).loose(),
}).loose();
export function parseEditableOrgDefinition(text: string): { def: OrgDefinition } | { error: string } {
  try { return { def: definitionInput.parse(JSON.parse(text)) }; }
  catch (e) { return { error: e instanceof Error ? e.message : String(e) }; }
}

/** Changed teams retain their full values for an honest comparison. */
export function orgDefinitionChanges(before: OrgDefinition, after: OrgDefinition) {
  const previous = new Map(before.units.map(u => [u.id, u]));
  const next = new Map(after.units.map(u => [u.id, u]));
  const units = [...new Set([...previous.keys(), ...next.keys()])].flatMap(id => {
    const a = previous.get(id), b = next.get(id);
    return JSON.stringify(a) === JSON.stringify(b) ? [] : [{ id, section: "units" as const, before: a, after: b }];
  });
  const sections = (["edges", "rules", "committees", "market"] as const).flatMap(section => JSON.stringify(before[section]) === JSON.stringify(after[section]) ? [] : [{ id: `$${section}`, section, before: undefined, after: undefined }]);
  return [...units, ...sections];
}
