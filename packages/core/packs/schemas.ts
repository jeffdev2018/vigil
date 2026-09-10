import { z } from "zod";

/**
 * Packs (OS plan, vague B): a pack turns a workspace into a ready-to-use
 * setup for a function — statuses, work item types, labels, properties,
 * views, rules, a doctrine section, a procedure skill, paused agents,
 * disabled autopilots, a project, a goal, a Brain note, example issues.
 *
 * Installing one runs the workspace transfer pipeline (preview, strategy,
 * one transaction, report) and writes an install ledger, so a pack can be
 * upgraded in place or uninstalled.
 *
 * Every schema here is deliberately lenient (`.loose()`, `.catch()` per
 * field, server enums kept as `z.string()`) so a newer server that ships a
 * new domain, prerequisite kind or strategy still renders. Switches on these
 * values carry a `default` branch; see CLAUDE.md "API Compatibility".
 *
 * The collision / item / report shapes below deliberately restate the
 * transfer ones from `../api/schemas` rather than importing them: the pack
 * report carries `items`, which the transfer schema does not model, and the
 * transfer collision schema is not exported.
 */

/** Mirrors `packs.Domains`; a server value outside this list still renders. */
export const PACK_DOMAINS = [
  "helpdesk",
  "ops",
  "support",
  "sales",
  "marketing",
  "leadership",
  "research",
  "hr",
  "finance",
  "legal",
  "engineering",
  "other",
] as const;

/** Mirrors `transferStrategies`. `skip` is the server default on a first install. */
export const PACK_STRATEGIES = ["skip", "merge", "rename"] as const;
export type PackStrategy = (typeof PACK_STRATEGIES)[number];

/** Prerequisite verification the server could do; `unknown` means it cannot tell. */
export type PackPrerequisiteStatus = "met" | "missing" | "unknown";

export const PackMetricSchema = z
  .object({
    label: z.string().catch(""),
    description: z.string().catch(""),
    hint: z.string().catch(""),
  })
  .loose()
  .catch({ label: "", description: "", hint: "" });

export const PackPrerequisiteSchema = z
  .object({
    kind: z.string().catch(""),
    name: z.string().catch(""),
    optional: z.boolean().catch(false),
    note: z.string().catch(""),
    status: z.string().catch("unknown"),
  })
  .loose();
export type PackPrerequisite = z.infer<typeof PackPrerequisiteSchema>;

export const PackChangelogRowSchema = z
  .object({ version: z.string().catch(""), note: z.string().catch("") })
  .loose();

export const PackManifestSchema = z
  .object({
    id: z.string().catch(""),
    version: z.string().catch(""),
    title: z.string().catch(""),
    summary: z.string().catch(""),
    /** Markdown. */
    description: z.string().catch(""),
    domain: z.string().catch("other"),
    wave: z.number().catch(0),
    author: z.string().catch(""),
    license: z.string().catch(""),
    tags: z.array(z.string()).catch([]).default([]),
    works_without_agents: z.boolean().catch(false),
    metric: PackMetricSchema,
    /** Declared prerequisites; only the catalogue's copy carries a `status`. */
    prerequisites: z.array(PackPrerequisiteSchema).catch([]).default([]),
    changelog: z.array(PackChangelogRowSchema).catch([]).default([]),
  })
  .loose();
export type PackManifest = z.infer<typeof PackManifestSchema>;

export const EMPTY_PACK_MANIFEST: PackManifest = {
  id: "",
  version: "",
  title: "",
  summary: "",
  description: "",
  domain: "other",
  wave: 0,
  author: "",
  license: "",
  tags: [],
  works_without_agents: false,
  metric: { label: "", description: "", hint: "" },
  prerequisites: [],
  changelog: [],
};

const CountsSchema = z.record(z.string(), z.number()).catch({}).default({});

export const PackSummarySchema = z
  .object({
    manifest: PackManifestSchema,
    counts: CountsSchema,
    builtin: z.boolean().catch(false),
    installed_version: z.string().nullable().catch(null),
    install_id: z.string().nullable().catch(null),
    upgrade_available: z.boolean().catch(false),
    prerequisites: z.array(PackPrerequisiteSchema).catch([]).default([]),
  })
  .loose();
