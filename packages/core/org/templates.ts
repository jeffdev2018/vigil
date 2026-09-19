import type { OrgAutonomy, OrgDefinition, OrgMember, OrgModel, OrgUnit } from "../types";

// The four-step wizard (K75): what a small team answers about itself, turned
// into a definition the server accepts.
//
// Routing is the constraint that shapes everything here. The server's
// `orgMatchUnit` (server/internal/handler/org.go) reads exactly three things:
// a rule's labels, a rule's keywords matched against the issue title and
// description, and a rule's path globs — plus, under the circles model, the
// keywords of a unit's roles. A unit has no keyword field of its own. So the
// "routing words" the wizard collects are written twice: into one rule per
// unit, and into that unit's role. Anything else would be a field the router
// never reads.

/** The purpose sentence of step 1, and a unit's mission, share this ceiling
 *  (mirrors `orgMissionMaxRunes` in server/internal/handler/org.go). */
export const ORG_PURPOSE_MAX = 240;

/** Every unit the wizard writes excludes external effects, so a fresh draft
 *  never lands on the Rule of Two or on the missing-decider refusal. */
const WIZARD_EXCLUDES: OrgUnit["excludes"] = ["external_effects"];
const WIZARD_ALLOW = ["read", "comment", "propose_plan"];
const WIZARD_ESCALATION_QUOTA = 5;
/** Mirrors the price cap the server's market template ships with. */
const WIZARD_PRICE_CAP_USD_TICKS = 5_000_000;

// --- step 1: the sentence ----------------------------------------------------

// Function words in the five UI languages plus the verbs a purpose sentence
// always carries. Dropping them keeps the routing words specific enough that a
// rule does not match every issue.
const STOP_WORDS = new Set([
  "and", "the", "for", "our", "with", "that", "this", "from", "into", "team", "teams", "work", "want", "need", "must", "should", "make", "help", "keep", "have", "does", "doing", "all", "any", "who", "what", "when", "each", "every", "their",
  "les", "des", "une", "un", "le", "la", "de", "du", "dans", "pour", "avec", "que", "qui", "quoi", "sur", "par", "aux", "est", "sont", "nous", "notre", "nos", "cette", "ce", "ces", "son", "sa", "ses", "leur", "leurs", "faire", "doit", "doivent", "veut", "veulent", "équipe", "equipe", "équipes", "equipes", "travail", "tout", "tous", "toute", "toutes", "plus", "bien", "sans",
]);

/**
 * The words of the purpose sentence that can route an issue: lowercase, at
 * least three characters, no function words, deduplicated, capped so a single
 * rule stays readable.
 */
export function orgRoutingWords(purpose: string, max = 8): string[] {
  const out: string[] = [];
  for (const raw of purpose.toLowerCase().split(/[^\p{L}\p{N}]+/u)) {
    const word = raw.trim();
    if (word.length < 3 || STOP_WORDS.has(word) || out.includes(word)) continue;
    out.push(word);
    if (out.length >= max) break;
  }
  return out;
}

/**
 * A name for the structure, built from the same words. Empty when the sentence
 * carries none — the server then names the structure after its model.
 */
