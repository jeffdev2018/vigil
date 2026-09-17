// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TaskMessagePayload } from "@multica/core/types/events";
import {
  appendTimelineItem,
  buildTimeline,
  coalesceTimelineItems,
  isOutputTruncated,
  type TimelineItem,
} from "./build-timeline";

function message(seq: number, type: TaskMessagePayload["type"], content?: string): TaskMessagePayload {
  return {
    task_id: "task-1",
    issue_id: "issue-1",
    seq,
    type,
    content,
  };
}

describe("task transcript timeline", () => {
  it("merges adjacent text and thinking fragments split by streaming flushes", () => {
    const items = buildTimeline([
      message(2, "text", "world"),
      message(1, "text", "hello "),
      message(3, "thinking", "step "),
      message(4, "thinking", "one"),
    ]);

    expect(items).toEqual([
      expect.objectContaining({ seq: 1, type: "text", content: "hello world" }),
      expect.objectContaining({ seq: 3, type: "thinking", content: "step one" }),
    ]);
  });

  it("does not merge across tool or error boundaries", () => {
    const items = coalesceTimelineItems([
      { seq: 1, type: "text", content: "before" },
      { seq: 2, type: "tool_use", tool: "bash" },
      { seq: 3, type: "text", content: "after" },
      { seq: 4, type: "error", content: "failed" },
      { seq: 5, type: "text", content: "done" },
    ]);

    expect(items.map((item) => item.content ?? item.tool)).toEqual([
      "before",
      "bash",
      "after",
      "failed",
      "done",
    ]);
  });

  it("coalesces newly appended live text with the previous text item", () => {
    const existing: TimelineItem[] = [{ seq: 1, type: "text", content: "hello" }];
    const items = appendTimelineItem(existing, { seq: 2, type: "text", content: " world" });

    expect(items).toEqual([
      expect.objectContaining({ seq: 1, type: "text", content: "hello world" }),
    ]);
  });

  it("coalesces out-of-order raw text by sequence", () => {
    const existing: TimelineItem[] = [
      { seq: 1, type: "text", content: "A" },
      { seq: 3, type: "text", content: "C" },
    ];
    const items = appendTimelineItem(existing, { seq: 2, type: "text", content: "B" });

    expect(items).toEqual([
      expect.objectContaining({ seq: 1, type: "text", content: "ABC" }),
    ]);
  });

  it("redacts secrets after adjacent chunks are coalesced", () => {
    const items = buildTimeline([
      message(1, "text", "Authorization: Bearer abc123xyz."),
      message(2, "text", "def456"),
    ]);

    expect(items[0]?.content).toBe("Authorization: Bearer [REDACTED]");
    expect(items[0]?.content).not.toContain("abc123xyz");
    expect(items[0]?.content).not.toContain("def456");
  });

  it("keeps the latest created_at when coalescing streaming fragments", () => {
    const items = coalesceTimelineItems([
      { seq: 1, type: "text", content: "hello ", created_at: "2026-06-09T09:00:00.000Z" },
      { seq: 2, type: "text", content: "world", created_at: "2026-06-09T09:00:05.000Z" },
    ]);

    expect(items).toEqual([
      expect.objectContaining({
        seq: 1,
        type: "text",
        content: "hello world",
        created_at: "2026-06-09T09:00:05.000Z",
      }),
    ]);
  });

  it("falls back to the previous created_at when the merged fragment has none", () => {
    const items = coalesceTimelineItems([
      { seq: 1, type: "text", content: "hello ", created_at: "2026-06-09T09:00:00.000Z" },
      { seq: 2, type: "text", content: "world" },
    ]);

    expect(items[0]?.created_at).toBe("2026-06-09T09:00:00.000Z");
  });
});

