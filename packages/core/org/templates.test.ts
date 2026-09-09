// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  ORG_BUSINESS_TEMPLATES,
  buildOrgDefinition,
  orgDefaultAssignments,
  orgModelFromAnswers,
  orgRoutingWords,
  orgStructureName,
  orgTemplateRoot,
  pickOrgTemplate,
  type OrgAnswers,
} from "./templates";

// Canonical layer for the wizard's pure logic; org-wizard.test.tsx keeps the
// walkthrough, not this matrix.

describe("orgRoutingWords", () => {
  it("keeps the words that can route and drops the function words", () => {
    expect(orgRoutingWords("Nous devons répondre aux tickets de nos clients en moins d'un jour")).toEqual([
      "devons", "répondre", "tickets", "clients", "moins", "jour",
    ]);
  });

  it("deduplicates, ignores punctuation and caps the list", () => {
    expect(orgRoutingWords("facture, facture; facture — budget budget paiement", 2)).toEqual(["facture", "budget"]);
  });

  it("returns nothing for a sentence made of function words", () => {
    expect(orgRoutingWords("nous devons")).toEqual(["devons"]);
    expect(orgRoutingWords("de la    ")).toEqual([]);
  });
});

describe("orgStructureName", () => {
  it("names the structure after the first words of the sentence", () => {
    expect(orgStructureName("répondre aux tickets clients du support en une journée")).toBe("Répondre Tickets Clients Support");
  });

  it("is empty when the sentence carries no routing word, so the server names it", () => {
    expect(orgStructureName("de la")).toBe("");
  });
});

describe("orgModelFromAnswers", () => {
  const answers = (over: Partial<OrgAnswers>): OrgAnswers => ({ decider: "one_person", hasEnd: false, compete: false, ...over });

  it("2a alone: one person decides, or the owner of the topic does", () => {
    expect(orgModelFromAnswers(answers({ decider: "one_person" })).model).toBe("hierarchy");
    expect(orgModelFromAnswers(answers({ decider: "topic_owner" })).model).toBe("owner_network");
  });

  it("2b: each team decides, by project, by role, or both", () => {
    expect(orgModelFromAnswers(answers({ decider: "each_team", teamShape: "project" })).model).toBe("squads");
    expect(orgModelFromAnswers(answers({ decider: "each_team", teamShape: "role" })).model).toBe("circles");
    expect(orgModelFromAnswers(answers({ decider: "each_team", teamShape: "both" })).model).toBe("matrix");
  });

  it("2c lays a task force over the base, which moves onto the units", () => {
    expect(orgModelFromAnswers(answers({ decider: "each_team", teamShape: "role", hasEnd: true }))).toEqual({
      model: "taskforce",
      unitModel: "circles",
      base: "circles",
    });
  });

  it("2d turns the structure into a market, and stays on the units under a task force", () => {
    expect(orgModelFromAnswers(answers({ compete: true }))).toEqual({ model: "market", base: "hierarchy" });
    expect(orgModelFromAnswers(answers({ hasEnd: true, compete: true }))).toEqual({
      model: "taskforce",
      unitModel: "market",
      base: "hierarchy",
    });
  });
});

describe("pickOrgTemplate", () => {
  it.each([
    ["répondre aux tickets de nos clients", "support"],
    ["Suivre Facture Clients", "finance"],
    ["campagne clients", "agency"],
    ["gérer les dossiers du cabinet et les mandats", "practice"],
    ["produire les campagnes de l'agence", "agency"],
    ["suivre les factures et le budget", "finance"],
    ["mener une étude et analyser les données", "research"],
    ["avancer sur nos sujets", "generic"],
  ])("picks %s → %s", (purpose, key) => {
    expect(pickOrgTemplate(purpose).key).toBe(key);
  });

  it("every template has exactly one root and one primary unit", () => {
    for (const tpl of ORG_BUSINESS_TEMPLATES) {
      expect(tpl.units.filter((u) => u.root === true)).toHaveLength(1);
      expect(tpl.units.filter((u) => u.primary === true)).toHaveLength(1);
    }
  });
});

