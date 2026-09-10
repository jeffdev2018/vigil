// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TimelineEntry } from "@multica/core/types";
import {
  collectThreadParticipants,
  collectThreadReplies,
  matchesThreadFilter,
  mentionsUser,
  resolvedThreadRootIds,
  rootCommentIds,
  threadInvolvesUser,
} from "./thread-utils";

function comment(id: string, createdAt: string, parentId: string | null): TimelineEntry {
  return {
    type: "comment",
    id,
    actor_type: "member",
    actor_id: "user-1",
    content: id,
    parent_id: parentId,
    created_at: createdAt,
    updated_at: createdAt,
    comment_type: "comment",
  } as TimelineEntry;
}

function bucketByParent(entries: TimelineEntry[]): Map<string, TimelineEntry[]> {
  const map = new Map<string, TimelineEntry[]>();
  for (const e of entries) {
    if (!e.parent_id) continue;
    const list = map.get(e.parent_id) ?? [];
    list.push(e);
    map.set(e.parent_id, list);
  }
  return map;
}

describe("collectThreadReplies", () => {
  it("orders a late nested reply after earlier sibling replies (#3691)", () => {
    // R1 (50m ago) triggered a slow agent; R2 (30m) and R3 (10m) arrived while
    // it ran; D (3m ago) is the agent's reply, forced to nest under R1. A
    // depth-first walk yields R1-D-R2-R3; the thread must read R1-R2-R3-D.
    const r1 = comment("r1", "2026-06-11T10:00:00Z", "root");
    const r2 = comment("r2", "2026-06-11T10:20:00Z", "root");
    const r3 = comment("r3", "2026-06-11T10:40:00Z", "root");
    const d = comment("d", "2026-06-11T10:47:00Z", "r1");

    const out = collectThreadReplies("root", bucketByParent([r1, r2, r3, d]));

    expect(out.map((e) => e.id)).toEqual(["r1", "r2", "r3", "d"]);
  });

  it("still returns every descendant across nesting levels", () => {
    const r1 = comment("r1", "2026-06-11T10:00:00Z", "root");
    const d1 = comment("d1", "2026-06-11T10:05:00Z", "r1");
    const d2 = comment("d2", "2026-06-11T10:10:00Z", "d1");

    const out = collectThreadReplies("root", bucketByParent([r1, d1, d2]));

    expect(out.map((e) => e.id)).toEqual(["r1", "d1", "d2"]);
  });

  it("breaks created_at ties by id so the order is deterministic", () => {
    const b = comment("b", "2026-06-11T10:00:00Z", "root");
    const a = comment("a", "2026-06-11T10:00:00Z", "b");

    const out = collectThreadReplies("root", bucketByParent([b, a]));

    expect(out.map((e) => e.id)).toEqual(["a", "b"]);
  });
});

function activity(id: string, createdAt: string): TimelineEntry {
  return {
    type: "activity",
    id,
    actor_type: "member",
    actor_id: "user-1",
    action: "status_changed",
    created_at: createdAt,
  } as TimelineEntry;
}

describe("rootCommentIds", () => {
  it("returns top-level comments only, skipping replies and activities", () => {
    const entries = [
      activity("act-1", "2026-06-11T09:00:00Z"),
      comment("root-1", "2026-06-11T10:00:00Z", null),
      comment("reply-1", "2026-06-11T10:05:00Z", "root-1"),
      comment("root-2", "2026-06-11T11:00:00Z", null),
    ];

    expect(rootCommentIds(entries)).toEqual(["root-1", "root-2"]);
  });
});

describe("resolvedThreadRootIds", () => {
  it("includes root-resolved and reply-resolved threads, excludes unresolved", () => {
    const rootResolved = {
      ...comment("root-resolved", "2026-06-11T10:00:00Z", null),
      resolved_at: "2026-06-11T12:00:00Z",
    };
    const replyResolvedRoot = comment("root-reply-resolved", "2026-06-11T10:10:00Z", null);
    const resolutionReply = {
      ...comment("reply-resolution", "2026-06-11T10:20:00Z", "root-reply-resolved"),
      resolved_at: "2026-06-11T12:30:00Z",
    };
    const openRoot = comment("root-open", "2026-06-11T10:30:00Z", null);
    const openReply = comment("reply-open", "2026-06-11T10:40:00Z", "root-open");

    const ids = resolvedThreadRootIds([
      activity("act-1", "2026-06-11T09:00:00Z"),
      rootResolved,
      replyResolvedRoot,
      resolutionReply,
      openRoot,
      openReply,
    ]);

    expect(ids).toEqual(["root-resolved", "root-reply-resolved"]);
  });

  it("detects a resolution on a nested reply", () => {
    const root = comment("root", "2026-06-11T10:00:00Z", null);
    const reply = comment("reply", "2026-06-11T10:05:00Z", "root");
    const nested = {
      ...comment("nested", "2026-06-11T10:10:00Z", "reply"),
      resolved_at: "2026-06-11T12:00:00Z",
    };

    expect(resolvedThreadRootIds([root, reply, nested])).toEqual(["root"]);
  });
});

