// @vitest-environment node
import i18next from "i18next";
import { describe, expect, it } from "vitest";
import type { OrgAutonomy, OrgEdgeKind, OrgModel } from "@multica/core/types";
import enOrg from "../locales/en/org.json";
import frOrg from "../locales/fr/org.json";
import {
  ORG_AGENT_TRUST_VALUES,
  ORG_APPROVAL_RISK_VALUES,
  ORG_AUTONOMY_VALUES,
  ORG_DECIDER_CLASS_VALUES,
  ORG_EDGE_KIND_VALUES,
  ORG_END_CONDITION_VALUES,
  ORG_MEMBER_ROLE_VALUES,
  ORG_MODEL_VALUES,
  ORG_UNIT_KIND_VALUES,
  orgActivationRequirementText,
  orgAgentTrustLabel,
  orgApprovalRiskLabel,
  orgAutonomyHint,
  orgAutonomyLabel,
  orgCapabilityEnforced,
  orgCapabilityEnforcementLabel,
  orgCapabilityLabel,
  orgDeciderClassLabel,
  orgEdgeKindEffectLabel,
  orgEdgeKindHasEffect,
  orgEdgeKindLabel,
  orgEndConditionLabel,
  orgFormatUsd,
  orgMemberRoleLabel,
  orgModelDescription,
  orgModelLabel,
  orgProblemParams,
  orgProposalBody,
  orgProposalMeasure,
  orgProposalTitle,
  orgSimulateNoteText,
  orgUnitKindLabel,
  orgUsdFromTicks,
} from "./labels";

// Compile-time exhaustiveness: if OrgAutonomy / OrgEdgeKind / OrgModel gains
// (or loses) a member, these literals stop satisfying their Record type and
// the build fails — the runtime tests below then prove every member in the
// literal resolves to a real (non-generic) label, not just that the type
// compiles.
const AUTONOMY_EXHAUSTIVE: Record<OrgAutonomy, true> = { read_only: true, draft: true, approve_payload: true, auto: true };
const EDGE_KIND_EXHAUSTIVE: Record<OrgEdgeKind, true> = { reports_to: true, escalates_to: true, backs_up: true, consults: true };
const MODEL_EXHAUSTIVE: Record<OrgModel, true> = { hierarchy: true, squads: true, matrix: true, circles: true, owner_network: true, taskforce: true, market: true };

function makeT(resource: object) {
  const instance = i18next.createInstance() as unknown as {
    init: (opts: Record<string, unknown>) => void;
    getFixedT: (lng: string, ns: string) => unknown;
  };
  instance.init({
    lng: "en",
    resources: { en: { org: resource } },
    interpolation: { escapeValue: false },
    initAsync: false,
  });
  return instance.getFixedT("en", "org") as Parameters<typeof orgAutonomyLabel>[0];
}

const t = makeT(enOrg);
const tFr = makeT(frOrg);
// Plain key lookup, not the selector form: the selector API is typed against
// the module-augmented resource shape (resources-types.ts), which the plain
// JSON import used for this fixture does not structurally match.
const unknownLabel = (t as unknown as (key: string) => string)("unknown_value");

describe("org labels: autonomy", () => {
  it("labels and hints every known autonomy notch, real English text every time", () => {
    expect(Object.keys(AUTONOMY_EXHAUSTIVE)).toEqual(ORG_AUTONOMY_VALUES);
    for (const v of ORG_AUTONOMY_VALUES) {
      expect(orgAutonomyLabel(t, v)).not.toBe(unknownLabel);
      expect(orgAutonomyLabel(t, v)).not.toBe(v);
      expect(orgAutonomyHint(t, v)).not.toBe("");
    }
  });

  it("falls back to a generic label for a value the server added ahead of this release", () => {
    expect(orgAutonomyLabel(t, "some_future_autonomy")).toBe(unknownLabel);
    expect(orgAutonomyLabel(t, "some_future_autonomy")).not.toBe("some_future_autonomy");
  });
});

