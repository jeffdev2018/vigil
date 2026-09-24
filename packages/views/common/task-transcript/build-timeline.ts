import type { RunAction, TaskMessagePayload } from "@multica/core/types/events";
import { redactSecrets } from "./redact";

/** A unified timeline entry: tool calls, thinking, text, the run's final
 *  response, the issue changes it made, and errors, in chronological order. */
export interface TimelineItem {
  seq: number;
  /**
   * Open, mirroring TaskMessagePayload.type. The server validates no
   * allow-list on ingest, so an installed build meets types it predates; the
   * presenter renders an unrecognised one as a neutral note rather than
   * dropping the evidence.
   */
  type:
    | "tool_use"
    | "tool_result"
    | "thinking"
    | "text"
    | "error"
    | "response"
    | "action"
    | "elicitation"
    | (string & {});
  tool?: string;
  /** Opaque identity for pairing tool events within a backend execution. */
  callId?: string;
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
  /**
   * Whether the stored `output` dropped bytes at the source (`tool_result`
   * only). `undefined` means unknown — the record predates the flag or came
   * from an older daemon — and must never be rendered as "complete".
   */
  output_truncated?: boolean;
  created_at?: string;
}

function canMergeStreamingText(prev: TimelineItem, next: TimelineItem): boolean {
  return (prev.type === "thinking" || prev.type === "text") && prev.type === next.type;
}

/** Merge adjacent text/thinking fragments that were split only by daemon flush timing. */
export function coalesceTimelineItems(items: TimelineItem[]): TimelineItem[] {
  const sorted = [...items].sort((a, b) => a.seq - b.seq);
  const out: TimelineItem[] = [];

  for (const item of sorted) {
    const prev = out[out.length - 1];
    if (prev && canMergeStreamingText(prev, item)) {
      out[out.length - 1] = {
        ...prev,
        content: `${prev.content ?? ""}${item.content ?? ""}`,
        created_at: item.created_at ?? prev.created_at,
      };
      continue;
    }
    out.push(item);
  }

  return out;
}

export function appendTimelineItem(items: TimelineItem[], item: TimelineItem): TimelineItem[] {
  return coalesceTimelineItems([...items, item]);
}

function redactTimelineItems(items: TimelineItem[]): TimelineItem[] {
  return items.map((item) => ({
    ...item,
    content: item.content ? redactSecrets(item.content) : item.content,
    output: item.output ? redactSecrets(item.output) : item.output,
  }));
}

/**
 * Whether this record's stored output is known to have dropped bytes.
 *
 * Only tool results carry the measurement, and an empty output has nothing to
 * be missing — truncation keeps the first 8 KiB, so a preview that dropped
 * bytes is never empty.
 */
export function isOutputTruncated(item: TimelineItem): boolean {
  return (
    item.type === "tool_result" && (item.output?.length ?? 0) > 0 && item.output_truncated === true
  );
}

/**
 * Timeline items already built, keyed on the first message behind each one.
 *
 * Redaction is essentially the whole cost of building a timeline: coalescing a
 * 3000-message transcript takes ~0.1ms and redacting it ~20ms, because every
 * rule scans every byte of every body. A live run rebuilds its timeline on each
 * 100ms flush window, so that scan was repeating over the entire transcript
 * several times a second while all but its newest messages were byte for byte
 * what they had been on the previous pass (MUL-7227).
 *
 * A message is immutable once it lands — the realtime merge appends unseen seqs
 * and keeps the objects it already holds — so identity is a sound proof that a
 * body has not changed. Each entry records the exact messages it was built
 * from, and is reused only when that list still matches: a coalescing run that
 * gained a fragment is rebuilt, which is what keeps redaction applied to merged
 * text rather than to the pieces.
 *
 * Weak, so entries are collected with the messages they belong to. They hold
 * little: `String.replace` returns its input unchanged when nothing matches, so
 * a body with no secrets in it is shared rather than copied.
 */
const builtRuns = new WeakMap<
  TaskMessagePayload,
  { members: readonly TaskMessagePayload[]; item: TimelineItem }
>();

function sameMembers(a: readonly TaskMessagePayload[], b: readonly TaskMessagePayload[]): boolean {
  if (a.length !== b.length) return false;
  return a.every((message, index) => message === b[index]);
}

/** Merge one coalescing run into its item, exactly as `coalesceTimelineItems` would. */
function mergeRun(run: readonly TaskMessagePayload[]): TimelineItem {
  const first = run[0]!;
  let content = first.content;
  let createdAt = first.created_at;
  for (const message of run.slice(1)) {
    content = `${content ?? ""}${message.content ?? ""}`;
    createdAt = message.created_at ?? createdAt;
  }
  return {
    seq: first.seq,
    type: first.type,
    tool: first.tool,
    callId: first.call_id,
    content,
    input: first.input,
    output: first.output,
    output_truncated: first.output_truncated,
    created_at: createdAt,
  };
}

function buildRun(run: readonly TaskMessagePayload[]): TimelineItem {
  const cached = builtRuns.get(run[0]!);
  if (cached && sameMembers(cached.members, run)) return cached.item;
  const item = redactTimelineItems([mergeRun(run)])[0]!;
  builtRuns.set(run[0]!, { members: [...run], item });
  return item;
}

/** The run's living plan (F04), which is placed by time rather than by seq. */
export const PLAN_MESSAGE_TYPE = "plan";

/** An entry with no usable seq of its own, waiting to be slotted by time. */
interface UnsequencedEntry {
  at?: string;
  item: Omit<TimelineItem, "seq">;
}