describe("collectThreadParticipants", () => {
  it("includes nested member and agent authors once, preserving identity and first-seen order", () => {
    const root = {
      ...comment("root", "2026-06-11T09:00:00Z", null),
      actor_name: "Alice",
      actor_avatar_url: "https://example.com/alice.png",
    };
    const repeat = comment("repeat", "2026-06-11T10:00:00Z", "root");
    const agent = { ...comment("agent", "2026-06-11T10:01:00Z", "repeat"), actor_type: "agent" };
    const member = { ...comment("member", "2026-06-11T10:02:00Z", "agent"), actor_id: "user-2" };
    const system = { ...comment("system", "2026-06-11T10:03:00Z", "root"), actor_type: "system" };
    const replies = collectThreadReplies("root", bucketByParent([repeat, agent, member, system]));
    expect(collectThreadParticipants(root, replies)).toEqual([root, agent, member]);
  });
});

describe("matchesThreadFilter", () => {
  const cases: [boolean, boolean, Record<string, boolean>][] = [
    [false, false, { all: true, unresolved: true, resolved: false, mine: false }],
    [true, false, { all: true, unresolved: false, resolved: true, mine: false }],
    [false, true, { all: true, unresolved: true, resolved: false, mine: true }],
    [true, true, { all: true, unresolved: false, resolved: true, mine: true }],
  ];
  it.each(cases)(
    "resolved=%s involvesMe=%s selects the right pills",
    (resolved, involvesMe, expected) => {
      for (const [filter, want] of Object.entries(expected)) {
        expect(
          matchesThreadFilter({ resolved, involvesMe }, filter as "all"),
        ).toBe(want);
      }
    },
  );

  it("shows everything rather than nothing for a filter it does not know", () => {
    // A pill added later must not silently empty the outline before its case
    // is written.
    expect(
      matchesThreadFilter({ resolved: true, involvesMe: false }, "future" as "all"),
    ).toBe(true);
  });
});

describe("mentionsUser", () => {
  const me = "11111111-1111-4111-8111-111111111111";

  it("matches the link form and the legacy shortcode still sitting in the database", () => {
    expect(mentionsUser(`hi [Ann](mention://member/${me}) here`, me)).toBe(true);
    expect(mentionsUser(`hi [@ id="${me}" label="Ann"] here`, me)).toBe(true);
  });

  it("does not match another member, empty content, or an empty reader", () => {
    expect(mentionsUser("hi [Bo](mention://member/22222222-2222-4222-8222-222222222222)", me)).toBe(false);
    expect(mentionsUser(undefined, me)).toBe(false);
    expect(mentionsUser("", me)).toBe(false);
    expect(mentionsUser(`[Ann](mention://member/${me})`, "")).toBe(false);
  });
});

describe("threadInvolvesUser", () => {
  const me = "11111111-1111-4111-8111-111111111111";
  const other = "22222222-2222-4222-8222-222222222222";

  function entry(actorType: string, actorId: string, content: string): TimelineEntry {
    return {
      ...comment("e", "2026-09-01T00:00:00Z", null),
      actor_type: actorType,
      actor_id: actorId,
      content,
    } as TimelineEntry;
  }

  it("counts authorship of the root, authorship of a reply, and a mention anywhere", () => {
    const mine = entry("member", me, "x");
    const theirs = entry("member", other, "x");
    expect(threadInvolvesUser(mine, [], me)).toBe(true);
    expect(threadInvolvesUser(theirs, [mine], me)).toBe(true);
    expect(
      threadInvolvesUser(theirs, [entry("agent", "a1", `cc [Ann](mention://member/${me})`)], me),
    ).toBe(true);
  });

  it("does not count an agent whose id happens to equal the reader's", () => {
    // actor_id is only unique within an actor type, so the type has to be
    // part of the comparison.
    expect(threadInvolvesUser(entry("agent", me, "x"), [], me)).toBe(false);
  });

  it("is false for a thread the reader never touched, and for an anonymous reader", () => {
    const theirs = entry("member", other, "x");
    expect(threadInvolvesUser(theirs, [entry("member", other, "y")], me)).toBe(false);
    expect(threadInvolvesUser(entry("member", me, "x"), [], "")).toBe(false);
  });
});
