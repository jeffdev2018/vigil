// @vitest-environment node
import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  applyChatParticipantEvent,
  isSoloChatSession,
  participantName,
  pruneTypists,
  resolveMessageAuthor,
  TYPING_EXPIRY_MS,
} from "./participants";
import { chatKeys } from "./queries";
import { parseWithFallback } from "../api/schema";
import {
  ChatParticipantListSchema,
  EMPTY_CHAT_PARTICIPANT_LIST,
} from "../api/schemas";
import type { ChatParticipant, ChatParticipantList } from "../types";

function participant(over: Partial<ChatParticipant> = {}): ChatParticipant {
  return {
    user_id: "user-1",
    name: "Ada",
    avatar_url: null,
    role: "participant",
    joined_at: "2026-09-01T00:00:00Z",
    online: false,
    ...over,
  };
}

function list(...participants: ChatParticipant[]): ChatParticipantList {
  return { participants };
}

describe("ChatParticipantListSchema", () => {
  it("parses a well-formed roster", () => {
    const parsed = parseWithFallback(
      {
        participants: [
          {
            user_id: "u1",
            name: "Ada",
            avatar_url: "https://cdn.test/a.png",
            role: "owner",
            joined_at: "2026-09-01T00:00:00Z",
            online: true,
          },
        ],
      },
      ChatParticipantListSchema,
      EMPTY_CHAT_PARTICIPANT_LIST,
      { endpoint: "test" },
    );
    expect(parsed.participants).toHaveLength(1);
    expect(parsed.participants[0]).toMatchObject({ user_id: "u1", role: "owner", online: true });
  });

  it("degrades an unknown role to the least-privileged one", () => {
    const parsed = parseWithFallback(
      { participants: [{ user_id: "u1", role: "superadmin" }] },
      ChatParticipantListSchema,
      EMPTY_CHAT_PARTICIPANT_LIST,
      { endpoint: "test" },
    );
    // A future server role must never light up the creator-only remove button.
    expect(parsed.participants[0]?.role).toBe("participant");
  });

  it("falls back to an empty roster on a malformed response", () => {
    for (const malformed of [null, "nope", 42, { participants: "not-an-array" }]) {
      const parsed = parseWithFallback(
        malformed,
        ChatParticipantListSchema,
        EMPTY_CHAT_PARTICIPANT_LIST,
        { endpoint: "test" },
      );
      expect(parsed.participants).toEqual([]);
    }
  });

  it("keeps a roster whose rows are missing optional display fields", () => {
    const parsed = parseWithFallback(
      { participants: [{ user_id: "u1" }] },
      ChatParticipantListSchema,
      EMPTY_CHAT_PARTICIPANT_LIST,
      { endpoint: "test" },
    );
    expect(parsed.participants[0]).toMatchObject({
      user_id: "u1",
      name: "",
      avatar_url: null,
      online: false,
    });
  });
});

describe("isSoloChatSession", () => {
  it("is solo while only the implicit owner is on the roster", () => {
    expect(isSoloChatSession(undefined)).toBe(true);
    expect(isSoloChatSession(list())).toBe(true);
    expect(isSoloChatSession(list(participant({ role: "owner" })))).toBe(true);
    expect(isSoloChatSession(list(participant({ role: "owner" }), participant()))).toBe(false);
  });
});

describe("pruneTypists", () => {
  const now = 1_000_000;

  it("drops the viewer's own ping and anything past the expiry", () => {
    const typing = {
      me: now,
      fresh: now - 500,
      stale: now - TYPING_EXPIRY_MS,
      ancient: now - TYPING_EXPIRY_MS * 10,
    };
    expect(pruneTypists(typing, "me", now)).toEqual(["fresh"]);
  });

  it("returns nothing when nobody is typing", () => {
    expect(pruneTypists({}, "me", now)).toEqual([]);
  });
});

describe("participantName", () => {
  it("falls back to the raw id for someone not on the roster", () => {
    const roster = list(participant({ user_id: "u1", name: "Ada" }));
    expect(participantName(roster, "u1")).toBe("Ada");
    expect(participantName(roster, "ghost")).toBe("ghost");
    expect(participantName(undefined, "ghost")).toBe("ghost");
  });

  it("falls back when the server sent a blank name", () => {
    expect(participantName(list(participant({ user_id: "u1", name: "  " })), "u1")).toBe("u1");
  });
});

describe("resolveMessageAuthor", () => {
  const owner = participant({ user_id: "owner", name: "Owner", role: "owner" });
  const peer = participant({ user_id: "peer", name: "Peer" });

  it("resolves an explicit author", () => {
    expect(resolveMessageAuthor(list(owner, peer), "peer")?.name).toBe("Peer");
  });

  it("attributes an author-less message to the owner (pre-K31 rows)", () => {
    expect(resolveMessageAuthor(list(owner, peer), null)?.user_id).toBe("owner");
    expect(resolveMessageAuthor(list(owner, peer), undefined)?.user_id).toBe("owner");
  });

  it("returns null for an author who has left the session", () => {
    expect(resolveMessageAuthor(list(owner), "departed")).toBeNull();
  });
});

describe("applyChatParticipantEvent", () => {
  const wsId = "ws-1";
  const sessionId = "sess-1";

  function clientWithSessions() {
    const qc = new QueryClient();
    qc.setQueryData(chatKeys.sessions(wsId), [{ id: sessionId }, { id: "other" }]);
    return qc;
  }

  it("drops the session from the list when the viewer is removed", () => {
    const qc = clientWithSessions();
    const lost = applyChatParticipantEvent(
      qc, wsId, "me", { session_id: sessionId, user_id: "me" }, "removed",
    );
    expect(lost).toBe(true);
    expect(qc.getQueryData(chatKeys.sessions(wsId))).toEqual([{ id: "other" }]);
  });

  it("leaves the list alone when somebody else is removed", () => {
    const qc = clientWithSessions();
    const lost = applyChatParticipantEvent(
      qc, wsId, "me", { session_id: sessionId, user_id: "someone-else" }, "removed",
    );
    expect(lost).toBe(false);
    expect(qc.getQueryData(chatKeys.sessions(wsId))).toEqual([{ id: sessionId }, { id: "other" }]);
  });

  it("never reports lost access on an add", () => {
    const qc = clientWithSessions();
    expect(
      applyChatParticipantEvent(qc, wsId, "me", { session_id: sessionId, user_id: "me" }, "added"),
    ).toBe(false);
    expect(qc.getQueryData(chatKeys.sessions(wsId))).toEqual([{ id: sessionId }, { id: "other" }]);
  });
});
