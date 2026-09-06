import { z } from "zod";

// Shared semantic repo index (K47): an index of the workspace's repository
// code, built by the daemon after runs and handed to the next run in its brief
// so it starts from a place instead of a grep.
//
// The workspace opts in per repository, because indexing copies source code
// into Multica's database — and, when the deployment configures an embeddings
// model, sends it to that model's provider.

/** Per-repository index state as the settings endpoint reports it. */
export const RepoIndexRepoSchema = z.object({
  repo_identifier: z.string().catch(""),
  enabled: z.boolean().catch(false),
  chunk_count: z.number().catch(0),
  file_count: z.number().catch(0),
  last_indexed_commit: z.string().catch(""),
  last_indexed_at: z.string().catch(""),
}).loose();

export const RepoIndexSettingsSchema = z.object({
  repos: z.array(RepoIndexRepoSchema).catch([]).default([]),
  embeddings_enabled: z.boolean().catch(false),
}).loose();

export interface RepoIndexRepo {
  repo_identifier: string;
  enabled: boolean;
  chunk_count: number;
  file_count: number;
  /** Commit the newest stored chunk came from; empty when never indexed. */
  last_indexed_commit: string;
  /** RFC3339; empty when never indexed. */
  last_indexed_at: string;
}

export interface RepoIndexSettings {
  repos: RepoIndexRepo[];
  /**
   * Whether this deployment ranks by meaning as well as by keyword. Reported by
   * the server rather than inferred: it depends on MULTICA_LLM_EMBEDDING_MODEL,
   * which no client can see, and a lexical-only index is a supported state
   * rather than a broken one.
   */
  embeddings_enabled: boolean;
}

export interface RepoIndexSettingsInput {
  repo_identifier: string;
  enabled: boolean;
}

export const REPO_INDEX_EMPTY_SETTINGS: RepoIndexSettings = {
  repos: [],
  embeddings_enabled: false,
};

export type RepoIndexState = "disabled" | "indexing" | "indexed";

/**
 * Which of the three states one repository is in.
 *
 * "indexing" is the enabled-but-empty state, which is genuinely what a user
 * sees between flipping the toggle and the first run finishing on that
 * repository — the index is built by the daemon after a run, not on demand, so
 * an enabled repo with no chunks is waiting rather than broken.
 */
export function repoIndexState(repo: RepoIndexRepo): RepoIndexState {
  if (repo?.enabled !== true) return "disabled";
  return (repo?.chunk_count ?? 0) > 0 ? "indexed" : "indexing";
}

/** Short commit for display; empty input stays empty. */
export function shortCommit(commit: string, length = 7): string {
  return (commit ?? "").slice(0, length);
}
