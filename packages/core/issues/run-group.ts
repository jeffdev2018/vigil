import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, errorCode } from "../api";
import type { RunGroup, RunGroupAttempt, StartRunGroupInput } from "../api/schemas";
import { issueKeys } from "./queries";

// Racing attempts (F11 / JEF-6): N independent runs of one issue, the human
// keeps one. Nothing here is optimistic — settling cancels the losing attempts
// server-side and the daemon then drops their branches, so what the list shows
// after the call is only ever what the server answered.

export type { RunGroup, RunGroupAttempt, StartRunGroupInput };

/** Server bounds, mirrored from server/internal/handler/run_group.go. */
export const MIN_RUN_GROUP_ATTEMPTS = 2;
export const MAX_RUN_GROUP_ATTEMPTS = 5;

export const runGroupKeys = {
  issue: (wsId: string, issueId: string) => ["run-groups", wsId, issueId] as const,
};

/**
 * Newest race first. created_at is an ISO string from the server, but a
 * malformed one parses to NaN and would silently scramble the order, so the
 * id (a UUIDv7, monotonic by creation) is the tiebreaker and the fallback.
 */
export function sortRunGroups(groups: readonly RunGroup[]): RunGroup[] {
  return [...groups].sort((a, b) => {
    const ta = Date.parse(a.created_at);
    const tb = Date.parse(b.created_at);
    if (Number.isFinite(ta) && Number.isFinite(tb) && ta !== tb) return tb - ta;
    return b.id.localeCompare(a.id);
  });
}

/** A race is running when the server says so; only then can it be settled. */
export function isRunGroupRunning(group: RunGroup): boolean {
  return group.status === "running";
}

/**
 * One open race per issue (CountActiveRunGroupsForIssue). The start button is
 * disabled while any group is running rather than letting the user discover it
 * through a 409.
 */
export function canStartRunGroup(groups: readonly RunGroup[]): boolean {
  return !groups.some(isRunGroupRunning);
}

export interface DiffStat {
  files: number;
  insertions: number;
  deletions: number;
}

/**
 * diff_stat is a daemon-written JSONB blob typed `unknown` at the boundary —
 * nothing in the repo pins its field names yet, so read the plausible spellings
 * and drop anything that is not a finite non-negative number. Returns null when
 * no shape at all could be read, which the UI renders as "no diff recorded".
 */
export function parseDiffStat(value: unknown): DiffStat | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  const row = value as Record<string, unknown>;
  const num = (...names: string[]): number | null => {
    for (const name of names) {
      const raw = row[name];
      if (typeof raw === "number" && Number.isFinite(raw) && raw >= 0) return Math.trunc(raw);
    }
    return null;
  };
  const files = num("files", "files_changed", "changed_files");
  const insertions = num("insertions", "additions", "added");
  const deletions = num("deletions", "removals", "deleted");
  if (files === null && insertions === null && deletions === null) return null;
  return { files: files ?? 0, insertions: insertions ?? 0, deletions: deletions ?? 0 };
}

/** "3 files +120 −18", or null when there is no stat to show. */
export function diffStatLabel(value: unknown): string | null {
  const stat = parseDiffStat(value);
  if (!stat) return null;
  return `${stat.files} · +${stat.insertions} −${stat.deletions}`;
}

export type DiffLineKind = "added" | "removed" | "hunk" | "meta" | "context";

export interface DiffLine {
  kind: DiffLineKind;
  text: string;
}

/**
 * Classifies the lines of an already-unified diff for display. `+++` / `---`
 * are file headers, not an added and a removed line — checked before the
 * single-character cases, which is the whole reason this is not an inline
 * `startsWith("+")` in the component.
 */
export function diffUnifiedLines(diff: string): DiffLine[] {
  if (diff === "") return [];
  return diff.split("\n").map((text) => {
    if (text.startsWith("+++") || text.startsWith("---") || text.startsWith("diff ") || text.startsWith("index ")) {
      return { kind: "meta" as const, text };
    }
    if (text.startsWith("@@")) return { kind: "hunk" as const, text };
    if (text.startsWith("+")) return { kind: "added" as const, text };
    if (text.startsWith("-")) return { kind: "removed" as const, text };
    return { kind: "context" as const, text };
  });
}

export type RunGroupErrorKind = "already_active" | "already_settled" | "generic";

/**
 * Maps a failed race write onto a sentence the UI owns. The two 409 codes are
 * the ones the human can act on: the list is stale, refetch and look again.
 */
export function runGroupErrorKind(err: unknown): RunGroupErrorKind {
  switch (errorCode(err)) {
    case "run_group_already_active":
      return "already_active";
    case "run_group_already_settled":
      return "already_settled";
    default:
      return "generic";
  }
}

export function runGroupsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: runGroupKeys.issue(wsId, issueId),
    queryFn: () => api.listRunGroups(issueId),
    // Attempts are ordinary runs whose status and diff land asynchronously.
    // The server publishes issue:updated for a race (publishIssueAuxChanged),
    // which no aux section subscribes to; polling is what the neighbouring
    // duel section does for the same reason.
    refetchInterval: 10_000,
  });
}

/**
 * Settling and abandoning both cancel the other attempts and bump the issue
 * revision server-side, so the issue's own runs are invalidated too.
 */
function invalidateRace(qc: ReturnType<typeof useQueryClient>, wsId: string, issueId: string) {
  qc.invalidateQueries({ queryKey: runGroupKeys.issue(wsId, issueId) });
  qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
}

export function useStartRunGroup(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: StartRunGroupInput) => api.startRunGroup(issueId, input),
    onSettled: () => invalidateRace(qc, wsId, issueId),
  });
}

export function useSettleRunGroup(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ groupId, winnerTaskId }: { groupId: string; winnerTaskId: string }) =>
      api.settleRunGroup(groupId, winnerTaskId),
    onSettled: () => invalidateRace(qc, wsId, issueId),
  });
}

export function useAbandonRunGroup(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ groupId }: { groupId: string }) => api.abandonRunGroup(groupId),
    onSettled: () => invalidateRace(qc, wsId, issueId),
  });
}
