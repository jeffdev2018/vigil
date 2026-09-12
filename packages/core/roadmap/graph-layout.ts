import { graphlib, layout } from "@dagrejs/dagre";
import type { IssueDependencyEdge } from "../types";

/**
 * Layered (Sugiyama) layout for the roadmap dependency graph (JEF-247).
 *
 * Pure and DOM-free: positions are node CENTER points in an abstract canvas
 * space (rankdir LR, blockers left of what they block); the caller scales and
 * renders them. Every input node gets a position, including disconnected ones.
 *
 * `duplicate` edges are excluded — a duplicate asserts identity, not an
 * ordering, and layering on it would drag two tracks together for no reason.
 * Edges with an endpoint outside the node set are dropped. Cycles are broken
 * by ignoring the edge that would close one (dagre's rank assignment requires
 * an acyclic input).
 */

export const GRAPH_NODE_WIDTH = 220;
export const GRAPH_NODE_HEIGHT = 64;

export interface RoadmapGraphNodePosition {
  id: string;
  x: number;
  y: number;
}

export function layoutDependencyGraph(
  nodes: { id: string }[],
  edges: IssueDependencyEdge[],
): RoadmapGraphNodePosition[] {
  const g = new graphlib.Graph();
  g.setGraph({ rankdir: "LR", nodesep: 40, ranksep: 80 });
  g.setDefaultEdgeLabel(() => ({}));
  for (const node of nodes) {
    g.setNode(node.id, { width: GRAPH_NODE_WIDTH, height: GRAPH_NODE_HEIGHT });
  }

  const ids = new Set(nodes.map((n) => n.id));
  const usable = edges.filter(
    (e) => e.type !== "duplicate" && ids.has(e.from) && ids.has(e.to),
  );
  for (const edge of withoutCycles(usable)) {
    g.setEdge(edge.from, edge.to);
  }

  layout(g);
  return nodes.map((node) => {
    const p = g.node(node.id);
    return { id: node.id, x: p.x, y: p.y };
  });
}

/**
 * Keeps the largest prefix-compatible acyclic subset: an edge is kept unless
 * the kept graph already has a path from its target back to its source.
 */
function withoutCycles(edges: IssueDependencyEdge[]): IssueDependencyEdge[] {
  const adjacency = new Map<string, string[]>();
  const kept: IssueDependencyEdge[] = [];
  for (const edge of edges) {
    if (!reachable(adjacency, edge.to, edge.from)) {
      kept.push(edge);
      const targets = adjacency.get(edge.from) ?? [];
      targets.push(edge.to);
      adjacency.set(edge.from, targets);
    }
  }
  return kept;
}

function reachable(adjacency: Map<string, string[]>, from: string, to: string): boolean {
  const seen = new Set<string>([from]);
  const stack = [from];
  while (stack.length > 0) {
    const current = stack.pop()!;
    if (current === to) return true;
    for (const next of adjacency.get(current) ?? []) {
      if (!seen.has(next)) {
        seen.add(next);
        stack.push(next);
      }
    }
  }
  return false;
}
