import type { OrgDefinition, OrgMember, OrgUnit } from "../types";

// The people view of a structure: one card per person or agent, one line per
// "reports to". Nothing here is stored — it is read off the units and their
// reports_to edges, so the chart and the engine can never disagree. Pure, so
// web and desktop lay out the same picture and the derivation is testable
// without a DOM.

export interface OrgPerson {
  /** `${unitId}/${type}:${id}`; the same actor in two units gets two cards. */
  key: string;
  type: OrgMember["type"];
  id: string;
  unitId: string;
  /** The person's role in the unit, else the unit's own name. */
  title: string;
  /** Leads its unit: the unit's owner, else a decider, else the first human. */
  lead: boolean;
  /** Key of the person this one reports to; null at the top of the chart. */
  reportsTo: string | null;
}

export const ORG_PERSON_WIDTH = 236;
export const ORG_PERSON_HEIGHT = 72;
const GAP_X = 28;
const GAP_Y = 56;
const PAD = 12;
/** Past this many reports without a team of their own, they stack in rows off
 *  a spine instead of one endless line — a lead with thirty agents stays readable. */
const MAX_PER_ROW = 4;
const SPINE_X = 16;
const ROW_GAP_Y = 16;

export const orgPersonKey = (unitId: string, m: Pick<OrgMember, "type" | "id">): string => `${unitId}/${m.type}:${m.id}`;

function leadOf(unit: OrgUnit): OrgMember | undefined {
  const members = unit.members ?? [];
  const owner = members.find((m) => m.type === "member" && m.id === unit.owner_id);
  if (owner !== undefined) return owner;
  const deciderIds = new Set(Object.values(unit.deciders ?? {}));
  const decider = members.find((m) => deciderIds.has(m.id));
  if (decider !== undefined) return decider;
  return members.find((m) => m.type === "member") ?? members[0];
}

function titleOf(unit: OrgUnit, m: OrgMember): string {
  const role = m.role?.trim();
  if (role) return role;
  const named = (unit.roles ?? []).find((r) => r.id === m.role_id)?.name?.trim();
  return named || unit.name;
}

/**
 * Every member of every unit, each reporting to its unit's lead; a lead reports
 * to the lead of the nearest ancestor unit that has one. A unit without members
 * is invisible here but still relays its children upward.
 */
export function orgPeople(def: OrgDefinition): OrgPerson[] {
  const units = def.units ?? [];
  const parentUnit = new Map<string, string>();
  for (const e of def.edges ?? []) {
    if (e.kind === "reports_to" && !parentUnit.has(e.from)) parentUnit.set(e.from, e.to);
  }
  const leads = new Map<string, OrgMember | undefined>(units.map((u) => [u.id, leadOf(u)]));

  const managerOf = (unitId: string): string | null => {
    const seen = new Set<string>([unitId]);
    let cur = parentUnit.get(unitId);
    while (cur !== undefined && !seen.has(cur)) {
      seen.add(cur);
      const lead = leads.get(cur);
      if (lead !== undefined) return orgPersonKey(cur, lead);
      cur = parentUnit.get(cur);
    }
    return null;
  };

  const out: OrgPerson[] = [];
  for (const u of units) {
    const lead = leads.get(u.id);
    if (lead === undefined) continue;
    const leadKey = orgPersonKey(u.id, lead);
    const above = managerOf(u.id);
    for (const m of u.members ?? []) {
      const key = orgPersonKey(u.id, m);
      const isLead = key === leadKey;
      out.push({ key, type: m.type, id: m.id, unitId: u.id, title: titleOf(u, m), lead: isLead, reportsTo: isLead ? above : leadKey });
    }
  }
  return out;
}

