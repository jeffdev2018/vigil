// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { OrgDefinition, OrgUnit } from "../types";
import { addOrgMembers, removeOrgMember, moveOrgMember, orgLayout, orgWouldCycle, removeOrgUnit, parseEditableOrgDefinition, orgDefinitionChanges } from "./editor";
const unit = (id: string): OrgUnit => ({ id, name: id, autonomy: "draft", members: [], roles: [], allow: [], deny: [], excludes: [], escalation_quota_per_day: 5 });
const def: OrgDefinition = { units: [unit("a"), unit("b"), unit("c")], edges: [{ from: "b", to: "a", kind: "reports_to" }, { from: "c", to: "b", kind: "reports_to" }], rules: [{ id: "r", target_unit: "b", priority: 0 }], committees: [{ decision_type: "review", unit_ids: ["a", "b"], quorum: 2, max_rounds: 1 }], market: { price_cap_usd_ticks: 0, min_offers: 2, offers_per_agent_per_day: 5 } };
describe("organization editing", () => {
  it("puts parents above children and rejects direct and indirect reporting cycles", () => {
    const nodes = orgLayout(def).nodes;
    expect(nodes.find(n => n.unit.id === "a")!.y).toBeLessThan(nodes.find(n => n.unit.id === "c")!.y);
    expect(orgWouldCycle(def, "a", "c")).toBe(true);
    expect(orgWouldCycle(def, "a", "a")).toBe(true);
    expect(orgWouldCycle(def, "c", "a")).toBe(false);
    expect(orgLayout({ ...def, edges: [...def.edges, { from: "a", to: "c", kind: "reports_to" }] }).nodes).toHaveLength(3);
  });
  it("rejects malformed JSON without silently dropping teams and preserves advanced properties", () => {
    expect(parseEditableOrgDefinition(JSON.stringify({ ...def, units: [null] }))).toHaveProperty("error");
    expect(parseEditableOrgDefinition(JSON.stringify({ ...def, edges: [{ from: "a", to: "b", kind: "unknown" }] }))).toHaveProperty("error");
    const advanced = { ...def, units: [{ ...def.units[0], human_approval: true, deciders: { publish: "owner" } }] };
    expect(parseEditableOrgDefinition(JSON.stringify(advanced))).toEqual({ def: advanced });
    expect(orgDefinitionChanges(def, { ...def, units: [unit("a")] })).toHaveLength(2);
  });
  it("refuses a removal that would lower a quorum, and removes other references without changing decision thresholds", () => {
    expect(removeOrgUnit(def, "b")).toBe(def);
    const next = removeOrgUnit({ ...def, committees: [{ ...def.committees[0]!, quorum: 1 }] }, "b");
    expect(next.edges).toEqual([]);
    expect(next.rules).toEqual([]);
    expect(next.committees[0]).toMatchObject({ unit_ids: ["a"], quorum: 1 });
    expect(def.units).toHaveLength(3);
    expect(def.committees[0]!.quorum).toBe(2);
  });
  it("adds an existing agent only to the selected team, deduplicates membership and keeps identity separate from team removal", () => {
    const actor = { id: "agent-1", type: "agent" as const };
    const next = addOrgMembers(def, "b", [actor, actor]);
    expect(next.units).toHaveLength(3);
    expect(next.units[1]!.members).toEqual([actor]);
    expect(next.edges).toEqual(def.edges);
    expect(addOrgMembers(next, "missing", [actor]).units).toEqual(next.units);
    const removed = removeOrgMember(next, "b", actor);
    expect(removed.units).toHaveLength(3);
    expect(removed.units[1]!.members).toEqual([]);
  });
  it("moves a membership without carrying its old team's role or routing responsibility", () => {
    const actor = { id: "agent-1", type: "agent" as const, role: "lead", role_id: "local-role" };
    const before = addOrgMembers(def, "b", [actor]);
    const next = moveOrgMember(before, "b", "c", actor);
    expect(next.units[1]!.members).toEqual([]);
    expect(next.units[2]!.members).toEqual([{ type: "agent", id: "agent-1" }]);
    expect(moveOrgMember(before, "b", "missing", actor)).toBe(before);
  });
  it("lays out all matrix parents independently of edge order and includes relation-only revisions", () => {
    const matrix = { ...def, edges: [...def.edges, { from: "c", to: "a", kind: "reports_to" as const }] };
    expect(orgLayout(matrix)).toEqual(orgLayout({ ...matrix, edges: [...matrix.edges].reverse() }));
    expect(orgDefinitionChanges(def, matrix)).toEqual([expect.objectContaining({ section: "edges" })]);
  });
});