export function orgStructureName(purpose: string): string {
  return orgRoutingWords(purpose, 4)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

// --- step 2: the decision table ---------------------------------------------

/** 2a — "when a request arrives, who decides in the end?" */
export type OrgDecider = "one_person" | "each_team" | "topic_owner";
/** 2b — "teams by project, by role, or both?" */
export type OrgTeamShape = "project" | "role" | "both";

export interface OrgAnswers {
  decider: OrgDecider;
  /** Only asked when `decider` is "each_team". */
  teamShape?: OrgTeamShape;
  /** 2c — a mission with an end. */
  hasEnd: boolean;
  /** 2d — put the agents in competition. */
  compete: boolean;
}

export interface OrgShape {
  /** The model of the structure itself. */
  model: OrgModel;
  /** The model each unit runs under, when the structure's own model is an
   *  overlay that would otherwise erase the answers to 2a/2b. */
  unitModel?: OrgModel;
  /** What 2a and 2b alone answered, before the two overlays. */
  base: OrgModel;
}

const BASE_BY_TEAM_SHAPE: Record<OrgTeamShape, OrgModel> = {
  project: "squads",
  role: "circles",
  both: "matrix",
};

/**
 * The decision table, in one place: 2a and 2b name the base model, 2c and 2d
 * lay an overlay over it. A task force is a structure model the units cannot
 * carry, so the base moves onto the units; a market is how a unit takes work,
 * so it moves onto the units when a task force already holds the structure.
 */
export function orgModelFromAnswers(a: OrgAnswers): OrgShape {
  const base: OrgModel =
    a.decider === "one_person" ? "hierarchy"
      : a.decider === "topic_owner" ? "owner_network"
        : BASE_BY_TEAM_SHAPE[a.teamShape ?? "project"];
  if (a.hasEnd) return { model: "taskforce", unitModel: a.compete ? "market" : base, base };
  if (a.compete) return { model: "market", base };
  return { model: base, base };
}

// --- step 3: the business templates -----------------------------------------

export interface OrgTemplateUnit {
  id: string;
  name: string;
  mission: string;
  /** Written into this unit's rule and its role — the two places the router reads. */
  keywords: string[];
  autonomy: OrgAutonomy;
  /** On top of the five refusals the server merges into every unit. */
  deny: string[];
  /** Everything reports to and escalates to the root; the root takes what no rule names. */
  root?: boolean;
  /** The unit the purpose sentence describes: it inherits its routing words. */
  primary?: boolean;
}

export type OrgTemplateKey = "support" | "practice" | "agency" | "finance" | "research" | "generic";

export interface OrgBusinessTemplate {
  key: OrgTemplateKey;
  /** Words in the purpose sentence that pick this template. */
  match: string[];
  units: OrgTemplateUnit[];
}

// English defaults for pure builders. The wizard localizes team names and
// missions before writing the definition; ids and routing keywords stay stable.
export const ORG_BUSINESS_TEMPLATES: OrgBusinessTemplate[] = [
  {
    key: "support",
    match: ["support", "ticket", "tickets", "helpdesk", "sav", "réclamation", "reclamation", "assistance", "hotline", "remboursement", "refund"],
    units: [
      { id: "support-lead", name: "Support lead", mission: "Own the queue and answer for the team.", keywords: [], autonomy: "approve_payload", deny: [], root: true },
      { id: "front-line", name: "Front line", mission: "Answer incoming requests the day they arrive.", keywords: ["support", "ticket", "question", "client", "customer", "commande", "order"], autonomy: "draft", deny: ["refund"], primary: true },
      { id: "escalation", name: "Escalation", mission: "Take what the front line cannot close.", keywords: ["bug", "incident", "escalation", "panne", "urgent", "remboursement", "refund"], autonomy: "draft", deny: ["refund"] },
    ],
  },
  {
    key: "practice",
    match: ["cabinet", "avocat", "avocats", "juridique", "legal", "notaire", "comptable", "expertise", "honoraires", "mandat", "dossier", "dossiers", "practice", "firm"],
    units: [
      { id: "partner", name: "Partner", mission: "Answer for every file that leaves the firm.", keywords: [], autonomy: "approve_payload", deny: [], root: true },
      { id: "intake", name: "Intake", mission: "Open a file, gather what it needs, say what it will take.", keywords: ["dossier", "mandat", "case", "contrat", "contract", "devis", "quote"], autonomy: "draft", deny: [], primary: true },
      { id: "review", name: "Review", mission: "Read every file before it goes out.", keywords: ["review", "relecture", "conformité", "compliance", "signature", "validation"], autonomy: "draft", deny: ["sign"] },
    ],
  },
  {
    key: "agency",
    match: ["agence", "agency", "campagne", "campagnes", "campaign", "marketing", "créa", "crea", "creative", "publicité", "brand", "marque", "contenu", "content", "studio"],
    units: [
      { id: "account", name: "Account", mission: "Hold the relationship and arbitrate what ships.", keywords: [], autonomy: "approve_payload", deny: [], root: true },
      { id: "production", name: "Production", mission: "Turn a brief into the deliverable it asks for.", keywords: ["brief", "campagne", "campaign", "design", "contenu", "content", "livrable", "deliverable"], autonomy: "draft", deny: [], primary: true },
      { id: "quality", name: "Quality", mission: "Nothing goes out unread.", keywords: ["review", "relecture", "qualité", "quality", "publication", "publish"], autonomy: "draft", deny: ["publish"] },
    ],
  },
  {
    key: "finance",
    match: ["finance", "financier", "comptabilité", "comptabilite", "compta", "facture", "factures", "facturation", "invoice", "invoicing", "budget", "paiement", "payment", "trésorerie", "tresorerie", "paie", "payroll", "direction", "dirigeant", "board"],
    units: [
      { id: "direction", name: "Direction", mission: "Decide what is spent and answer for it.", keywords: [], autonomy: "approve_payload", deny: [], root: true },
      { id: "finance", name: "Finance", mission: "Prepare every figure; a human signs it off.", keywords: ["facture", "invoice", "paiement", "payment", "budget", "dépense", "expense", "compta", "accounting", "salaire", "payroll"], autonomy: "read_only", deny: ["pay", "transfer_funds"], primary: true },
    ],
  },
  {
    key: "research",
    match: ["recherche", "research", "étude", "etude", "études", "etudes", "study", "analyse", "analysis", "science", "veille", "données", "donnees", "data", "rapport", "report"],
    units: [
      { id: "research-lead", name: "Research lead", mission: "Choose the questions and answer for the conclusions.", keywords: [], autonomy: "approve_payload", deny: [], root: true },
      { id: "research", name: "Research", mission: "Look for the answer and show the sources.", keywords: ["recherche", "research", "étude", "study", "analyse", "analysis", "veille", "données", "data"], autonomy: "draft", deny: [], primary: true },
      { id: "peer-review", name: "Peer review", mission: "Check the sources before a conclusion is published.", keywords: ["review", "relecture", "validation", "source", "citation"], autonomy: "draft", deny: ["publish"] },
    ],
  },
  {
    key: "generic",
    match: [],
    units: [
      { id: "direction", name: "Direction", mission: "Decide and answer for the team.", keywords: [], autonomy: "approve_payload", deny: [], root: true },
      { id: "team", name: "Team", mission: "Do the work the team was set up for.", keywords: [], autonomy: "draft", deny: [], primary: true },
    ],
  },
];

/**
 * The business template the purpose sentence names: the one whose match words
 * appear most often in it, the generic Direction + Team otherwise.
 */
export function pickOrgTemplate(purpose: string): OrgBusinessTemplate {
  const words = new Set(orgRoutingWords(purpose, 64));
  let best = ORG_BUSINESS_TEMPLATES[ORG_BUSINESS_TEMPLATES.length - 1] as OrgBusinessTemplate;
  let bestScore = 0;
  for (const tpl of ORG_BUSINESS_TEMPLATES) {
    const score = tpl.match.filter((m) => words.has(m)).length;
    if (score > bestScore) {
      best = tpl;
      bestScore = score;
    }
  }
  return best;
}

/** The template's root unit — where the workspace owner and the catch-all rule go. */
export function orgTemplateRoot(tpl: OrgBusinessTemplate): OrgTemplateUnit {
  return tpl.units.find((u) => u.root === true) ?? (tpl.units[0] as OrgTemplateUnit);
}

/**
 * Who lands where before the user touches anything: the humans on the root,
 * the agents spread one by one over the working units so no unit that a model
 * requires agents in stays empty.
 */
export function orgDefaultAssignments(
  tpl: OrgBusinessTemplate,
  actors: OrgMember[],
): Record<string, string[]> {
  const root = orgTemplateRoot(tpl);
  const working = tpl.units.filter((u) => u.root !== true);
  const out: Record<string, string[]> = {};
  for (const u of tpl.units) out[u.id] = [];
  let next = 0;
  for (const actor of actors) {
    const key = `${actor.type}:${actor.id}`;
    if (actor.type === "member" || working.length === 0) {
      out[root.id]?.push(key);
      continue;
    }
    out[(working[next % working.length] as OrgTemplateUnit).id]?.push(key);
    next += 1;
  }
  return out;
}

// --- step 4: the definition --------------------------------------------------

export interface OrgBuildParams {
  template: OrgBusinessTemplate;
  shape: OrgShape;
  /** Unit id → `"member:<uuid>"` / `"agent:<uuid>"` keys, as step 3 holds them. */
  assignments: Record<string, string[]>;
  /** The workspace owner: decider of the root and owner of every unit, so the
   *  units are eligible to receive work at all. */
  ownerId: string;
  /** The words of step 1, which land on the primary unit's rule. */
  routingWords: string[];
}

function parseActor(key: string): OrgMember | null {
  const index = key.indexOf(":");
  if (index < 1) return null;
  const type = key.slice(0, index);
  const id = key.slice(index + 1);
  if ((type !== "member" && type !== "agent") || id === "") return null;
  return { type, id };
}

/**
 * The draft the wizard sends to `POST /api/org`: units carrying their mission,
 * their Trust Dial notch and their refusals; one rule per unit holding the
 * routing words; a role per unit holding the same words, which is what the
 * circles model routes on; everything reporting and escalating to the root,
 * which also takes what no rule names.
 */
export function buildOrgDefinition(p: OrgBuildParams): OrgDefinition {
  const root = orgTemplateRoot(p.template);
  const units: OrgUnit[] = [];
  const rules: OrgDefinition["rules"] = [];

  for (const u of p.template.units) {
    const keywords = u.primary === true ? [...new Set([...u.keywords, ...p.routingWords])] : u.keywords;
    const members = (p.assignments[u.id] ?? [])
      .map(parseActor)
      .filter((m): m is OrgMember => m !== null);
    units.push({
      id: u.id,
      name: u.name,
      mission: u.mission,
      owner_id: p.ownerId,
      ...(u.id === root.id && (p.shape.model === "squads" || p.shape.unitModel === "squads")
        ? { model: "owner_network" as const }
        : u.id !== root.id && p.shape.unitModel !== undefined ? { model: p.shape.unitModel } : {}),
      excludes: [...WIZARD_EXCLUDES],
      autonomy: u.autonomy,
      allow: [...WIZARD_ALLOW],
      deny: [...u.deny],
      escalation_quota_per_day: WIZARD_ESCALATION_QUOTA,
      members,
      roles: [{ id: `${u.id}-role`, name: u.name, responsibilities: u.mission, keywords }],
    });
    if (u.id === root.id) {
      rules.push({ id: `r-${u.id}`, paths: ["*"], target_unit: u.id, priority: 0 });
    } else if (keywords.length > 0) {
      rules.push({ id: `r-${u.id}`, keywords, target_unit: u.id, priority: 1 });
    }
  }

  return {
    units,
    edges: p.template.units
      .filter((u) => u.id !== root.id)
      .flatMap((u) => [
        ...(["hierarchy", "matrix"].includes(p.shape.base) ? [{ from: u.id, to: root.id, kind: "reports_to" as const }] : []),
        { from: u.id, to: root.id, kind: "escalates_to" as const },
      ]),
    rules,
    committees: [],
    market: {
      price_cap_usd_ticks: WIZARD_PRICE_CAP_USD_TICKS,
      offers_per_agent_per_day: 5,
      min_offers: 2,
    },
  };
}
