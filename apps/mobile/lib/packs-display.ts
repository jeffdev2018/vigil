/**
 * Pack display helpers (OS plan, vague B).
 *
 * Every label mirrors `packages/views/locales/en/settings.json` → `packs`,
 * which is what web renders; mobile has no i18n, so the English strings live
 * here. Every lookup falls back to the raw server key rather than to a blank
 * or to a dropped row — a newer server shipping a new kind, domain, source,
 * status or strategy must still render (root CLAUDE.md "Server-driven enum
 * switches need a default branch").
 */

/** Content kinds a bundle can carry — `packs.kinds` on web. */
const KIND_LABEL: Record<string, string> = {
  issue_statuses: "statuses",
  issue_types: "work item types",
  labels: "labels",
  properties: "properties",
  views: "views",
  transition_rules: "transition rules",
  business_rules: "business rules",
  ownership_rules: "ownership rules",
  permission_profiles: "permission profiles",
  skills: "procedures",
  agents: "agents",
  projects: "projects",
  goals: "goals",
  autopilots: "automations",
  triage_sources: "intake sources",
  org_structures: "org charts",
  notes: "notes",
  issues: "work items",
  doctrine: "doctrine",
};

export function packKindLabel(kind: string): string {
  return KIND_LABEL[kind] ?? kind;
}

/** `packs.domains` on web. The server's own `domains` list drives the pills. */
const DOMAIN_LABEL: Record<string, string> = {
  helpdesk: "Helpdesk",
  ops: "Operations",
  support: "Customer support",
  sales: "Sales",
  marketing: "Marketing",
  leadership: "Leadership",
  research: "Research",
  hr: "People",
  finance: "Finance",
  legal: "Legal",
  engineering: "Engineering",
  other: "Other",
};

export function packDomainLabel(domain: string): string {
  return DOMAIN_LABEL[domain] ?? domain;
}

const STRATEGY_LABEL: Record<string, string> = {
  skip: "Skip",
  merge: "Merge",
  rename: "Rename",
};

export function packStrategyLabel(strategy: string): string {
  return STRATEGY_LABEL[strategy] ?? strategy;
}

/** One line under the strategy row — `packs.strategy_help` on web. */
const STRATEGY_HELP: Record<string, string> = {
  skip: "Anything this workspace already has by that name is left exactly as it is.",
  merge: "The pack's version updates the row you already have. Use it for an update.",
  rename: "The pack's item arrives under a new name, side by side with yours.",
};

export function packStrategyHelp(strategy: string): string {
  return STRATEGY_HELP[strategy] ?? "";
}

const PREREQUISITE_STATUS_LABEL: Record<string, string> = {
  met: "Met",
  missing: "Missing",
  unknown: "Up to you",
};

export function packPrerequisiteStatusLabel(status: string): string {
  return PREREQUISITE_STATUS_LABEL[status] ?? status;
}

const SOURCE_LABEL: Record<string, string> = {
  builtin: "Built-in",
  upload: "Upload",
  workspace: "Workspace",
};

export function packSourceLabel(source: string): string {
  return SOURCE_LABEL[source] ?? source;
}

const INSTALL_STATUS_LABEL: Record<string, string> = {
  installed: "Installed",
  failed: "Failed",
  removed: "Removed",
};

export function packInstallStatusLabel(status: string): string {
  return INSTALL_STATUS_LABEL[status] ?? status;
}

/** Total rows a report touched — mirrors `packCountTotal` in core. */
export function packCountTotal(counts: Record<string, number>): number {
  return Object.values(counts).reduce((a, b) => a + b, 0);
}

/**
 * "5 labels · 3 views · 2 agents" — biggest kinds first, capped at four,
 * zero-count kinds dropped. Same rule and same cap as web's `countsSummary`
 * in `packages/views/settings/components/packs-tab.tsx`, so the phone and
 * the browser summarise a pack identically.
 */
export function packCountsSummary(counts: Record<string, number>): string {
  return Object.entries(counts)
    .filter(([, n]) => n > 0)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 4)
    .map(([kind, n]) => `${n} ${packKindLabel(kind)}`)
    .join(" · ");
}

/** "Created · 3 labels, 1 view" — the per-kind breakdown of a report row. */
export function packCountsDetail(counts: Record<string, number>): string {
  return Object.entries(counts)
    .filter(([, n]) => n > 0)
    .map(([kind, n]) => `${n} ${packKindLabel(kind)}`)
    .join(", ");
}
