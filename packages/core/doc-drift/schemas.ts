import { z } from "zod";

// Agent context document drift detection (K56).
//
// The agent context document — CLAUDE.md, AGENTS.md, the conventions doc — is
// what every agent run reads before it acts, and it is the file that quietly
// stops being true: a command is renamed, a package moves, a convention is
// dropped. After the default branch moves, a read-only run compares the
// document against what the repository actually declares and the server turns
// what drifted into a proposal reviewed as a DRAFT pull request.
//
// Nothing here ever lands on the default branch by itself. The proposal is the
// product; the pull request is how a human sees it.

export type DocDriftStatus = "draft" | "opened_pr" | "dismissed" | "merged";

// Enums stay `z.string()` with a safe `.catch()` so an unknown value from a
// newer server still parses; the UI switches carry a default branch. The
// exported TS types are the narrow unions.
export const DocDriftProposalSchema = z.object({
  id: z.string(),
  workspace_id: z.string().catch(""),
  repo_identifier: z.string().catch(""),
  doc_path: z.string().catch(""),
  detected_drift: z.string().catch(""),
  proposed_patch: z.string().catch(""),
  detected_at_commit: z.string().catch(""),
  status: z.string().catch("draft"),
  pull_request_url: z.string().catch(""),
  scan_task_id: z.string().nullable().catch(null),
  pr_task_id: z.string().nullable().catch(null),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();

export const DocDriftProposalListSchema = z.object({
  proposals: z.array(DocDriftProposalSchema).catch([]).default([]),
}).loose();

export const DocDriftProposalEnvelopeSchema = z.object({
  proposal: DocDriftProposalSchema.nullable().catch(null),
}).loose();

export const DocDriftRepoSchema = z.object({
  repo_identifier: z.string().catch(""),
  last_indexed_commit: z.string().catch(""),
  last_checked_commit: z.string().catch(""),
  due: z.boolean().catch(false),
  scanning: z.boolean().catch(false),
}).loose();

export const DocDriftSettingsSchema = z.object({
  enabled: z.boolean().catch(false),
  agent_id: z.string().catch(""),
  docs: z.array(z.string()).catch([]).default([]),
  open_pr: z.boolean().catch(true),
  last_checked: z.record(z.string(), z.string()).catch({}).default({}),
  scan_tasks: z.record(z.string(), z.string()).catch({}).default({}),
  repos: z.array(DocDriftRepoSchema).catch([]).default([]),
}).loose();

export const DocDriftCheckSchema = z.object({
  repo_identifier: z.string().catch(""),
  task_id: z.string().catch(""),
}).loose();

export interface DocDriftProposal {
  id: string;
  workspace_id: string;
  repo_identifier: string;
  doc_path: string;
  detected_drift: string;
  proposed_patch: string;
  detected_at_commit: string;
  status: DocDriftStatus;
  pull_request_url: string;
  scan_task_id: string | null;
  pr_task_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface DocDriftRepo {
  repo_identifier: string;
  /** Commit the daemon last indexed this repository at; empty when never. */
  last_indexed_commit: string;
  /** Commit this check last ran at; empty when never. */
  last_checked_commit: string;
  /** The two differ: the default branch moved since the last check. */
  due: boolean;
  /** A scan is in flight; a second check would be refused with 409. */
  scanning: boolean;
}

export interface DocDriftSettings {
  enabled: boolean;
  agent_id: string;
  docs: string[];
  open_pr: boolean;
  /** Server-owned; a client never sets these. */
  last_checked: Record<string, string>;
  scan_tasks: Record<string, string>;
  repos: DocDriftRepo[];
}

/** What a PUT may carry. The server owns everything else. */
export interface DocDriftSettingsInput {
  enabled: boolean;
  agent_id: string;
  docs: string[];
  open_pr: boolean;
}

export const DOC_DRIFT_DEFAULT_DOCS = [
  "CLAUDE.md",
  "AGENTS.md",
  "apps/docs/content/docs/developers/conventions.mdx",
];

export const DOC_DRIFT_DEFAULT_SETTINGS: DocDriftSettings = {
  enabled: false,
  agent_id: "",
  docs: [...DOC_DRIFT_DEFAULT_DOCS],
  open_pr: true,
  last_checked: {},
  scan_tasks: {},
  repos: [],
};

/**
 * A proposal still waiting on a human: it is what the list is for, and what
 * blocks a fresh detection on the same document.
 */
export function isOpenProposal(proposal: DocDriftProposal): boolean {
  return proposal?.status === "draft" || proposal?.status === "opened_pr";
}

/**
 * Colour band for a proposal row. A draft is work waiting on someone, an open
 * pull request is progress, and a dismissed proposal is a decision — none of
 * them is an error, so nothing here is destructive.
 */
export function proposalTone(status: string): "success" | "warning" | "muted" {
  switch (status) {
    case "merged":
      return "success";
    case "draft":
    case "opened_pr":
      return "warning";
    default:
      return "muted";
  }
}

/** Short commit for display; empty input stays empty. */
export function shortDriftCommit(commit: string, length = 7): string {
  return (commit ?? "").slice(0, length);
}

/**
 * First line of the drift summary, for a list row. The full text is a bulleted
 * report and would swamp a table cell.
 */
export function driftExcerpt(proposal: DocDriftProposal, max = 160): string {
  const first = (proposal?.detected_drift ?? "").split("\n").map((l) => l.trim()).find(Boolean) ?? "";
  return first.length > max ? `${first.slice(0, max - 1)}…` : first;
}
