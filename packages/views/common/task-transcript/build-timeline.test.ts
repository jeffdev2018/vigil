// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { appendTimelineItem, buildTimeline, coalesceTimelineItems, type TimelineItem } from "./build-timeline";

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
