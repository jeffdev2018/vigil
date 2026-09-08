// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { OrgDefinition, OrgUnit } from "../types";
import { ORG_PERSON_WIDTH, orgAddTeammate, orgPeople, orgPeopleLayout } from "./people";

const unit = (over: Partial<OrgUnit>): OrgUnit => ({
  id: "u", name: "Unit", excludes: [], autonomy: "draft", allow: [], deny: [], escalation_quota_per_day: 5, members: [], roles: [], ...over,
});
const def = (over: Partial<OrgDefinition>): OrgDefinition => ({
  units: [], edges: [], rules: [], committees: [], market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 }, ...over,
});

const support = def({
  units: [
    unit({ id: "dir", name: "Direction", owner_id: "jeff", members: [{ type: "member", id: "jeff", role: "Propriétaire" }] }),
    unit({
      id: "support", name: "Support", owner_id: "lea", roles: [{ id: "n1", name: "Support N1" }],
      members: [{ type: "agent", id: "nova", role_id: "n1" }, { type: "member", id: "lea" }, { type: "agent", id: "vera" }],
    }),
    unit({ id: "empty", name: "Shell" }),
    unit({ id: "billing", name: "Facturation", deciders: { money: "sable" }, members: [{ type: "agent", id: "sable" }] }),
  ],
  edges: [
    { from: "support", to: "dir", kind: "reports_to" },
    { from: "empty", to: "dir", kind: "reports_to" },
    { from: "billing", to: "empty", kind: "reports_to" },
  ],
});

describe("orgPeople", () => {
  it("makes the owner lead, hands reports to the lead, and skips an empty unit on the way up", () => {
    const people = orgPeople(support);
    const by = Object.fromEntries(people.map((p) => [p.key, p]));
    expect(by["dir/member:jeff"]).toMatchObject({ lead: true, reportsTo: null, title: "Propriétaire" });
    expect(by["support/member:lea"]).toMatchObject({ lead: true, reportsTo: "dir/member:jeff", title: "Support" });
    expect(by["support/agent:nova"]).toMatchObject({ lead: false, reportsTo: "support/member:lea", title: "Support N1" });
    expect(by["support/agent:vera"]?.title).toBe("Support");
    // Nobody in "Shell": billing's decider reports straight to the director.
    expect(by["billing/agent:sable"]).toMatchObject({ lead: true, reportsTo: "dir/member:jeff" });
    expect(people.some((p) => p.unitId === "empty")).toBe(false);
  });

  it("falls back to the first human, then the first member, when nothing names a lead", () => {
    const d = def({ units: [unit({ id: "a", members: [{ type: "agent", id: "x" }, { type: "member", id: "h" }] }), unit({ id: "b", members: [{ type: "agent", id: "y" }] })] });
    const leads = orgPeople(d).filter((p) => p.lead).map((p) => p.key);
    expect(leads).toEqual(["a/member:h", "b/agent:y"]);
  });
});

describe("orgPeopleLayout", () => {
  it("centres a manager over its reports and draws one orthogonal line per report", () => {
    const layout = orgPeopleLayout(orgPeople(support));
    const at = (key: string) => layout.nodes.find((n) => n.key === key)!;
    const lea = at("support/member:lea");
    const nova = at("support/agent:nova");
    const vera = at("support/agent:vera");
    expect(nova.y).toBe(vera.y);
    expect(nova.y).toBeGreaterThan(lea.y);
    expect(lea.x + lea.width / 2).toBeCloseTo((nova.x + vera.x + ORG_PERSON_WIDTH) / 2);
    expect(layout.edges).toHaveLength(orgPeople(support).length - 1);
    expect(layout.edges.find((e) => e.to === "support/agent:nova")?.d).toMatch(/^M [\d.]+ [\d.]+ V [\d.]+ H [\d.]+ V [\d.]+$/);
    expect(layout.width).toBeGreaterThan(0);
    expect(layout.height).toBeGreaterThan(0);
  });

  it("still lays out a manager loop instead of hanging", () => {
    const people = orgPeople(def({
      units: [unit({ id: "a", members: [{ type: "member", id: "1" }] }), unit({ id: "b", members: [{ type: "member", id: "2" }] })],
      edges: [{ from: "a", to: "b", kind: "reports_to" }, { from: "b", to: "a", kind: "reports_to" }],
    }));
    const layout = orgPeopleLayout(people);
    expect(layout.nodes).toHaveLength(2);
  });
});

describe("orgAddTeammate", () => {
  it("joins the manager's unit, and opens a unit of its own at the top when there is no manager", () => {
    const joined = orgAddTeammate(support, "support/member:lea", { type: "agent", id: "kimi", role: "Support N2" }, "Support N2");
    expect(joined.units.find((u) => u.id === "support")?.members.at(-1)).toEqual({ type: "agent", id: "kimi", role: "Support N2" });
    const first = orgAddTeammate(def({}), null, { type: "member", id: "jeff", role: "Direction" }, "Direction");
    expect(first.units).toHaveLength(1);
    expect(first.units[0]).toMatchObject({ id: "direction", name: "Direction", owner_id: "jeff", members: [{ type: "member", id: "jeff" }] });
    const again = orgAddTeammate(first, null, { type: "agent", id: "a" }, "Direction");
    expect(again.units.map((u) => u.id)).toEqual(["direction", "direction-2"]);
  });

  it("only updates the title when the person is already in that unit", () => {
    const d = orgAddTeammate(support, "support/member:lea", { type: "agent", id: "nova", role: "Chef N1" }, "x");
    const members = d.units.find((u) => u.id === "support")?.members ?? [];
    expect(members).toHaveLength(3);
    expect(members.find((m) => m.id === "nova")?.role).toBe("Chef N1");
  });
});