/**
 * Place entries that carry no usable seq among the run's messages.
 *
 * Two kinds need this. Actions come from activity_log and have no seq at all.
 * Plan messages DO have one, but it is drawn from a reserved band far above the
 * daemon's per-run counter so the two writers can never collide (see
 * CreateTaskPlanMessage) — sorting on it would pin every plan to the end of the
 * transcript instead of the moment it was published.
 *
 * Each entry is slotted just after the last message that precedes it in time,
 * using a fractional seq so it sorts into place without colliding with a real
 * message or with a sibling sharing the same anchor. Fractions are safe here
 * because seq is only ever compared and sorted on this side; the merge-by-seq
 * cache never sees these items.
 *
 * An entry with no usable timestamp lands at the end rather than being
 * dropped — it did happen, and hiding it is worse than showing it out of order.
 */
function slotByTimestamp(
  entries: UnsequencedEntry[],
  msgs: TaskMessagePayload[],
): TimelineItem[] {
  if (entries.length === 0) return [];
  const anchors = msgs
    .map((m) => ({ seq: m.seq, at: m.created_at ? Date.parse(m.created_at) : Number.NaN }))
    .filter((a) => Number.isFinite(a.at))
    .sort((a, b) => a.at - b.at);
  const maxSeq = msgs.reduce((max, m) => Math.max(max, m.seq), 0);

  const perAnchor = new Map<number, number>();
  // Oldest first, so two entries sharing an anchor keep their real order.
  // Entries with no timestamp stay last, in the order they came in.
  const ordered = entries
    .map((entry, index) => ({ entry, index, at: entry.at ? Date.parse(entry.at) : Number.NaN }))
    .sort((a, b) => {
      const aKnown = Number.isFinite(a.at);
      const bKnown = Number.isFinite(b.at);
      if (aKnown !== bKnown) return aKnown ? -1 : 1;
      if (aKnown && a.at !== b.at) return a.at - b.at;
      return a.index - b.index;
    });

  return ordered.map(({ entry, at }) => {
    let base = maxSeq;
    if (Number.isFinite(at)) {
      base = 0;
      for (const anchor of anchors) {
        if (anchor.at <= at) base = anchor.seq;
        else break;
      }
    }
    const nth = (perAnchor.get(base) ?? 0) + 1;
    perAnchor.set(base, nth);
    return {
      // 1/(nth+1) keeps every entry strictly between its anchor and the next
      // integer seq, in the order resolved above.
      seq: base + 1 - 1 / (nth + 1),
      ...entry.item,
    };
  });
}

function actionEntries(actions: RunAction[]): UnsequencedEntry[] {
  return actions.map((action) => ({
    at: action.at || undefined,
    item: {
      type: "action" as const,
      content: action.action,
      input: { action: action.action, before: action.before, after: action.after },
      created_at: action.at || undefined,
    },
  }));
}

function planEntries(plans: TaskMessagePayload[]): UnsequencedEntry[] {
  return plans.map((msg) => ({
    at: msg.created_at || undefined,
    item: {
      type: msg.type,
      tool: msg.tool,
      content: msg.content,
      input: msg.input,
      output: msg.output,
      created_at: msg.created_at,
    },
  }));
}

/**
 * Build a chronologically ordered timeline from raw task messages, optionally
 * interleaving the issue changes the run made.
 *
 * Coalescing runs are reused across rebuilds (see `builtRuns`); the plan and
 * action entries slotted in afterwards are redacted on every pass, because
 * there are a handful of them next to a transcript of thousands of messages.
 */
export function buildTimeline(msgs: TaskMessagePayload[], actions: RunAction[] = []): TimelineItem[] {
  // Plan messages are pulled out of the stream before anything anchors on it:
  // their reserved-band seq would otherwise become the stream's maxSeq and drag
  // every timestamp-less action to sort after them.
  const stream = msgs.filter((msg) => msg.type !== PLAN_MESSAGE_TYPE);
  const plans = msgs.filter((msg) => msg.type === PLAN_MESSAGE_TYPE);

  // One slotting pass for both kinds, so a plan and an action published at the
  // same moment cannot be handed the same fractional seq. Slotted first,
  // because a plan or an action landing between two prose fragments has to stop
  // them merging over it — that is what keeps the change readable in its place.
  const slotted = redactTimelineItems(
    slotByTimestamp([...planEntries(plans), ...actionEntries(actions)], stream),
  );
  // A slotted seq sits strictly between its anchor message and the next
  // integer, so the anchor is its floor.
  const anchorsWithSlots = new Set(slotted.map((item) => Math.floor(item.seq)));

  const sorted = [...stream].sort((a, b) => a.seq - b.seq);
  const out: TimelineItem[] = [];
  let run: TaskMessagePayload[] = [];

  for (const msg of sorted) {
    const previous = run[run.length - 1];
    // Same rule as `canMergeStreamingText`, read off the messages: a run's type
    // is its first message's, and every member shares it.
    if (
      previous &&
      (previous.type === "text" || previous.type === "thinking") &&
      previous.type === msg.type &&
      !anchorsWithSlots.has(previous.seq)
    ) {
      run.push(msg);
      continue;
    }
    if (run.length > 0) out.push(buildRun(run));
    run = [msg];
  }
  if (run.length > 0) out.push(buildRun(run));

  if (slotted.length === 0) return out;
  // The sort is stable, so entries sharing an anchor keep the order resolved
  // by `slotByTimestamp`.
  return [...out, ...slotted].sort((a, b) => a.seq - b.seq);
}