export type PackSummary = z.infer<typeof PackSummarySchema>;

export const EMPTY_PACK_SUMMARY: PackSummary = {
  manifest: EMPTY_PACK_MANIFEST,
  counts: {},
  builtin: false,
  installed_version: null,
  install_id: null,
  upgrade_available: false,
  prerequisites: [],
};

/** `Record<kind, names[]>` — the names a pack would create, per kind. */
export const PackContentsSchema = z
  .record(z.string(), z.array(z.string()).catch([]))
  .catch({})
  .default({});
export type PackContents = z.infer<typeof PackContentsSchema>;

export const PackCollisionSchema = z
  .object({
    kind: z.string().catch(""),
    name: z.string().catch(""),
    existing_id: z.string().catch(""),
  })
  .loose();
export type PackCollision = z.infer<typeof PackCollisionSchema>;

export const PackItemSchema = z
  .object({
    kind: z.string().catch(""),
    name: z.string().catch(""),
    id: z.string().catch(""),
    action: z.string().catch(""),
  })
  .loose();
export type PackItem = z.infer<typeof PackItemSchema>;

export const PackReportSchema = z
  .object({
    created: CountsSchema,
    merged: CountsSchema,
    skipped: z.array(PackCollisionSchema).catch([]).default([]),
    warnings: z.array(z.string()).catch([]).default([]),
    items: z.array(PackItemSchema).catch([]).default([]),
  })
  .loose();
export type PackReport = z.infer<typeof PackReportSchema>;

export const EMPTY_PACK_REPORT: PackReport = {
  created: {},
  merged: {},
  skipped: [],
  warnings: [],
  items: [],
};

/**
 * One row of the install ledger. `report` is the transfer report on an
 * install and the uninstall report on a removed one, so it stays an opaque
 * record here — the install/uninstall mutations return the typed report.
 */
export const PackInstallSchema = z
  .object({
    id: z.string().catch(""),
    pack_id: z.string().catch(""),
    pack_version: z.string().catch(""),
    title: z.string().catch(""),
    source: z.string().catch("builtin"),
    strategy: z.string().catch("skip"),
    status: z.string().catch("installed"),
    run_id: z.string().nullable().catch(null),
    report: z.record(z.string(), z.unknown()).catch({}).default({}),
    manifest: z.record(z.string(), z.unknown()).catch({}).default({}),
    installed_by: z.string().nullable().catch(null),
    installed_at: z.string().catch(""),
    removed_at: z.string().nullable().catch(null),
    item_count: z.number().catch(0),
    metric: PackMetricSchema,
    domain: z.string().catch("other"),
    /** The catalogue version this install can move to, when newer. */
    upgrade_to: z.string().nullable().catch(null),
    bundle_sha256: z.string().catch(""),
  })
  .loose();
export type PackInstall = z.infer<typeof PackInstallSchema>;

export const PackCatalogueSchema = z
  .object({
    packs: z.array(PackSummarySchema).catch([]).default([]),
    domains: z.array(z.string()).catch([]).default([]),
  })
  .loose();
export type PackCatalogue = z.infer<typeof PackCatalogueSchema>;

export const EMPTY_PACK_CATALOGUE: PackCatalogue = { packs: [], domains: [] };

/**
 * One entry of the pre-workspace catalogue (`GET /api/pack-catalogue`): the
 * manifest, the counts and the contents, and deliberately no install state —
 * that endpoint takes no workspace, so there is no workspace to report
 * against. It is what the create-workspace flow picks a seed from.
 */
export const PackCatalogueEntrySchema = z
  .object({
    manifest: PackManifestSchema,
    counts: CountsSchema,
    contents: PackContentsSchema,
  })
  .loose();
export type PackCatalogueEntry = z.infer<typeof PackCatalogueEntrySchema>;

