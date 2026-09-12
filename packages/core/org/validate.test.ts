// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { OrgDefinition, OrgUnit } from "../types";
import {
  ORG_NON_NEGOTIABLE_DENY,
  orgEffectiveModel,
  orgUnitBreaksRuleOfTwo,
  orgUnitProperties,
  validateOrgDefinition,
} from "./validate";

// Canonical matrix for the invariants the canvas reproduces from the server's
// validateOrg (server/internal/handler/org.go). The component suite only checks
// that a message reaches the card.

const unit = (over: Partial<OrgUnit>): OrgUnit => ({
  id: "u", name: "Unit", excludes: ["external_effects"], autonomy: "draft", allow: [], deny: [],
  escalation_quota_per_day: 5, members: [], roles: [], ...over,
});

const def = (over: Partial<OrgDefinition>): OrgDefinition => ({
  units: [], edges: [], rules: [], committees: [],
  market: { price_cap_usd_ticks: 0, offers_per_agent_per_day: 0, min_offers: 0 },
  ...over,
});

const codes = (d: OrgDefinition, ctx: Parameters<typeof validateOrgDefinition>[1]) =>
  validateOrgDefinition(d, ctx).map((p) => p.code);

describe("orgUnitProperties", () => {
  it("is every property the unit does not exclude", () => {
    expect(orgUnitProperties({ excludes: [] })).toEqual(["untrusted_input", "sensitive_data", "external_effects"]);
    expect(orgUnitProperties({ excludes: ["sensitive_data", "external_effects"] })).toEqual(["untrusted_input"]);
  });

  it("breaks the Rule of Two only when the three cumulate without human approval", () => {
    expect(orgUnitBreaksRuleOfTwo({ excludes: [] })).toBe(true);
    expect(orgUnitBreaksRuleOfTwo({ excludes: [], human_approval: true })).toBe(false);
    expect(orgUnitBreaksRuleOfTwo({ excludes: ["sensitive_data"] })).toBe(false);
  });
});

describe("orgEffectiveModel", () => {
  it("takes the unit's own model, else the nearest ancestor's, else the structure's", () => {
    const d = def({
      units: [unit({ id: "root", model: "squads" }), unit({ id: "mid" }), unit({ id: "leaf", model: "market" })],
      edges: [
        { from: "mid", to: "root", kind: "reports_to" },
        { from: "leaf", to: "mid", kind: "reports_to" },
      ],
    });
    expect(orgEffectiveModel(d, "root", "hierarchy")).toBe("squads");
    expect(orgEffectiveModel(d, "mid", "hierarchy")).toBe("squads");
    expect(orgEffectiveModel(d, "leaf", "hierarchy")).toBe("market");
  });

  it("stops on a reports_to cycle instead of looping", () => {
    const d = def({
      units: [unit({ id: "a" }), unit({ id: "b" })],
      edges: [
        { from: "a", to: "b", kind: "reports_to" },
        { from: "b", to: "a", kind: "reports_to" },
      ],
    });
    expect(orgEffectiveModel(d, "a", "circles")).toBe("circles");
  });
});