describe("org labels: capabilities", () => {
  const KNOWN = ["read", "comment", "propose_plan", "delete", "bill", "refund", "send_external_without_approval", "touch_secrets", "commit_money", "publish", "sign"];

  it("gives every documented verb an infinitive label", () => {
    for (const verb of KNOWN) {
      expect(orgCapabilityLabel(t, verb)).not.toBe(unknownLabel);
      expect(orgCapabilityLabel(t, verb)).not.toBe(verb);
    }
  });

  it("shows a custom verb as typed — it is user text, not a drifting server enum", () => {
    expect(orgCapabilityLabel(t, "merge")).toBe("merge");
  });

  it("only the non-negotiable denies are enforced by the MCP gateway", () => {
    expect(orgCapabilityEnforced("delete")).toBe(true);
    expect(orgCapabilityEnforced("bill")).toBe(true);
    expect(orgCapabilityEnforced("send_external_without_approval")).toBe(true);
    expect(orgCapabilityEnforced("touch_secrets")).toBe(true);
    expect(orgCapabilityEnforced("commit_money")).toBe(true);
    expect(orgCapabilityEnforced("refund")).toBe(false);
    expect(orgCapabilityEnforcementLabel(t, "delete")).not.toBe(orgCapabilityEnforcementLabel(t, "refund"));
  });
});

describe("org labels: unit kind, approval risk, edge kind, agent trust, decider class, end condition, member role", () => {
  it("unit kind", () => {
    for (const v of ORG_UNIT_KIND_VALUES) expect(orgUnitKindLabel(t, v)).not.toBe(unknownLabel);
    expect(orgUnitKindLabel(t, undefined)).toBe(orgUnitKindLabel(t, "unit"));
    expect(orgUnitKindLabel(t, "some_future_kind")).toBe(unknownLabel);
  });

  it("approval risk — empty means unset, not unknown", () => {
    for (const v of ORG_APPROVAL_RISK_VALUES) expect(orgApprovalRiskLabel(t, v)).not.toBe(unknownLabel);
    expect(orgApprovalRiskLabel(t, "")).toBe("");
    expect(orgApprovalRiskLabel(t, undefined)).toBe("");
    expect(orgApprovalRiskLabel(t, "some_future_risk")).toBe(unknownLabel);
  });

  it("edge kind: reports_to and escalates_to affect routing, backs_up and consults are informational only", () => {
    expect(Object.keys(EDGE_KIND_EXHAUSTIVE)).toEqual(ORG_EDGE_KIND_VALUES);
    for (const v of ORG_EDGE_KIND_VALUES) expect(orgEdgeKindLabel(t, v)).not.toBe(unknownLabel);
    expect(orgEdgeKindHasEffect("reports_to")).toBe(true);
    expect(orgEdgeKindHasEffect("escalates_to")).toBe(true);
    expect(orgEdgeKindHasEffect("backs_up")).toBe(false);
    expect(orgEdgeKindHasEffect("consults")).toBe(false);
    expect(orgEdgeKindEffectLabel(t, "reports_to")).not.toBe(orgEdgeKindEffectLabel(t, "backs_up"));
  });

  it("an agent's Trust Dial mode", () => {
    for (const v of ORG_AGENT_TRUST_VALUES) expect(orgAgentTrustLabel(t, v)).not.toBe(unknownLabel);
    expect(orgAgentTrustLabel(t, "some_future_mode")).toBe(unknownLabel);
  });

  it("decider class", () => {
    for (const v of ORG_DECIDER_CLASS_VALUES) expect(orgDeciderClassLabel(t, v)).not.toBe(unknownLabel);
  });

  it("end condition, including the empty string", () => {
    for (const v of ORG_END_CONDITION_VALUES) expect(orgEndConditionLabel(t, v)).not.toBe(unknownLabel);
    expect(orgEndConditionLabel(t, "")).not.toBe("");
  });

  it("member role", () => {
    for (const v of ORG_MEMBER_ROLE_VALUES) expect(orgMemberRoleLabel(t, v)).not.toBe(unknownLabel);
  });
});