export const PackSeedCatalogueSchema = z
  .object({
    packs: z.array(PackCatalogueEntrySchema).catch([]).default([]),
    domains: z.array(z.string()).catch([]).default([]),
  })
  .loose();
export type PackSeedCatalogue = z.infer<typeof PackSeedCatalogueSchema>;

export const EMPTY_PACK_SEED_CATALOGUE: PackSeedCatalogue = { packs: [], domains: [] };

export const PackDetailSchema = z
  .object({
    pack: PackSummarySchema,
    contents: PackContentsSchema,
    /** The pack.yaml itself. */
    source: z.string().catch(""),
  })
  .loose();
export type PackDetail = z.infer<typeof PackDetailSchema>;

export const EMPTY_PACK_DETAIL: PackDetail = {
  pack: EMPTY_PACK_SUMMARY,
  contents: {},
  source: "",
};

export const PackPreviewSchema = z
  .object({
    pack: PackSummarySchema,
    contents: PackContentsSchema,
    collisions: z.array(PackCollisionSchema).catch([]).default([]),
    problems: z.array(z.string()).catch([]).default([]),
    strategies: z.array(z.string()).catch([...PACK_STRATEGIES]).default([...PACK_STRATEGIES]),
    /** The strategy the server picked: skip on a first install, merge on an upgrade. */
    strategy: z.string().catch("skip"),
    installed: PackInstallSchema.nullable().catch(null),
    /** Empty when installable; otherwise why not (same version, downgrade). */
    blocked: z.string().catch(""),
  })
  .loose();
export type PackPreview = z.infer<typeof PackPreviewSchema>;

export const EMPTY_PACK_PREVIEW: PackPreview = {
  pack: EMPTY_PACK_SUMMARY,
  contents: {},
  collisions: [],
  problems: [],
  strategies: [...PACK_STRATEGIES],
  strategy: "skip",
  installed: null,
  blocked: "",
};

export const PackInstallResultSchema = z
  .object({ install: PackInstallSchema, report: PackReportSchema })
  .loose();
export type PackInstallResult = z.infer<typeof PackInstallResultSchema>;

export const PackInstallListSchema = z
  .object({ installs: z.array(PackInstallSchema).catch([]).default([]) })
  .loose();

export const PackInstallDetailSchema = z
  .object({
    install: PackInstallSchema,
    items: z.array(PackItemSchema).catch([]).default([]),
  })
  .loose();
export type PackInstallDetail = z.infer<typeof PackInstallDetailSchema>;

export const PackUninstallReportSchema = z
  .object({
    removed: CountsSchema,
    kept: z.array(PackItemSchema).catch([]).default([]),
    reasons: z.array(z.string()).catch([]).default([]),
  })
  .loose();
export type PackUninstallReport = z.infer<typeof PackUninstallReportSchema>;

export const PackUninstallResultSchema = z
  .object({ install: PackInstallSchema, report: PackUninstallReportSchema })
  .loose();
export type PackUninstallResult = z.infer<typeof PackUninstallResultSchema>;

export const EMPTY_PACK_INSTALL: PackInstall = {
  id: "",
  pack_id: "",
  pack_version: "",
  title: "",
  source: "builtin",
  strategy: "skip",
  status: "installed",
  run_id: null,
  report: {},
  manifest: {},
  installed_by: null,
  installed_at: "",
  removed_at: null,
  item_count: 0,
  metric: { label: "", description: "", hint: "" },
  domain: "other",
  upgrade_to: null,
  bundle_sha256: "",
};

/** What `POST /api/packs/export` needs: a manifest plus the content opt-ins. */
export interface PackExportInput {
  manifest: {
    id: string;
    version: string;
    title: string;
    summary: string;
    domain: string;
    metric: { label: string; description: string; hint?: string };
  };
  include_issues: boolean;
  include_notes: boolean;
}

/** Total rows a report touched, for the one-line "n created · n merged" summary. */
export function packCountTotal(counts: Record<string, number>): number {
  return Object.values(counts).reduce((a, b) => a + b, 0);
}
