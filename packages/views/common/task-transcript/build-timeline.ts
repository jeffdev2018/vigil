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

/**
 * Place the run's issue changes among its messages.
 *
 * Actions come from activity_log, not the message stream, so they carry no
 * seq. Each one is slotted just after the last message that precedes it in
 * time, using a fractional seq so it sorts into place without colliding with a
 * real message or with a sibling action sharing the same anchor. Fractions are
 * safe here because seq is only ever compared and sorted on this side; the
 * merge-by-seq cache never sees these items.
 *
 * An action with no usable timestamp lands at the end rather than being
 * dropped — the run did make the change, and hiding it is worse than showing
 * it out of order.
 */
function actionsToTimelineItems(actions: RunAction[], msgs: TaskMessagePayload[]): TimelineItem[] {
  if (actions.length === 0) return [];
  const anchors = msgs
    .map((m) => ({ seq: m.seq, at: m.created_at ? Date.parse(m.created_at) : Number.NaN }))
    .filter((a) => Number.isFinite(a.at))
    .sort((a, b) => a.at - b.at);
  const maxSeq = msgs.reduce((max, m) => Math.max(max, m.seq), 0);

  const perAnchor = new Map<number, number>();
  return actions.map((action) => {
    const at = action.at ? Date.parse(action.at) : Number.NaN;
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
      // 1/(nth+1) keeps every action strictly between its anchor and the next
      // integer seq, in the order the server returned them.
      seq: base + 1 - 1 / (nth + 1),
      type: "action" as const,
      content: action.action,
      input: { action: action.action, before: action.before, after: action.after },
      created_at: action.at || undefined,
    };
  });
}

/**
 * Build a chronologically ordered timeline from raw task messages, optionally
 * interleaving the issue changes the run made.
 */
export function buildTimeline(msgs: TaskMessagePayload[], actions: RunAction[] = []): TimelineItem[] {
  const items: TimelineItem[] = [];
  for (const msg of msgs) {
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
  items.push(...actionsToTimelineItems(actions, msgs));
  return redactTimelineItems(coalesceTimelineItems(items));
}
