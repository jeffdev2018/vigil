import { z } from "zod";

// Review flags by severity (F06 / JEF-19).
//
// A flag is one structured finding an agent recorded during its run: a file, a
// line range of one revision of a linked pull request, a severity and — when
// the author was willing to state one — a confidence.
//
// The ORDER is the server's, not this module's: severity beats confidence, and
// sorting again here would be a second, quietly diverging opinion about what a
// reviewer reads first. What lives here is the vocabulary and the defaults a
// response from a newer or older build has to degrade into.

/** Render and sort order. `bug` is what must be fixed; `info` is what may be skipped. */
export const REVIEW_FLAG_SEVERITIES = ["bug", "warning", "info"] as const;
export type ReviewFlagSeverity = (typeof REVIEW_FLAG_SEVERITIES)[number];

export const REVIEW_FLAG_STATES = ["open", "resolved", "dismissed", "stale"] as const;
export type ReviewFlagState = (typeof REVIEW_FLAG_STATES)[number];

/** What the list endpoint accepts. `open` is what a reviewer still owes. */
export type ReviewFlagFilter = "open" | "all";

// Enums stay `z.string()` with a safe `.catch()` so an unknown value from a
// newer server still parses; `normalizeSeverity` / `normalizeState` decide what
// it reads as, and the UI switches carry a default branch.
export const ReviewFlagSchema = z.object({
  id: z.string().catch(""),
  issue_id: z.string().catch(""),
  pr_source: z.string().catch(""),
  pr_id: z.string().catch(""),
  head_sha: z.string().catch(""),
  file_path: z.string().catch(""),
  line_start: z.number().catch(0).default(0),
  line_end: z.number().catch(0).default(0),
  side: z.string().catch("new"),
  severity: z.string().catch("info"),
  // Absent and null are the same answer — "did not say" — and neither is 0.
  confidence: z.number().nullable().catch(null).default(null),
  title: z.string().catch(""),
  body: z.string().catch(""),
  author_agent_id: z.string().catch(""),
  author_user_id: z.string().catch(""),
  task_id: z.string().catch(""),
  state: z.string().catch("open"),
  resolved_by_type: z.string().catch(""),
  resolved_by_id: z.string().catch(""),
  resolved_at: z.string().catch(""),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();

export const ReviewFlagCountsSchema = z.object({
  bug: z.number().catch(0).default(0),
  warning: z.number().catch(0).default(0),
  info: z.number().catch(0).default(0),
}).loose();

export const ReviewFlagListSchema = z.object({
  flags: z.array(ReviewFlagSchema).catch([]).default([]),
  counts: ReviewFlagCountsSchema.catch({ bug: 0, warning: 0, info: 0 }).default({ bug: 0, warning: 0, info: 0 }),
}).loose();

export interface ReviewFlag {
  id: string;
  issue_id: string;
  pr_source: string;
  pr_id: string;
  head_sha: string;
  file_path: string;
  line_start: number;
  line_end: number;
  /** Open string: a newer server may send a side this build does not know. */
  side: string;
  /** Open string: unknown severities read as `info` via `normalizeSeverity`. */
  severity: string;
  /** `null` means the author declined to state one. Never the same as 0. */
  confidence: number | null;
  title: string;
  body: string;
  author_agent_id: string;
  author_user_id: string;
  task_id: string;
  /** Open string: unknown states read as `open` via `normalizeState`. */
  state: string;
  resolved_by_type: string;
  resolved_by_id: string;
  resolved_at: string;
  created_at: string;
  updated_at: string;
}

export interface ReviewFlagCounts {
  bug: number;
  warning: number;
  info: number;
}

export interface ReviewFlagList {
  flags: ReviewFlag[];
  counts: ReviewFlagCounts;
}

/** An empty list is a real answer: this pull request has nothing flagged. */
export const EMPTY_REVIEW_FLAG_LIST: ReviewFlagList = {
  flags: [],
  counts: { bug: 0, warning: 0, info: 0 },
};

/**
 * An unknown severity reads as `info` — worth reading, nothing owed. The other
 * direction would be worse: a vocabulary this build has never seen would jump
 * to the top of the reviewer's list and claim to be a defect.
 */
export function normalizeSeverity(severity: string): ReviewFlagSeverity {
  const lower = (severity ?? "").trim().toLowerCase();
  return (REVIEW_FLAG_SEVERITIES as readonly string[]).includes(lower)
    ? (lower as ReviewFlagSeverity)
    : "info";
}

/**
 * An unknown state reads as `open`. A finding this build cannot classify is
 * still a finding, and hiding it would be the one failure mode a review tool
 * must not have.
 */
export function normalizeState(state: string): ReviewFlagState {
  const lower = (state ?? "").trim().toLowerCase();
  return (REVIEW_FLAG_STATES as readonly string[]).includes(lower)
    ? (lower as ReviewFlagState)
    : "open";
}

/** A flag whose revision the pull request has moved past. */
export function isStale(flag: ReviewFlag): boolean {
  return normalizeState(flag?.state ?? "") === "stale";
}

/** Only `open` flags can be settled, and only settled ones reopened. */
export function isSettled(flag: ReviewFlag): boolean {
  const state = normalizeState(flag?.state ?? "");
  return state === "resolved" || state === "dismissed";
}

/**
 * `path:42` for a single line, `path:42-48` for a range. The label a reviewer
 * pastes into their editor, so the range has to survive it.
 */
export function flagLocation(flag: ReviewFlag): string {
  const path = flag?.file_path ?? "";
  const start = flag?.line_start ?? 0;
  const end = flag?.line_end ?? 0;
  if (!start) return path;
  return end > start ? `${path}:${start}-${end}` : `${path}:${start}`;
}

/**
 * Long repository paths would push the severity and the confidence off the
 * row, and the head of a path ("packages/views/…") is the least informative
 * part of it. Keep both ends and elide the middle.
 */
export function middleTruncatePath(path: string, max = 44): string {
  const value = path ?? "";
  if (value.length <= max) return value;
  const head = Math.ceil((max - 1) / 2);
  const tail = Math.floor((max - 1) / 2);
  return `${value.slice(0, head)}…${value.slice(value.length - tail)}`;
}

/** Total open findings — the number the section header answers with. */
export function totalOpen(counts: ReviewFlagCounts): number {
  return (counts?.bug ?? 0) + (counts?.warning ?? 0) + (counts?.info ?? 0);
}
