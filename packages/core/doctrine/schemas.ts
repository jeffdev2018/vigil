import { z } from "zod";

/**
 * Workspace doctrine (OS plan, chantier 22): the old workspace "Context"
 * textarea grown into the one governing document a workspace's owners write
 * for every agent — versioned, optionally held for a second reviewer, with a
 * line diff, restore, and the reports an agent files when a task collides
 * with a rule.
 *
 * Every schema here is deliberately lenient (`.loose()`, `.catch()` per
 * field, string enums kept as `z.string()`), so a newer server that adds a
 * version status or a report kind still renders. Switches on these values
 * carry a `default` branch; see CLAUDE.md "API Compatibility".
 */

export type DoctrineVersionStatus = "active" | "pending" | "rejected" | "superseded";
export type DoctrineReportKind = "conflict" | "refusal" | "ambiguity";
export type DoctrineReportStatus = "open" | "acknowledged" | "dismissed";
/** `all` is a filter value the list endpoint accepts, never a stored status. */
export type DoctrineReportFilter = DoctrineReportStatus | "all";

export const DOCTRINE_REPORT_KINDS: readonly DoctrineReportKind[] = ["conflict", "refusal", "ambiguity"];
export const DOCTRINE_REPORT_FILTERS: readonly DoctrineReportFilter[] = ["open", "acknowledged", "dismissed", "all"];
/** Server fallback (`doctrineMaxBytes`), used only until the real limit lands. */
export const DOCTRINE_DEFAULT_BYTE_LIMIT = 32000;

export const DoctrineVersionSchema = z.object({
  id: z.string().catch(""),
  /** null while a proposal is pending or after it was rejected: it was never live. */
  revision: z.number().nullable().catch(null),
  content: z.string().catch(""),
  status: z.string().catch("superseded"),
  note: z.string().catch(""),
  author_id: z.string().nullable().catch(null),
  reviewed_by: z.string().nullable().catch(null),
  reviewed_at: z.string().nullable().catch(null),
  review_note: z.string().catch(""),
  restored_from_revision: z.number().nullable().catch(null),
  created_at: z.string().catch(""),
  bytes: z.number().catch(0),
}).loose();
export type DoctrineVersion = z.infer<typeof DoctrineVersionSchema>;

export const EMPTY_DOCTRINE_VERSION: DoctrineVersion = {
  id: "",
  revision: null,
  content: "",
  status: "superseded",
  note: "",
  author_id: null,
  reviewed_by: null,
  reviewed_at: null,
  review_note: "",
  restored_from_revision: null,
  created_at: "",
  bytes: 0,
};

export const DoctrineSchema = z.object({
  content: z.string().catch(""),
  revision: z.number().catch(0),
  updated_at: z.string().nullable().catch(null),
  updated_by: z.string().nullable().catch(null),
  byte_limit: z.number().catch(DOCTRINE_DEFAULT_BYTE_LIMIT),
  require_review: z.boolean().catch(false),
  can_publish: z.boolean().catch(false),
  active_version_id: z.string().nullable().catch(null),
  pending: DoctrineVersionSchema.nullable().catch(null),
  open_reports: z.number().catch(0),
}).loose();
export type Doctrine = z.infer<typeof DoctrineSchema>;

export const EMPTY_DOCTRINE: Doctrine = {
  content: "",
  revision: 0,
  updated_at: null,
  updated_by: null,
  byte_limit: DOCTRINE_DEFAULT_BYTE_LIMIT,
  require_review: false,
  can_publish: false,
  active_version_id: null,
  pending: null,
  open_reports: 0,
};

export const DoctrinePublishResponseSchema = z.object({
  doctrine: DoctrineSchema.catch(EMPTY_DOCTRINE),
  version: DoctrineVersionSchema.catch(EMPTY_DOCTRINE_VERSION),
}).loose();
export interface DoctrinePublishResponse {
  doctrine: Doctrine;
  version: DoctrineVersion;
}
export const EMPTY_DOCTRINE_PUBLISH: DoctrinePublishResponse = {
  doctrine: EMPTY_DOCTRINE,
  version: EMPTY_DOCTRINE_VERSION,
};

