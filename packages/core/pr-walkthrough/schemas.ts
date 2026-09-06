import { z } from "zod";

// Narrative pull request walkthrough (F05 / JEF-16).
//
// A reviewer opening a pull request gets a file list sorted by a tool that does
// not know which files matter. A read-only agent run reads the diff and tells
// the story instead: ordered groups, each with a rationale and a per-hunk
// explanation anchored to the lines the reviewer is looking at.
//
// The walkthrough belongs to ONE revision of the diff. When the head moves the
// old narrative is not shown — it describes a change that is no longer there.

/** Render order. `core` is what must be read; `noise` is what may be skipped. */
export const PR_WALKTHROUGH_KINDS = ["core", "test", "generated", "noise"] as const;
export type PrWalkthroughKind = (typeof PR_WALKTHROUGH_KINDS)[number];

export type PrWalkthroughState = "pending" | "ready" | "failed";

// Enums stay `z.string()` with a safe `.catch()` so an unknown value from a
// newer server still parses; the UI switches carry a default branch.
export const PrWalkthroughHunkSchema = z.object({
  old_start: z.number().catch(0).default(0),
  new_start: z.number().catch(0).default(0),
  lines: z.string().catch(""),
  explanation: z.string().catch(""),
  moved_from: z.string().catch(""),
}).loose();

export const PrWalkthroughFileSchema = z.object({
  path: z.string().catch(""),
  hunks: z.array(PrWalkthroughHunkSchema).catch([]).default([]),
}).loose();

export const PrWalkthroughGroupSchema = z.object({
  title: z.string().catch(""),
  kind: z.string().catch("noise"),
  rationale: z.string().catch(""),
  files: z.array(PrWalkthroughFileSchema).catch([]).default([]),
}).loose();

export const PrWalkthroughSchema = z.object({
  state: z.string().catch("pending"),
  head_sha: z.string().catch(""),
  truncated: z.boolean().catch(false),
  omitted_files: z.number().catch(0).default(0),
  groups: z.array(PrWalkthroughGroupSchema).catch([]).default([]),
  generated_at: z.string().catch(""),
  error: z.string().catch(""),
}).loose();

export const PrWalkthroughRefreshSchema = z.object({
  task_id: z.string().catch(""),
}).loose();

export const PrWalkthroughSettingsSchema = z.object({
  enabled: z.boolean().catch(false),
  agent_id: z.string().catch(""),
}).loose();

export interface PrWalkthroughHunk {
  old_start: number;
  new_start: number;
  lines: string;
  explanation: string;
  /** Set when this hunk is a move or rename, so a reviewer can skip it. */
  moved_from: string;
}

export interface PrWalkthroughFile {
  path: string;
  hunks: PrWalkthroughHunk[];
}

export interface PrWalkthroughGroup {
  title: string;
  /** Open string: a newer server may send a kind this build does not know. */
  kind: string;
  rationale: string;
  files: PrWalkthroughFile[];
}

export interface PrWalkthrough {
  state: string;
  head_sha: string;
  /** The diff was over the size cap, so the narrative covers only part of it. */
  truncated: boolean;
  omitted_files: number;
  groups: PrWalkthroughGroup[];
  generated_at: string;
  error: string;
}

export interface PrWalkthroughSettings {
  enabled: boolean;
  agent_id: string;
}

/** No row yet is a real answer, not an error: a run is (or will be) out. */
export const EMPTY_PR_WALKTHROUGH: PrWalkthrough = {
  state: "pending",
  head_sha: "",
  truncated: false,
  omitted_files: 0,
  groups: [],
  generated_at: "",
  error: "",
};

export const PR_WALKTHROUGH_DEFAULT_SETTINGS: PrWalkthroughSettings = {
  enabled: false,
  agent_id: "",
};

/**
 * The server already stores groups in render order, but a response from a
 * newer or older build may not, and an unknown kind must not silently jump the
 * queue ahead of `core`. Sorting here is what makes the order a guarantee of
 * this component rather than of whatever wrote the row.
 */
export function orderedGroups(groups: PrWalkthroughGroup[]): PrWalkthroughGroup[] {
  const rank = (kind: string) => {
    const i = (PR_WALKTHROUGH_KINDS as readonly string[]).indexOf(normalizeKind(kind));
    return i < 0 ? PR_WALKTHROUGH_KINDS.length : i;
  };
  return [...(groups ?? [])].sort((a, b) => rank(a?.kind ?? "") - rank(b?.kind ?? ""));
}