describe("tool output completeness", () => {
  function result(output: string | undefined, output_truncated?: boolean): TimelineItem {
    return { seq: 1, type: "tool_result", tool: "bash", output, output_truncated };
  }

  it("carries the server's truncation flag onto the timeline", () => {
    const items = buildTimeline([
      { ...message(1, "tool_result"), output: "cut here", output_truncated: true },
      { ...message(2, "tool_result"), output: "all of it", output_truncated: false },
      { ...message(3, "tool_result"), output: "who knows" },
    ]);

    expect(items.map((i) => i.output_truncated)).toEqual([true, false, undefined]);
  });

  // false is a measurement, undefined is the absence of one. Only a positive
  // measurement earns the per-step remark.
  it("marks only an output measured as truncated", () => {
    expect(isOutputTruncated(result("x", true))).toBe(true);
    expect(isOutputTruncated(result("x", false))).toBe(false);
    expect(isOutputTruncated(result("x"))).toBe(false);
  });

  // A truncated preview keeps the first 8 KiB, so an empty output cannot be one.
  it("says nothing about an empty output", () => {
    expect(isOutputTruncated(result("", true))).toBe(false);
    expect(isOutputTruncated(result(undefined, true))).toBe(false);
  });

  // The flag only describes tool output; prose and thinking have no preview
  // budget to overflow.
  it("ignores message types that have no tool output", () => {
    const others: TimelineItem[] = [
      { seq: 1, type: "text", content: "hello" },
      { seq: 2, type: "thinking", content: "hmm" },
      { seq: 3, type: "error", content: "boom" },
    ];
    expect(others.some(isOutputTruncated)).toBe(false);
  });
});

describe("reusing work across rebuilds", () => {
  it("reuses the item for a run whose messages have not changed", () => {
    // Redaction is the whole cost of building a timeline and a live run rebuilds
    // on every 100ms flush, so a message that has not changed must not be
    // scanned again (MUL-7227). Identity of the returned item is the observable
    // form of that: a fresh scan would produce a fresh object.
    const msgs = [message(1, "text", "hello"), message(2, "tool_result", "out")];

    const first = buildTimeline(msgs);
    const second = buildTimeline([...msgs]);

    expect(second[0]).toBe(first[0]);
    expect(second[1]).toBe(first[1]);
  });

  it("rebuilds a run that gained a fragment, so redaction still sees the merged text", () => {
    // The reuse key is the run's exact message list, precisely so a coalescing
    // run that grew is redacted again as one string. Split so neither half
    // matches alone: reusing the first half's item, or redacting the fragments
    // separately, would leave the key on screen.
    const first = message(1, "text", "key AKIA123456");
    const partial = buildTimeline([first]);
    expect(partial[0]?.content).toBe("key AKIA123456");

    const merged = buildTimeline([first, message(2, "text", "7890ABCDEF")]);

    expect(merged[0]?.content).toBe("key [REDACTED AWS KEY]");
    expect(merged[0]).not.toBe(partial[0]);
  });

  it("rebuilds when a message is replaced at the same seq", () => {
    // A fetch response is the authority and replaces what the cache held, so a
    // same-seq record with different content arrives as a different object.
    const before = buildTimeline([message(1, "text", "first body")]);
    const after = buildTimeline([message(1, "text", "second body")]);

    expect(before[0]?.content).toBe("first body");
    expect(after[0]?.content).toBe("second body");
  });
});

// ─── F03 · run actions folded into the timeline ─────────────────────────────