describe("org labels: model", () => {
  it("labels and describes all seven models, from real product behaviour", () => {
    expect(Object.keys(MODEL_EXHAUSTIVE)).toEqual(ORG_MODEL_VALUES);
    for (const v of ORG_MODEL_VALUES) {
      expect(orgModelLabel(t, v)).not.toBe(unknownLabel);
      expect(orgModelDescription(t, v)).not.toBe("");
    }
  });
});

describe("org labels: budget ticks to USD", () => {
  it("1,000,000 ticks is $1 — the market/unit budget scale, not the task-cost scale", () => {
    expect(orgUsdFromTicks(5_000_000)).toBe(5);
    expect(orgFormatUsd(5_000_000, "en-US")).toBe("$5.00");
    expect(orgFormatUsd(1_500_000, "en-US")).toBe("$1.50");
  });
});

describe("org labels: validation problem params", () => {
  it("translates the raw autonomy and trust values before interpolation", () => {
    const params = orgProblemParams(t, { code: "autonomy_over_trust", params: { unit: "Front line", autonomy: "auto", trust: "propose", agent: "Mika" } });
    expect(params.autonomy).toBe(orgAutonomyLabel(t, "auto"));
    expect(params.trust).toBe(orgAgentTrustLabel(t, "propose"));
    expect(params.unit).toBe("Front line");
  });

  it("translates each decider class in a joined list", () => {
    const params = orgProblemParams(t, { code: "deciders_missing", params: { unit: "Finance", classes: "money, outbound_data" } });
    expect(params.classes).toBe(`${orgDeciderClassLabel(t, "money")}, ${orgDeciderClassLabel(t, "outbound_data")}`);
  });

  it("passes params through unchanged for any other problem code", () => {
    const params = orgProblemParams(t, { code: "market_price_cap", params: { foo: "bar" } });
    expect(params).toEqual({ foo: "bar" });
  });
});

describe("org labels: server-coded text (simulate notes, proposals, activation requirements)", () => {
  it("translates a known simulate-note code and interpolates its params", () => {
    const text = orgSimulateNoteText(t, "unit “Front line” has no human owner, so it never receives work.", "unit_no_owner", { unit: "Front line" });
    expect(text).toContain("Front line");
    expect(text).not.toContain("unit_no_owner");
  });

  it("falls back to the server's English text when the code is missing or unknown", () => {
    expect(orgSimulateNoteText(t, "server sentence", undefined)).toBe("server sentence");
    expect(orgSimulateNoteText(t, "server sentence", "some_future_code")).toBe("server sentence");
  });

  it("translates a known proposal's title, body and measure", () => {
    const title = orgProposalTitle(t, "fallback title", "escalations", { unit: "Front line" });
    const body = orgProposalBody(t, "fallback body", "escalations");
    const measure = orgProposalMeasure(t, "fallback measure", "escalations", { count: 12, window_days: 7, quota: 5 });
    expect(title).toContain("Front line");
    expect(body).not.toBe("fallback body");
    expect(measure).toContain("12");
  });

  it("translates an activation requirement code", () => {
    expect(orgActivationRequirementText(t, "human owner", "owner")).not.toBe("human owner");
    expect(orgActivationRequirementText(t, "human owner", undefined)).toBe("human owner");
  });
});

describe("org labels: French renders real French, not English fallthrough", () => {
  it("autonomy, model and capability read in French", () => {
    expect(orgAutonomyLabel(tFr, "auto")).toBe("Agit seule dans ses limites");
    expect(orgModelLabel(tFr, "taskforce")).toBe("Task force temporaire");
    expect(orgCapabilityLabel(tFr, "delete")).toBe("Supprimer");
  });
});
