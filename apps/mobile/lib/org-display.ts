/**
 * Org chart display helpers (K75).
 *
 * `orgModelLabel` / `orgIsLive` mirror `packages/core/org/queries.ts`.
 * Mirrored, not imported: that file also exports React Query hooks, which
 * are outside mobile's `@multica/core` whitelist (types + pure functions).
 * When the web version changes, sync this file.
 */
import type { OrgModel, OrgStatus, OrgStructure } from "@multica/core/types";

const ORG_MODEL_LABEL: Record<OrgModel, string> = {
  hierarchy: "Hierarchy",
  squads: "Autonomous squads",
  matrix: "Competence × project matrix",
  circles: "Circles and roles",
  owner_network: "Owner network",
  taskforce: "Temporary task force",
  market: "Internal market",
};

/** Which of the seven models a structure follows; unknown values render raw. */
export function orgModelLabel(model: string): string {
  return (ORG_MODEL_LABEL as Record<string, string>)[model] ?? model;
}

const ORG_STATUS_LABEL: Record<OrgStatus, string> = {
  draft: "Draft",
  active: "Active",
  paused: "Paused",
  dissolved: "Dissolved",
};

// Unknown server values render as-is rather than crashing or vanishing
// (root CLAUDE.md "API Response Compatibility").
export function orgStatusLabel(value: string): string {
  return (ORG_STATUS_LABEL as Record<string, string>)[value] ?? value;
}

/** Is the structure acting right now. */
export function orgIsLive(s: Pick<OrgStructure, "status">): boolean {
  return s.status === "active";
}