export interface OrgPeopleNode {
  key: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface OrgPeopleEdge {
  from: string;
  to: string;
  /** SVG path data, orthogonal, in the nodes' coordinate space. */
  d: string;
}

export interface OrgPeopleLayout {
  nodes: OrgPeopleNode[];
  edges: OrgPeopleEdge[];
  width: number;
  height: number;
}

/** Classic tidy tree: a parent sits centred over the block of its reports,
 *  roots side by side. Order follows `people`, so an edit never reshuffles
 *  the chart under the pointer. A report whose manager is missing is a root.
 *  Many leaf reports under one person wrap into rows hung off a spine. */
export function orgPeopleLayout(people: OrgPerson[]): OrgPeopleLayout {
  const keys = new Set(people.map((p) => p.key));
  const children = new Map<string | null, OrgPerson[]>();
  for (const p of people) {
    const parent = p.reportsTo !== null && keys.has(p.reportsTo) ? p.reportsTo : null;
    children.set(parent, [...(children.get(parent) ?? []), p]);
  }
  // Guard against a manager loop: a person already on the path is treated as a root.
  const kidsOf = (key: string, path: Set<string>) => (children.get(key) ?? []).filter((k) => !path.has(k.key));
  const stacks = (key: string, path: Set<string>): boolean => {
    const kids = kidsOf(key, path);
    return kids.length > MAX_PER_ROW && kids.every((k) => kidsOf(k.key, new Set(path).add(key)).length === 0);
  };
  const sizes = new Map<string, { w: number; h: number }>();
  const subtree = (key: string, path: Set<string>): { w: number; h: number } => {
    const cached = sizes.get(key);
    if (cached !== undefined) return cached;
    const kids = kidsOf(key, path);
    const next = new Set(path).add(key);
    let size: { w: number; h: number };
    if (kids.length === 0) {
      size = { w: ORG_PERSON_WIDTH, h: ORG_PERSON_HEIGHT };
    } else if (stacks(key, path)) {
      const cols = Math.min(MAX_PER_ROW, kids.length);
      const rows = Math.ceil(kids.length / MAX_PER_ROW);
      size = {
        w: Math.max(ORG_PERSON_WIDTH, SPINE_X + cols * ORG_PERSON_WIDTH + (cols - 1) * GAP_X),
        h: ORG_PERSON_HEIGHT + GAP_Y + rows * ORG_PERSON_HEIGHT + (rows - 1) * ROW_GAP_Y,
      };
    } else {
      const under = kids.map((k) => subtree(k.key, next));
      size = {
        w: Math.max(ORG_PERSON_WIDTH, under.reduce((sum, u) => sum + u.w, 0) + (kids.length - 1) * GAP_X),
        h: ORG_PERSON_HEIGHT + GAP_Y + Math.max(...under.map((u) => u.h)),
      };
    }
    sizes.set(key, size);
    return size;
  };

  const nodes: OrgPeopleNode[] = [];
  const stacked = new Set<string>();
  const placed = new Set<string>();
  const place = (key: string, left: number, top: number, path: Set<string>) => {
    if (placed.has(key)) return;
    placed.add(key);
    const { w } = subtree(key, path);
    const next = new Set(path).add(key);
    const kids = kidsOf(key, path);
    const below = top + ORG_PERSON_HEIGHT + GAP_Y;
    if (stacks(key, path)) {
      // The lead sits over the block's spine; reports fill rows of MAX_PER_ROW.
      nodes.push({ key, x: left + SPINE_X, y: top, width: ORG_PERSON_WIDTH, height: ORG_PERSON_HEIGHT });
      kids.forEach((kid, i) => {
        placed.add(kid.key);
        stacked.add(kid.key);
        nodes.push({
          key: kid.key,
          x: left + SPINE_X + (i % MAX_PER_ROW) * (ORG_PERSON_WIDTH + GAP_X),
          y: below + Math.floor(i / MAX_PER_ROW) * (ORG_PERSON_HEIGHT + ROW_GAP_Y),
          width: ORG_PERSON_WIDTH,
          height: ORG_PERSON_HEIGHT,
        });
      });
      return;
    }
    nodes.push({ key, x: left + (w - ORG_PERSON_WIDTH) / 2, y: top, width: ORG_PERSON_WIDTH, height: ORG_PERSON_HEIGHT });
    let cursor = left;
    for (const kid of kids) {
      place(kid.key, cursor, below, next);
      cursor += subtree(kid.key, next).w + GAP_X;
    }
  };
  let cursor = PAD;
  for (const root of children.get(null) ?? []) {
    place(root.key, cursor, PAD, new Set());
    cursor += subtree(root.key, new Set()).w + GAP_X;
  }
  // Anyone left over sits in a manager loop nobody reached: lay them out as roots.
  for (const p of people) {
    if (placed.has(p.key)) continue;
    place(p.key, cursor, PAD, new Set());
    cursor += subtree(p.key, new Set()).w + GAP_X;
  }

  const byKey = new Map(nodes.map((n) => [n.key, n]));
  const edges: OrgPeopleEdge[] = [];
  for (const p of people) {
    const a = p.reportsTo === null ? undefined : byKey.get(p.reportsTo);
    const b = byKey.get(p.key);
    if (a === undefined || b === undefined || b.y <= a.y) continue;
    const ax = a.x + a.width / 2;
    if (stacked.has(p.key)) {
      // Down the spine on the block's left, then a stub into the card's side.
      const sx = a.x - SPINE_X / 2;
      const cy = b.y + b.height / 2;
      edges.push({ from: p.reportsTo as string, to: p.key, d: `M ${ax} ${a.y + a.height} V ${a.y + a.height + GAP_Y / 2} H ${sx} V ${cy} H ${b.x}` });
      continue;
    }
    const bx = b.x + b.width / 2;
    const mid = (a.y + a.height + b.y) / 2;
    edges.push({ from: p.reportsTo as string, to: p.key, d: `M ${ax} ${a.y + a.height} V ${mid} H ${bx} V ${b.y}` });
  }
  const width = Math.max(0, ...nodes.map((n) => n.x + n.width)) + PAD;
  const height = Math.max(0, ...nodes.map((n) => n.y + n.height)) + PAD;
  return { nodes, edges, width, height };
}

const slug = (s: string): string => s.toLowerCase().normalize("NFD").replace(/[^a-z0-9]+/g, "-").replace(/(^-|-$)/g, "") || "unit";

/**
 * Put `member` under `managerKey`: into the manager's unit. With no manager the
 * newcomer opens a unit of their own at the top of the chart, named after their
 * title, so the first card of an empty structure is one gesture.
 */
export function orgAddTeammate(def: OrgDefinition, managerKey: string | null, member: OrgMember, unitName: string): OrgDefinition {
  const units = def.units ?? [];
  const manager = managerKey === null ? undefined : orgPeople(def).find((p) => p.key === managerKey);
  if (manager !== undefined) {
    return {
      ...def,
      units: units.map((u) => {
        if (u.id !== manager.unitId) return u;
        const already = u.members.findIndex((m) => m.type === member.type && m.id === member.id);
        const members = already === -1 ? [...u.members, member] : u.members.map((m, i) => (i === already ? { ...m, role: member.role } : m));
        return { ...u, members };
      }),
    };
  }
  let id = slug(unitName);
  for (let n = 2; units.some((u) => u.id === id); n += 1) id = `${slug(unitName)}-${n}`;
  const unit: OrgUnit = {
    id,
    name: unitName,
    ...(member.type === "member" ? { owner_id: member.id } : {}),
    excludes: ["external_effects"],
    autonomy: "draft",
    allow: [],
    deny: [],
    escalation_quota_per_day: 5,
    members: [member],
    roles: [],
  };
  return { ...def, units: [...units, unit] };
}
