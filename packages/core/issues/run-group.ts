import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api, errorCode } from "../api";
import type { RunGroup, RunGroupAttempt, RunGroupJudgement, RunGroupJudgementScore, StartRunGroupInput } from "../api/schemas";
import { runCostUsd } from "../runs/fleet-schemas";
import { issueKeys } from "./queries";

// Racing attempts (F11 / JEF-6): N independent runs of one issue, the human
// keeps one. Nothing here is optimistic — settling cancels the losing attempts
// server-side and the daemon then drops their branches, so what the list shows
// after the call is only ever what the server answered.

export type { RunGroup, RunGroupAttempt, RunGroupJudgement, RunGroupJudgementScore, StartRunGroupInput };

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

/**
 * The judge (JEF-234) compares what actually finished, so the button mirrors
 * the server's rule (409 run_group_not_judgeable): at least two completed
 * attempts, on a race not yet settled. Judging an abandoned race is allowed —
 * the verdict is still useful for the record even though nothing can be kept.
 */
export function canJudgeRunGroup(group: RunGroup): boolean {
  if (group.status === "settled") return false;
  return group.attempts.filter((a) => a.status === "completed").length >= MIN_RUN_GROUP_ATTEMPTS;
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

/**
 * The attempt's cost (JEF-234) as a display string, or null when the server
 * has not reported one — 0 ticks means "unreported", not "free", so the UI
 * must not render it as $0.00. Cents under $100, whole dollars above (the
 * same rule the runtimes usage views use), and two significant digits under a
 * cent so a genuinely cheap attempt doesn't round to $0.00.
 */
export function formatUsdTicks(costUsdTicks: number): string | null {
  if (!Number.isFinite(costUsdTicks) || costUsdTicks <= 0) return null;
  const usd = runCostUsd(costUsdTicks);
  if (usd >= 100) return `$${usd.toFixed(0)}`;
  if (usd >= 0.01) return `$${usd.toFixed(2)}`;
  return `$${usd.toPrecision(2)}`;
}

/**
 * The attempt's duration as `m:ss`, or `h:mm` past an hour. 0 seconds is what
 * the server sends while the attempt is still running, so null tells the UI
 * to show a dash instead of a fake 0:00.
 */
export function formatAttemptDuration(seconds: number): string | null {
  if (!Number.isFinite(seconds) || seconds <= 0) return null;
  const total = Math.floor(seconds);
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (hours > 0) return `${hours}:${String(minutes).padStart(2, "0")}`;
  return `${minutes}:${String(total % 60).padStart(2, "0")}`;
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

export type RunGroupErrorKind = "already_active" | "already_settled" | "not_judgeable" | "generic";

/**
 * Maps a failed race write onto a sentence the UI owns. The 409 codes are
 * the ones the human can act on: for the first two the list is stale, refetch
 * and look again; for the third the race simply has too few finished attempts
 * for the judge to compare.
 */
export function runGroupErrorKind(err: unknown): RunGroupErrorKind {
  switch (errorCode(err)) {
    case "run_group_already_active":
      return "already_active";
    case "run_group_already_settled":
      return "already_settled";
    case "run_group_not_judgeable":
      return "not_judgeable";
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

/**
 * Asking the judge never settles the race — it only writes the verdict onto
 * the group — so unlike settle/abandon it leaves the issue's own runs alone.
 */
export function useJudgeRunGroup(wsId: string, issueId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ groupId }: { groupId: string }) => api.judgeRunGroup(groupId),
    onSettled: () => qc.invalidateQueries({ queryKey: runGroupKeys.issue(wsId, issueId) }),
  });
}