describe("validateOrgDefinition", () => {
  it("accepts a one-root hierarchy with excluded external effects", () => {
    const d = def({
      units: [unit({ id: "lead" }), unit({ id: "dev" })],
      edges: [{ from: "dev", to: "lead", kind: "reports_to" }],
    });
    expect(validateOrgDefinition(d, { model: "hierarchy" })).toEqual([]);
  });

  it("wants exactly one root in a hierarchy, and does not care in other models", () => {
    const two = def({ units: [unit({ id: "a" }), unit({ id: "b" })] });
    expect(codes(two, { model: "hierarchy" })).toEqual(["hierarchy_roots"]);
    expect(validateOrgDefinition(two, { model: "hierarchy" })[0]?.params).toEqual({ count: 2 });
    expect(codes(two, { model: "owner_network" })).toEqual([]);
  });

  it("caps a unit's autonomy at the Trust Dial of every agent it holds", () => {
    const d = def({ units: [unit({ id: "a", autonomy: "auto", members: [{ type: "agent", id: "ag-1" }] })] });
    expect(codes(d, { model: "owner_network", agentTrust: { "ag-1": "approval" }, agentName: { "ag-1": "Mika" } })).toEqual([
      "autonomy_over_trust",
    ]);
    expect(validateOrgDefinition(d, { model: "owner_network", agentTrust: { "ag-1": "autonomous" } })).toEqual([]);
    // An agent the client has not loaded is the server's business, not ours.
    expect(validateOrgDefinition(d, { model: "owner_network" })).toEqual([]);
  });

  it("names the Rule of Two on the unit and on the path", () => {
    const both = def({
      units: [unit({ id: "a", excludes: ["sensitive_data"] }), unit({ id: "b", excludes: ["untrusted_input"] })],
      edges: [{ from: "a", to: "b", kind: "consults" }],
    });
    expect(codes(both, { model: "owner_network" })).toEqual(["deciders_missing", "deciders_missing", "rule_of_two_edge"]);
    const one = def({ units: [unit({ id: "a", excludes: [], deciders: { money: "m", outbound_data: "m", external_message: "m" } })] });
    expect(codes(one, { model: "owner_network" })).toEqual(["rule_of_two_unit"]);
    // The edge's own human_approval clears the path.
    both.edges[0]!.human_approval = true;
    expect(codes(both, { model: "owner_network" })).toEqual(["deciders_missing", "deciders_missing"]);
  });

  it("requires a decider per class as soon as a unit keeps external effects", () => {
    const d = def({ units: [unit({ id: "a", excludes: ["untrusted_input"], deciders: { money: "u-1" } })] });
    const [problem] = validateOrgDefinition(d, { model: "owner_network" });
    expect(problem?.code).toBe("deciders_missing");
    expect(problem?.params.classes).toBe("outbound_data, external_message");
  });

  it("rejects duplicate ids, nameless units and taskforce as a unit model", () => {
    const d = def({ units: [unit({ id: "a" }), unit({ id: "a" }), unit({ id: "c", name: " " }), unit({ id: "d", model: "taskforce" })] });
    expect(codes(d, { model: "owner_network" })).toEqual(["duplicate_unit_id", "unit_name_required", "taskforce_unit_model"]);
  });

  it("demands termination from a committee and a price cap from a market", () => {
    const committee = def({
      units: [unit({ id: "a" })],
      committees: [{ decision_type: "release", unit_ids: ["a"], quorum: 2, max_rounds: 1 }],
    });
    expect(codes(committee, { model: "owner_network" })).toEqual(["committee_termination"]);
    const market = def({ units: [unit({ id: "a" })] });
    expect(codes(market, { model: "market" })).toEqual(["market_price_cap"]);
    market.market.price_cap_usd_ticks = 5_000_000;
    expect(codes(market, { model: "market" })).toEqual([]);
  });

  it("applies the per-model shape to the model the unit actually runs under", () => {
    const squads = def({ units: [unit({ id: "a" })] });
    expect(codes(squads, { model: "squads" })).toEqual(["squads_needs_agents"]);
    expect(codes(def({ units: [unit({ id: "a" })] }), { model: "circles" })).toEqual(["circles_needs_role"]);
    const nested = def({
      units: [unit({ id: "root" }), unit({ id: "leaf", model: "circles" })],
      edges: [{ from: "leaf", to: "root", kind: "reports_to" }],
    });
    expect(validateOrgDefinition(nested, { model: "hierarchy" }).map((p) => p.unit_id)).toEqual(["leaf"]);
  });
});

describe("ORG_NON_NEGOTIABLE_DENY", () => {
  it("carries the five verbs the server merges into every unit", () => {
    expect([...ORG_NON_NEGOTIABLE_DENY]).toEqual(["delete", "bill", "send_external_without_approval", "touch_secrets", "commit_money"]);
  });
});
