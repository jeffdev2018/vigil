import { describe, expect, it } from "vitest";
import { InboxListSchema, InboxUnreadSummarySchema } from "./schemas";

/**
 * Tests for mobile's CLIENT-SIDE parsing of GET /api/inbox.
 *
 * Scope, stated precisely because the name of this file used to overclaim:
 * these are hand-written fixtures run against `InboxListSchema`. They pin how
 * this client REACTS to a given payload. They cannot fail when the Go server
 * starts sending something new — nothing here executes server code.
 *
 * The upstream notification listeners type every `details` map as
 * `map[string]string`, but newer server paths (goal-loop questions,
 * confidence reviews) marshal numbers and nested objects into it. During
 * MUL-5483 a NUMBER in `details.child_count` failed the strict
 * `z.record(z.string(), z.string())`, and because the endpoint parsed an
 * ARRAY, one bad row failed the whole parse and `listInbox` fell back to
 * `EMPTY_INBOX_LIST`: the entire mobile inbox rendered empty. The schema now
 * coerces non-string values and parses row by row; these tests pin that.
 */
describe("inbox list schema", () => {
  it("parses a row shaped like the documented server payload", () => {
    const serverRow = {
      id: "inbox-1",
      workspace_id: "ws-1",
      recipient_type: "member",
      recipient_id: "user-1",
      type: "status_changed",
      severity: "info",
      issue_id: "issue-1",
      title: "P0: delegated subscription rule",
      body: "",
      actor_type: "agent",
      actor_id: "agent-1",
      read: false,
      archived: false,
      created_at: "2026-07-30T00:00:00Z",
      // Every value is a string. A number here drops the whole list.
      details: { from: "in_progress", to: "in_review" },
    };

    const parsed = InboxListSchema.safeParse([serverRow]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.type).toBe("status_changed");
    expect(parsed.success && parsed.data[0]?.details?.to).toBe("in_review");
  });

  it("coerces non-string details values instead of rejecting the row", () => {
    // Goal-loop questions carry a nested `question` object and confidence
    // reviews carry numbers; before this, one such row blanked the inbox.
    const rows = [
      {
        id: "inbox-2",
        recipient_type: "member",
        type: "confidence_review",
        details: { child_count: 3 },
      },
      {
        id: "inbox-2b",
        recipient_type: "member",
        type: "goal_question",
        details: { goal_id: "g1", question: { prompt: "Which DB?", options: ["a", "b"] } },
      },
    ];

    const parsed = InboxListSchema.safeParse(rows);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.details?.child_count).toBe("3");
    expect(parsed.success && parsed.data[1]?.details?.goal_id).toBe("g1");
    expect(parsed.success && parsed.data[1]?.details?.question).toBe(
      JSON.stringify({ prompt: "Which DB?", options: ["a", "b"] }),
    );
  });

  it("drops a malformed row without emptying the list", () => {
    // The blast radius that made this a P1: the schema is an array, so a
    // single unreadable row used to invalidate every good one.
    const good = {
      id: "inbox-3",
      recipient_type: "member",
      type: "status_changed",
      details: { from: "todo", to: "in_review" },
    };
    const bad = { recipient_type: "member", type: "status_changed" }; // no id

    const parsed = InboxListSchema.safeParse([good, bad]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data.map((r) => r.id)).toEqual(["inbox-3"]);
  });

  it("renders an unknown server type instead of dropping the row", () => {
    // Mirrors the root CLAUDE.md API-compatibility rule and mobile's own
    // "render every inbox type, never silently drop a category" parity rule: a
    // type this build has never heard of must still parse.
    const future = {
      id: "inbox-5",
      recipient_type: "member",
      type: "some_future_type",
      details: { anything: "still a string" },
    };

    const parsed = InboxListSchema.safeParse([future]);
    expect(parsed.success).toBe(true);
  });
});

/**
 * GET /api/inbox/unread-summary — the source of the inbox tab badge.
 *
 * Blast radius differs from the list above: `getInboxUnreadSummary` falls back
 * to an empty array, which reads as "nothing unread" and simply hides the
 * badge. A wrong number would be worse than no number, so the schema stays
 * strict about the shape and lenient only about extra fields.
 */
describe("inbox unread summary schema", () => {
  it("parses the documented server payload", () => {
    const parsed = InboxUnreadSummarySchema.safeParse([
      { workspace_id: "ws-1", count: 3 },
      { workspace_id: "ws-2", count: 1 },
    ]);

    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[1]?.count).toBe(1);
  });

  it("passes through a field this client does not know yet", () => {
    const parsed = InboxUnreadSummarySchema.safeParse([
      { workspace_id: "ws-1", count: 3, unread_mentions: 2 },
    ]);

    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.count).toBe(3);
  });

  it("reads a non-numeric count as zero rather than failing the list", () => {
    // One malformed row must not blank every other workspace's count.
    const parsed = InboxUnreadSummarySchema.safeParse([
      { workspace_id: "ws-1", count: "many" },
      { workspace_id: "ws-2", count: 5 },
    ]);

    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.count).toBe(0);
    expect(parsed.success && parsed.data[1]?.count).toBe(5);
  });

  it("rejects a row with no workspace id", () => {
    // Without an id the entry can never be matched to a workspace, so it would
    // silently contribute nothing — fail loudly into the empty fallback.
    expect(
      InboxUnreadSummarySchema.safeParse([{ count: 3 }]).success,
    ).toBe(false);
  });
});