describe("orgDefaultAssignments", () => {
  it("puts the humans on the root and spreads the agents over the working units", () => {
    const tpl = pickOrgTemplate("tickets clients");
    const assignments = orgDefaultAssignments(tpl, [
      { type: "member", id: "u-1" },
      { type: "agent", id: "a-1" },
      { type: "agent", id: "a-2" },
    ]);
    expect(assignments["support-lead"]).toEqual(["member:u-1"]);
    expect(assignments["front-line"]).toEqual(["agent:a-1"]);
    expect(assignments["escalation"]).toEqual(["agent:a-2"]);
  });
});

describe("buildOrgDefinition", () => {
  const tpl = pickOrgTemplate("répondre aux tickets de nos clients");
  const routingWords = orgRoutingWords("répondre aux tickets de nos clients");
  const def = buildOrgDefinition({
    template: tpl,
    shape: orgModelFromAnswers({ decider: "one_person", hasEnd: false, compete: false }),
    assignments: orgDefaultAssignments(tpl, [{ type: "member", id: "u-1" }, { type: "agent", id: "a-1" }]),
    ownerId: "u-1",
    routingWords,
  });

  it("gives the routing words to the primary unit's rule, which is what orgMatchUnit reads", () => {
    const rule = def.rules.find((r) => r.target_unit === "front-line");
    expect(rule?.priority).toBe(1);
    expect(rule?.keywords).toEqual(expect.arrayContaining(["support", "ticket", "répondre", "clients"]));
  });

  it("repeats them on the unit's role, the only unit-level field the circles model routes on", () => {
    const unit = def.units.find((u) => u.id === "front-line");
    expect(unit?.roles[0]?.keywords).toEqual(def.rules.find((r) => r.target_unit === "front-line")?.keywords);
  });

  it("lets the root take what no rule names, at a lower priority", () => {
    const root = def.rules.find((r) => r.target_unit === orgTemplateRoot(tpl).id);
    expect(root?.paths).toEqual(["*"]);
    expect(root?.priority).toBe(0);
  });

  it("gives every unit an owner, a mission and the assigned members", () => {
    expect(def.units.every((u) => u.owner_id === "u-1" && (u.mission ?? "") !== "")).toBe(true);
    expect(def.units.find((u) => u.id === "support-lead")?.members).toEqual([{ type: "member", id: "u-1" }]);
    expect(def.units.find((u) => u.id === "front-line")?.members).toEqual([{ type: "agent", id: "a-1" }]);
  });

  it("reports and escalates every working unit to the root", () => {
    expect(def.edges).toEqual(
      expect.arrayContaining([
        { from: "front-line", to: "support-lead", kind: "reports_to" },
        { from: "front-line", to: "support-lead", kind: "escalates_to" },
      ]),
    );
    expect(def.edges.some((e) => e.from === "support-lead")).toBe(false);
  });

  it("carries the unit model of an overlay onto the working units only", () => {
    const overlaid = buildOrgDefinition({
      template: tpl,
      shape: orgModelFromAnswers({ decider: "one_person", hasEnd: true, compete: true }),
      assignments: {},
      ownerId: "u-1",
      routingWords: [],
    });
    expect(overlaid.units.find((u) => u.id === "support-lead")?.model).toBeUndefined();
    expect(overlaid.units.find((u) => u.id === "front-line")?.model).toBe("market");
    expect(overlaid.market.price_cap_usd_ticks).toBeGreaterThan(0);
  });
});


it("keeps peer squads flat and their human coordination unit eligible without an agent", () => {
  const template = pickOrgTemplate("tickets clients");
  const definition = buildOrgDefinition({ template, shape: orgModelFromAnswers({ decider: "each_team", teamShape: "project", hasEnd: false, compete: false }), assignments: {}, ownerId: "u-1", routingWords: [] });
  expect(definition.edges.some(e => e.kind === "reports_to")).toBe(false);
  expect(definition.units.find(u => u.id === orgTemplateRoot(template).id)?.model).toBe("owner_network");
  expect(definition.edges.some(e => e.kind === "escalates_to")).toBe(true);
});
