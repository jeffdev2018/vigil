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
  content?: string;
  input?: Record<string, unknown>;
  output?: string;
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
 */
export function buildTimeline(msgs: TaskMessagePayload[], actions: RunAction[] = []): TimelineItem[] {
  // Plan messages are pulled out of the stream before anything anchors on it:
  // their reserved-band seq would otherwise become the stream's maxSeq and drag
  // every timestamp-less action to sort after them.
  const stream = msgs.filter((msg) => msg.type !== PLAN_MESSAGE_TYPE);
  const plans = msgs.filter((msg) => msg.type === PLAN_MESSAGE_TYPE);

  const items: TimelineItem[] = [];
  for (const msg of stream) {
    items.push({
      seq: msg.seq,
      type: msg.type,
      tool: msg.tool,
      content: msg.content,
      input: msg.input,
      output: msg.output,
      created_at: msg.created_at,
    });
  }
  // One slotting pass for both kinds, so a plan and an action published at the
  // same moment cannot be handed the same fractional seq.
  items.push(...slotByTimestamp([...planEntries(plans), ...actionEntries(actions)], stream));
  return redactTimelineItems(coalesceTimelineItems(items));
}
