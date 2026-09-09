// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { OrgDefinition, OrgUnit } from "../types";
import { ORG_CARD_HEIGHT, ORG_CARD_WIDTH, orgLayout } from "./layout";

const unit = (id: string): OrgUnit => ({
  id, name: id, excludes: ["external_effects"], autonomy: "draft", allow: [], deny: [],
  escalation_quota_per_day: 5, members: [], roles: [],
});

const def = (over: Partial<OrgDefinition>): OrgDefinition => ({
  units: [], edges: [], rules: [], committees: [],
  market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 },
  ...over,
});

describe("orgLayout", () => {
  it("puts a root at level 0 and its reports one level below, centred", () => {
    const { nodes, width } = orgLayout(def({
      units: [unit("lead"), unit("dev"), unit("ops")],
      edges: [
        { from: "dev", to: "lead", kind: "reports_to" },
        { from: "ops", to: "lead", kind: "reports_to" },
      ],
    }));
    expect(nodes.map((n) => [n.id, n.level])).toEqual([["lead", 0], ["dev", 1], ["ops", 1]]);
    const lead = nodes.find((n) => n.id === "lead")!;
    const dev = nodes.find((n) => n.id === "dev")!;
    expect(dev.y - lead.y).toBe(ORG_CARD_HEIGHT + 64);
    // The single root sits between the two children it spans.
    expect(lead.x + ORG_CARD_WIDTH / 2).toBe(width / 2);
  });

  it("draws reports_to as a bottom-to-top elbow and a same-level edge as a hop", () => {
    const { edges } = orgLayout(def({
      units: [unit("lead"), unit("dev"), unit("ops")],
      edges: [
        { from: "dev", to: "lead", kind: "reports_to" },
        { from: "ops", to: "lead", kind: "reports_to" },
        { from: "dev", to: "ops", kind: "escalates_to" },
      ],
    }));
    expect(edges[0]?.kind).toBe("reports_to");
    expect(edges[0]?.d).toMatch(/^M [\d.]+ [\d.]+ V [\d.]+ H [\d.]+ V [\d.]+$/);
    expect(edges[2]?.kind).toBe("escalates_to");
    expect(edges[2]?.d).toContain("C");
  });

  it("keeps a reports_to cycle flat instead of recursing", () => {
    const { nodes, height } = orgLayout(def({
      units: [unit("a"), unit("b")],
      edges: [
        { from: "a", to: "b", kind: "reports_to" },
        { from: "b", to: "a", kind: "reports_to" },
      ],
    }));
    expect(nodes.map((n) => n.level)).toEqual([1, 1]);
    expect(height).toBeGreaterThan(ORG_CARD_HEIGHT);
  });

  it("drops an edge whose endpoint is not a unit", () => {
    const { edges } = orgLayout(def({ units: [unit("a")], edges: [{ from: "a", to: "ghost", kind: "reports_to" }] }));
    expect(edges).toEqual([]);
  });
});