/** An unknown kind reads as `noise` — skippable, never missing. */
export function normalizeKind(kind: string): PrWalkthroughKind {
  const lower = (kind ?? "").trim().toLowerCase();
  return (PR_WALKTHROUGH_KINDS as readonly string[]).includes(lower)
    ? (lower as PrWalkthroughKind)
    : "noise";
}

/**
 * Long repository paths would push the hunk count off the row, and the head of
 * a path ("packages/views/…") is the least informative part of it. Keep both
 * ends and elide the middle.
 */
export function middleTruncate(path: string, max = 48): string {
  const value = path ?? "";
  if (value.length <= max) return value;
  const head = Math.ceil((max - 1) / 2);
  const tail = Math.floor((max - 1) / 2);
  return `${value.slice(0, head)}…${value.slice(value.length - tail)}`;
}

/** Total hunks a group explains — the honest size of one chapter. */
export function groupHunkCount(group: PrWalkthroughGroup): number {
  return (group?.files ?? []).reduce((n, file) => n + (file?.hunks?.length ?? 0), 0);
}

// ---------------------------------------------------------------------------
// Diff anchoring (F07 / JEF-21)
// ---------------------------------------------------------------------------

/** One rendered line of a hunk body, with the numbers a reader can anchor to. */
export interface HunkLine {
  /** The raw diff line, marker included. */
  text: string;
  kind: "add" | "del" | "context" | "meta";
  /** Line number on the old side, or 0 when this line is not on it. */
  oldLine: number;
  /** Line number on the new side, or 0 when this line is not on it. */
  newLine: number;
}

const HUNK_HEADER = /^@@\s*-(\d+)(?:,\d+)?\s*\+(\d+)(?:,\d+)?\s*@@/;

/**
 * Split a hunk body into lines carrying their real file line numbers, so a
 * reader can point at "line 41 of the new side" rather than "the 5th row of
 * this block".
 *
 * The counters start at the hunk's declared old_start / new_start and are RESET
 * by any `@@ … @@` header inside the body — the walkthrough stores the header
 * as part of `lines`, and a body that concatenates several hunks would
 * otherwise number everything after the first one wrong.
 *
 * A `\ No newline at end of file` marker is `meta`: it belongs to neither side
 * and must not consume a line number.
 */
export function hunkLines(hunk: PrWalkthroughHunk): HunkLine[] {
  const body = hunk?.lines ?? "";
  if (!body) return [];
  let oldNo = Math.max(1, hunk?.old_start ?? 1);
  let newNo = Math.max(1, hunk?.new_start ?? 1);
  const out: HunkLine[] = [];
  for (const text of body.split("\n")) {
    const header = HUNK_HEADER.exec(text);
    if (header) {
      oldNo = Number(header[1]);
      newNo = Number(header[2]);
      out.push({ text, kind: "meta", oldLine: 0, newLine: 0 });
      continue;
    }
    if (text.startsWith("+")) {
      out.push({ text, kind: "add", oldLine: 0, newLine: newNo++ });
    } else if (text.startsWith("-")) {
      out.push({ text, kind: "del", oldLine: oldNo++, newLine: 0 });
    } else if (text.startsWith("\\")) {
      out.push({ text, kind: "meta", oldLine: 0, newLine: 0 });
    } else {
      out.push({ text, kind: "context", oldLine: oldNo++, newLine: newNo++ });
    }
  }
  return out;
}

/**
 * Which side and line number a question about `line` is about. A deletion can
 * only be discussed on the old side; everything else reads on the new one,
 * which is the code that will be merged.
 */
export function anchorForLine(line: HunkLine): { side: "old" | "new"; line: number } | null {
  if (line.kind === "del") return line.oldLine > 0 ? { side: "old", line: line.oldLine } : null;
  if (line.kind === "meta") return null;
  return line.newLine > 0 ? { side: "new", line: line.newLine } : null;
}

/**
 * Does an anchor point INTO this hunk? Used to decide which hunk a thread
 * renders under. A thread anchored to a file the walkthrough does not cover
 * falls through to the timeline alone.
 */
export function hunkContainsAnchor(
  lines: HunkLine[],
  side: string,
  lineStart: number,
  lineEnd: number,
): boolean {
  const end = Math.max(lineStart, lineEnd || lineStart);
  return lines.some((l) => {
    const at = side === "old" ? l.oldLine : l.newLine;
    return at > 0 && at >= lineStart && at <= end;
  });
}
