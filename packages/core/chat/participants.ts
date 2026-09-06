import { queryOptions, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { chatKeys } from "./queries";
import { createLogger } from "../logger";
import type { ChatParticipant, ChatParticipantList } from "../types";

const logger = createLogger("chat.participants");

/**
 * Multiplayer chat sessions (K31 / JEF-181).
 *
 * The roster is server state (TanStack Query); the typing indicator is not —
 * it is derived per render from ephemeral events by `pruneTypists`, which is a
 * pure function so its expiry matrix is testable without a DOM.
 */

/**
 * How long a `chat:typing` ping keeps the indicator lit. There is no "stopped
 * typing" event by design, so the client expires it. Comfortably longer than
 * TYPING_THROTTLE_MS so a steady typist never flickers.
 */
export const TYPING_EXPIRY_MS = 3_000;

/** One POST per this window while the composer changes. */
export const TYPING_THROTTLE_MS = 2_000;

export const chatParticipantKeys = {
  all: () => ["chat", "participants"] as const,
  bySession: (wsId: string, sessionId: string) =>
    [...chatParticipantKeys.all(), wsId, sessionId] as const,
};

export function chatParticipantsOptions(wsId: string, sessionId: string) {
  return queryOptions({
    queryKey: chatParticipantKeys.bySession(wsId, sessionId),
    queryFn: () => api.listChatParticipants(sessionId),
    enabled: Boolean(wsId) && Boolean(sessionId),
  });
}

/** The session roster: creator first (role "owner"), then join order. */
export function useChatParticipants(wsId: string, sessionId: string) {
  return useQuery(chatParticipantsOptions(wsId, sessionId));
}

/**
 * A session is solo while nobody has been added — the roster is exactly the
 * implicit owner. Callers hide the participant bar and the author badges in
 * that state, so the single-player chat looks untouched by this feature.
 */
export function isSoloChatSession(list: ChatParticipantList | undefined): boolean {
  return (list?.participants?.length ?? 0) <= 1;
}

/**
 * Typing pings that are still fresh at `now`, minus the viewer's own.
 * Pure: the expiry matrix is pinned in participants.test.ts, not through a
 * mounted component.
 */
export function pruneTypists(
  typing: Record<string, number>,
  selfUserId: string,
  now: number,
): string[] {
  return Object.entries(typing)
    .filter(([userId, at]) => userId !== selfUserId && now - at < TYPING_EXPIRY_MS)
    .map(([userId]) => userId);
}

/** Display name for a typing user, falling back to the raw id. */
export function participantName(
  list: ChatParticipantList | undefined,
  userId: string,
): string {
  const found = list?.participants?.find((p) => p.user_id === userId);
  return found?.name?.trim() || userId;
}

/**
 * Author of a chat bubble. Messages written before the column existed carry
 * no author, so a user message without one is attributed to the session's
 * owner — the only human who could have sent it back then.
 */
export function resolveMessageAuthor(
  list: ChatParticipantList | undefined,
  authorUserId: string | null | undefined,
): ChatParticipant | null {
  const participants = list?.participants ?? [];
  if (authorUserId) {
    return participants.find((p) => p.user_id === authorUserId) ?? null;
  }
  return participants.find((p) => p.role === "owner") ?? null;
}

export function useAddChatParticipant(wsId: string, sessionId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api.addChatParticipant(sessionId, userId),
    // Adding navigates nowhere and the roster is server-ordered (joined_at),
    // so this refetches instead of guessing where the new row lands.
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: chatParticipantKeys.bySession(wsId, sessionId) });
    },
  });
}

export function useRemoveChatParticipant(wsId: string, sessionId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api.removeChatParticipant(sessionId, userId),
    onSettled: () => {
      void qc.invalidateQueries({ queryKey: chatParticipantKeys.bySession(wsId, sessionId) });
    },
  });
}

/**
 * Fire-and-forget typing ping. Deliberately NOT a mutation hook: it touches no
 * cache, has no loading state and must not retry, and making it one would make
 * a QueryClientProvider a hard requirement of the composer. Throttling belongs
 * to the caller (see useThrottledChatTyping in views).
 *
 * Silent on failure: a dropped ping costs the reader three seconds of a stale
 * indicator and nothing else.
 */
export function sendChatTypingPing(sessionId: string): void {
  if (!sessionId) return;
  // try/catch AND .catch: the composer calls this from inside its onUpdate
  // handler, so a transport that throws synchronously (rather than rejecting)
  // would abort the draft commit on the same keystroke and lose the user's
  // text. A cosmetic indicator must never be able to do that.
  try {
    void Promise.resolve(api.sendChatTyping(sessionId)).catch((err: unknown) => {
      logger.debug("typing.ping.failed", { sessionId, err });
    });
  } catch (err) {
    logger.debug("typing.ping.failed", { sessionId, err });
  }
}

/**
 * WS reaction to a roster change. `chat:participant_added` and
 * `chat:participant_removed` are workspace-broadcast, so every client sees
 * both — including the member who was just added (whose session list must now
 * include the session) and the one removed (whose list must drop it).
 *
 * Returns true when the event was about the viewer losing access, so the
 * caller can also clear a pointer at that session.
 */
export function applyChatParticipantEvent(
  qc: QueryClient,
  wsId: string,
  selfUserId: string,
  event: { session_id: string; user_id: string },
  kind: "added" | "removed",
): boolean {
  void qc.invalidateQueries({
    queryKey: chatParticipantKeys.bySession(wsId, event.session_id),
  });

  const aboutSelf = event.user_id === selfUserId;
  if (!aboutSelf) return false;

  if (kind === "removed") {
    // Never optimistic in the other direction: the row is gone server-side
    // already, and leaving it in the list would render a session every read
    // now 403s on.
    qc.setQueryData(chatKeys.sessions(wsId), (old?: { id: string }[]) =>
      old?.filter((s) => s.id !== event.session_id),
    );
    qc.removeQueries({ queryKey: chatKeys.messages(event.session_id) });
    return true;
  }

  void qc.invalidateQueries({ queryKey: chatKeys.sessions(wsId) });
  return false;
}
