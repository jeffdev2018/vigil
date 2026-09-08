import type { OrgDefinition, OrgEdgeKind } from "../types";

// Pure geometry for the org canvas: units on levels read off the reports_to
// edges, and one SVG path per edge. Kept out of the component so the layout can
// be asserted without a DOM, and so web and desktop draw the identical chart.

export const ORG_CARD_WIDTH = 208;
export const ORG_CARD_HEIGHT = 112;
const GAP_X = 24;
const GAP_Y = 64;
const PAD = 8;

export interface OrgLayoutNode {
  id: string;
  level: number;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface OrgLayoutEdge {
  from: string;
  to: string;
  kind: OrgEdgeKind;
  /** SVG path data in the same coordinate space as the nodes. */
  d: string;
}

export interface OrgLayoutResult {
  nodes: OrgLayoutNode[];
  edges: OrgLayoutEdge[];
  width: number;
  height: number;
}

/** Depth of every unit along reports_to. Cycles stop at the first repeat, so a
 *  malformed definition still lays out instead of hanging. */
function levels(def: OrgDefinition): Map<string, number> {
  const parent = new Map<string, string>();
  for (const e of def.edges ?? []) {
    if (e.kind === "reports_to" && !parent.has(e.from)) parent.set(e.from, e.to);
  }
  const ids = new Set((def.units ?? []).map((u) => u.id));
  const out = new Map<string, number>();
  for (const u of def.units ?? []) {
    const seen = new Set<string>();
    let depth = 0;
    let id: string | undefined = u.id;
    while (id !== undefined) {
      seen.add(id);
      const next: string | undefined = parent.get(id);
      if (next === undefined || !ids.has(next) || seen.has(next)) break;
      depth += 1;
      id = next;
    }
    out.set(u.id, depth);
  }
  return out;
}

/** Elbow from the bottom edge of `a` up into the top edge of `b`, or a side
 *  hop when they sit on the same level. */
function edgePath(a: OrgLayoutNode, b: OrgLayoutNode): string {
  if (a.level === b.level) {
    const ay = a.y + a.height / 2;
    const [left, right] = a.x < b.x ? [a, b] : [b, a];
    const lift = a.y - GAP_Y / 3;
    return `M ${left.x + left.width} ${ay} C ${right.x} ${lift}, ${left.x + left.width} ${lift}, ${right.x} ${ay}`;
  }
  // The child sits below its parent: leave the child's top, enter the parent's bottom.
  const [lower, upper] = a.level > b.level ? [a, b] : [b, a];
  const lx = lower.x + lower.width / 2;
  const ux = upper.x + upper.width / 2;
  const mid = (lower.y + upper.y + upper.height) / 2;
  return `M ${lx} ${lower.y} V ${mid} H ${ux} V ${upper.y + upper.height}`;
}

/**
 * Cards laid out by level, each level centred on the widest one. Node order
 * inside a level follows `def.units`, so editing a unit never reshuffles the
 * canvas under the pointer.
 */
export function orgLayout(def: OrgDefinition): OrgLayoutResult {
  const units = def.units ?? [];
  const depth = levels(def);
  const byLevel = new Map<number, string[]>();
  for (const u of units) {
    const l = depth.get(u.id) ?? 0;
    byLevel.set(l, [...(byLevel.get(l) ?? []), u.id]);
  }
  const widest = Math.max(1, ...[...byLevel.values()].map((row) => row.length));
  const canvasWidth = widest * ORG_CARD_WIDTH + (widest - 1) * GAP_X;

  const nodes: OrgLayoutNode[] = [];
  for (const [level, row] of [...byLevel.entries()].sort((a, b) => a[0] - b[0])) {
    const rowWidth = row.length * ORG_CARD_WIDTH + (row.length - 1) * GAP_X;
    const offset = (canvasWidth - rowWidth) / 2;
    row.forEach((id, i) => {
      nodes.push({
        id,
        level,
        x: PAD + offset + i * (ORG_CARD_WIDTH + GAP_X),
        y: PAD + level * (ORG_CARD_HEIGHT + GAP_Y),
        width: ORG_CARD_WIDTH,
        height: ORG_CARD_HEIGHT,
      });
    });
  }

  const nodeById = new Map(nodes.map((n) => [n.id, n]));
  const edges: OrgLayoutEdge[] = [];
  for (const e of def.edges ?? []) {
    const a = nodeById.get(e.from);
    const b = nodeById.get(e.to);
    if (a === undefined || b === undefined || a.id === b.id) continue;
    edges.push({ from: e.from, to: e.to, kind: e.kind, d: edgePath(a, b) });
  }

  const maxLevel = Math.max(0, ...nodes.map((n) => n.level));
  return {
    nodes,
    edges,
    width: canvasWidth + PAD * 2,
    height: PAD * 2 + (maxLevel + 1) * ORG_CARD_HEIGHT + maxLevel * GAP_Y,
  };
}