describe("buildTimeline with run actions", () => {
  const T0 = Date.parse("2026-08-15T10:00:00.000Z");
  const at = (s: number) => new Date(T0 + s * 1000).toISOString();
  const msg = (seq: number, seconds: number): TaskMessagePayload => ({
    task_id: "task-1",
    issue_id: "issue-1",
    seq,
    type: "text",
    content: `m${seq}`,
    created_at: at(seconds),
  });
  const action = (name: string, seconds: number) => ({
    kind: "action" as const,
    action: name,
    before: "todo",
    after: "done",
    at: at(seconds),
  });

  it("places an action after the message it followed in time", () => {
    const items = buildTimeline([msg(1, 0), msg(2, 10)], [action("status_changed", 5)]);
    expect(items.map((i) => i.type)).toEqual(["text", "action", "text"]);
  });

  it("keeps two actions sharing an anchor in server order", () => {
    const items = buildTimeline(
      [msg(1, 0), msg(2, 10)],
      [action("status_changed", 5), action("assignee_changed", 6)],
    );
    expect(items.map((i) => i.content)).toEqual(["m1", "status_changed", "assignee_changed", "m2"]);
  });

  it("carries before/after so the presenter can render the pair", () => {
    const items = buildTimeline([], [action("status_changed", 1)]);
    expect(items[0]!.input).toEqual({ action: "status_changed", before: "todo", after: "done" });
  });

  // An action with an unusable timestamp still happened. Showing it last beats
  // hiding a change the run made.
  it("appends an action with no usable timestamp rather than dropping it", () => {
    const items = buildTimeline(
      [msg(1, 0)],
      [{ kind: "action", action: "created", before: "", after: "", at: "" }],
    );
    expect(items.map((i) => i.type)).toEqual(["text", "action"]);
  });

  it("changes nothing when the run made no issue changes", () => {
    // Two adjacent text fragments still coalesce into one item, exactly as
    // before the actions argument existed.
    const withNone = buildTimeline([msg(1, 0), msg(2, 1)], []);
    expect(withNone.map((i) => i.type)).toEqual(["text"]);
    expect(buildTimeline([msg(1, 0), msg(2, 1)])).toEqual(withNone);
  });

  // The text/thinking coalescer keys on adjacency, and an action is not text,
  // so it both stays its own row AND stops the two fragments it sits between
  // from merging — which is what keeps the change readable in its place.
  it("never coalesces an action with the prose around it", () => {
    const items = buildTimeline([msg(1, 0), msg(2, 10)], [action("status_changed", 5)]);
    expect(items).toHaveLength(3);
    expect(items.map((i) => i.content)).toEqual(["m1", "status_changed", "m2"]);
  });
});

// ─── Living run plan (F04) ──────────────────────────────────────────────────

describe("plan messages in the timeline", () => {
  it("places a plan by the time it was published, not by its reserved seq", () => {
    const items = buildTimeline([
      { task_id: "t", seq: 1, type: "text", content: "starting", created_at: "2026-01-01T00:00:00Z" },
      // Server-allocated from a band far above the daemon's counter, so
      // sorting on it would pin the plan to the end of the transcript.
      {
        task_id: "t",
        seq: 1_000_001,
        type: "plan",
        content: "0/2 done",
        input: { items: [{ text: "a", status: "in_progress" }, { text: "b", status: "pending" }] },
        created_at: "2026-01-01T00:00:10Z",
      },
      { task_id: "t", seq: 2, type: "text", content: "later", created_at: "2026-01-01T00:00:20Z" },
    ] as never);

    expect(items.map((i) => i.type)).toEqual(["text", "plan", "text"]);
    // The checklist survives the trip: it is what the plan block renders.
    expect((items[1]?.input as { items: unknown[] }).items).toHaveLength(2);
  });

  it("does not let a plan's seq push a timestamp-less action past it", () => {
    const items = buildTimeline(
      [
        { task_id: "t", seq: 1, type: "text", content: "hello", created_at: "2026-01-01T00:00:00Z" },
        { task_id: "t", seq: 1_000_001, type: "plan", content: "0/1 done", created_at: "2026-01-01T00:00:05Z" },
      ] as never,
      [{ kind: "action", action: "status_changed", before: "todo", after: "in_progress", at: "" }],
    );

    // Both are slotted against the message stream, whose last seq is 1 — the
    // plan's 1,000,001 must never become the stream's maximum, or every
    // timestamp-less action would be dragged past it.
    expect(items.map((i) => i.type)).toEqual(["text", "plan", "action"]);
    for (const item of items) {
      expect(item.seq).toBeLessThan(3);
    }
  });
});