export const DoctrineVersionsResponseSchema = z.object({
  versions: z.array(DoctrineVersionSchema).catch([]),
  next_cursor: z.string().nullable().catch(null),
}).loose();
export interface DoctrineVersionsResponse {
  versions: DoctrineVersion[];
  next_cursor: string | null;
}
export const EMPTY_DOCTRINE_VERSIONS: DoctrineVersionsResponse = { versions: [], next_cursor: null };

export const DoctrineVersionEnvelopeSchema = z.object({
  version: DoctrineVersionSchema.catch(EMPTY_DOCTRINE_VERSION),
}).loose();

export const DoctrineDiffLineSchema = z.object({
  kind: z.string().catch("same"),
  text: z.string().catch(""),
}).loose();
export type DoctrineDiffLine = z.infer<typeof DoctrineDiffLineSchema>;

export const DoctrineDiffSchema = z.object({
  /** null when the version has no predecessor; its `content` is stripped server-side. */
  from: DoctrineVersionSchema.nullable().catch(null),
  to: DoctrineVersionSchema.catch(EMPTY_DOCTRINE_VERSION),
  lines: z.array(DoctrineDiffLineSchema).catch([]),
  added: z.number().catch(0),
  removed: z.number().catch(0),
}).loose();
export interface DoctrineDiff {
  from: DoctrineVersion | null;
  to: DoctrineVersion;
  lines: DoctrineDiffLine[];
  added: number;
  removed: number;
}
export const EMPTY_DOCTRINE_DIFF: DoctrineDiff = {
  from: null,
  to: EMPTY_DOCTRINE_VERSION,
  lines: [],
  added: 0,
  removed: 0,
};

export const DoctrineReportSchema = z.object({
  id: z.string().catch(""),
  doctrine_revision: z.number().catch(0),
  kind: z.string().catch("ambiguity"),
  summary: z.string().catch(""),
  passage: z.string().catch(""),
  reporter_type: z.string().catch("agent"),
  reporter_id: z.string().catch(""),
  task_id: z.string().nullable().catch(null),
  issue_id: z.string().nullable().catch(null),
  status: z.string().catch("open"),
  resolved_by: z.string().nullable().catch(null),
  resolved_at: z.string().nullable().catch(null),
  resolution_note: z.string().catch(""),
  created_at: z.string().catch(""),
}).loose();
export type DoctrineReport = z.infer<typeof DoctrineReportSchema>;

export const EMPTY_DOCTRINE_REPORT: DoctrineReport = {
  id: "",
  doctrine_revision: 0,
  kind: "ambiguity",
  summary: "",
  passage: "",
  reporter_type: "agent",
  reporter_id: "",
  task_id: null,
  issue_id: null,
  status: "open",
  resolved_by: null,
  resolved_at: null,
  resolution_note: "",
  created_at: "",
};

export const DoctrineReportsResponseSchema = z.object({
  reports: z.array(DoctrineReportSchema).catch([]),
}).loose();
export interface DoctrineReportsResponse {
  reports: DoctrineReport[];
}
export const EMPTY_DOCTRINE_REPORTS: DoctrineReportsResponse = { reports: [] };

export const DoctrineReportEnvelopeSchema = z.object({
  report: DoctrineReportSchema.catch(EMPTY_DOCTRINE_REPORT),
}).loose();

/** What a publication sends. `expected_revision` is the optimistic-lock guard. */
export interface DoctrinePublishInput {
  content: string;
  expected_revision: number;
  note?: string;
}

/**
 * Byte length of the doctrine as the server counts it (`len(content)` over
 * UTF-8), not `String.length` — an accented or CJK doctrine is over the limit
 * long before its character count says so.
 */
export function doctrineByteLength(content: string): number {
  return new TextEncoder().encode(content).length;
}

/** The trailing-whitespace trim the server applies before comparing/storing. */
export function normalizeDoctrineContent(content: string): string {
  return content.replace(/\r\n/g, "\n").replace(/[ \t\r\n]+$/, "");
}

/**
 * Whether Publish should be offered: a manager, changed text, within the
 * limit, and no proposal already waiting (the server 409s on that).
 */
export function canPublishDoctrine(doctrine: Doctrine, draft: string): boolean {
  if (doctrine.can_publish !== true || doctrine.pending) return false;
  const next = normalizeDoctrineContent(draft);
  if (next === normalizeDoctrineContent(doctrine.content)) return false;
  return doctrineByteLength(next) <= doctrine.byte_limit;
}
