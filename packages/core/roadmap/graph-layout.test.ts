// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IssueDependencyEdge } from "../types";
import { GRAPH_NODE_HEIGHT, GRAPH_NODE_WIDTH, layoutDependencyGraph } from "./graph-layout";

describe("layoutDependencyGraph", () => {
  it("returns an empty layout for an empty graph", () => {
    expect(layoutDependencyGraph([], [])).toEqual([]);
  });

  it("positions every node even without edges", () => {
    const laid = layoutDependencyGraph([{ id: "a" }, { id: "b" }], []);
    expect(laid).toHaveLength(2);
    for (const p of laid) {
      expect(Number.isFinite(p.x)).toBe(true);
      expect(Number.isFinite(p.y)).toBe(true);
    }
  });

  it("layers a chain left to right", () => {
    const laid = layoutDependencyGraph(
      [{ id: "a" }, { id: "b" }, { id: "c" }],
      [edge("e1", "a", "b"), edge("e2", "b", "c")],
    );
    const x = new Map(laid.map((p) => [p.id, p.x]));
    expect(x.get("a")!).toBeLessThan(x.get("b")!);
    expect(x.get("b")!).toBeLessThan(x.get("c")!);
  });

  it("puts two branches of one root on the same rank", () => {
    const laid = layoutDependencyGraph(
      [{ id: "root" }, { id: "left" }, { id: "right" }],
      [edge("e1", "root", "left"), edge("e2", "root", "right")],
    );
    const byId = new Map(laid.map((p) => [p.id, p]));
    expect(byId.get("root")!.x).toBeLessThan(byId.get("left")!.x);
    expect(byId.get("left")!.x).toBeCloseTo(byId.get("right")!.x);
    expect(byId.get("left")!.y).not.toBe(byId.get("right")!.y);
  });

  it("ignores edges with an endpoint outside the node set", () => {
    const laid = layoutDependencyGraph(
      [{ id: "a" }],
      [edge("e1", "a", "ghost"), edge("e2", "ghost", "a")],
    );
    expect(laid).toHaveLength(1);
    expect(Number.isFinite(laid[0]!.x)).toBe(true);
  });

  it("ignores duplicate edges", () => {
    const laid = layoutDependencyGraph(
      [{ id: "a" }, { id: "b" }],
      [edge("e1", "a", "b", "duplicate")],
    );
    expect(laid).toHaveLength(2);
  });

  it("resolves a cycle instead of hanging", () => {
    const laid = layoutDependencyGraph(
      [{ id: "a" }, { id: "b" }, { id: "c" }],
      [edge("e1", "a", "b"), edge("e2", "b", "c"), edge("e3", "c", "a")],
    );
    expect(laid).toHaveLength(3);
    for (const p of laid) {
      expect(Number.isFinite(p.x)).toBe(true);
      expect(Number.isFinite(p.y)).toBe(true);
    }
  });

  it("keeps related edges directional like blocks", () => {
    const laid = layoutDependencyGraph(
      [{ id: "a" }, { id: "b" }],
      [edge("e1", "a", "b", "related")],
    );
    const x = new Map(laid.map((p) => [p.id, p.x]));
    expect(x.get("a")!).toBeLessThan(x.get("b")!);
  });
});

describe("graph node constants", () => {
  it("exposes stable node dimensions", () => {
    expect(GRAPH_NODE_WIDTH).toBe(220);
    expect(GRAPH_NODE_HEIGHT).toBe(64);
  });
});

const edge = (id: string, from: string, to: string, type = "blocks"): IssueDependencyEdge => ({
  id,
  from,
  to,
  type,
});
