"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { UserPlus, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useAuthStore } from "@multica/core/auth";
import { useWSEvent } from "@multica/core/realtime";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  useAddChatParticipant,
  useChatParticipants,
  useRemoveChatParticipant,
  isSoloChatSession,
  participantName,
  pruneTypists,
  resolveMessageAuthor,
  sendChatTypingPing,
  TYPING_EXPIRY_MS,
  TYPING_THROTTLE_MS,
} from "@multica/core/chat/participants";
import type { ChatMessage, ChatSession } from "@multica/core/types";
import type { ChatTypingPayload } from "@multica/core/types/events";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";

/**
 * Roster strip for a multiplayer chat session (K31 / JEF-181): who is in the
 * conversation, who is online, who is typing, and — for the creator — the
 * controls to change that.
 *
 * Hidden entirely while the session is solo, so a single-player chat looks
 * exactly as it did before this feature.
 *
 * Typing state is local and ephemeral: `chat:typing` is workspace-broadcast
 * and never persisted, so this subscribes directly instead of routing it
 * through the query cache or a store. The expiry matrix lives in
 * `pruneTypists` — see packages/core/chat/participants.test.ts.
 */
export function ParticipantBar({
  session,
  wsId,
}: {
  session: ChatSession;
  wsId: string;
}) {
  const { t } = useT("chat");
  const selfId = useAuthStore((s) => s.user?.id) ?? "";
  const { data: roster } = useChatParticipants(wsId, session.id);
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const addParticipant = useAddChatParticipant(wsId, session.id);
  const removeParticipant = useRemoveChatParticipant(wsId, session.id);

  const isCreator = session.creator_id === selfId;
  // Stable identity: a fresh [] each render would re-run the addable memo and
  // re-render the popover on every parent tick.
  const participants = useMemo(() => roster?.participants ?? [], [roster]);
  const solo = isSoloChatSession(roster);

  // `{ userId: receivedAtMs }`. Stamped locally, not from payload.at: a
  // client whose clock is minutes off would otherwise show a permanent or a
  // never-appearing indicator.
  const [typing, setTyping] = useState<Record<string, number>>({});
  const onTyping = useCallback(
    (p: unknown) => {
      const payload = p as ChatTypingPayload;
      if (payload?.session_id !== session.id || !payload.user_id) return;
      setTyping((prev) => ({ ...prev, [payload.user_id]: Date.now() }));
    },
    [session.id],
  );
  useWSEvent("chat:typing", onTyping);

  // A typing ping has no "stopped" counterpart, so the indicator has to expire
  // on a timer rather than on an event. Re-render once per expiry window while
  // anyone is typing, and stop scheduling as soon as nobody is.
  const [, setTick] = useState(0);
  const hasPings = Object.keys(typing).length > 0;
  useEffect(() => {
    if (!hasPings) return;
    const id = setInterval(() => setTick((n) => n + 1), TYPING_EXPIRY_MS / 3);
    return () => clearInterval(id);
  }, [hasPings]);

  const typists = pruneTypists(typing, selfId, Date.now());
  const typingLabel =
    typists.length === 1
      ? t(($) => $.participants.typing_name, { name: participantName(roster, typists[0] ?? "") })
      : typists.length > 1
        ? t(($) => $.participants.typing_count, { count: typists.length })
        : "";

  const addable = useMemo(() => {
    const present = new Set(participants.map((p) => p.user_id));
    return members.filter((m) => !present.has(m.user_id));
  }, [members, participants]);

  // Solo sessions render nothing but the creator's "add" affordance — without
  // it a chat could never become multiplayer in the first place.
  if (solo && !isCreator) return null;

  return (
    <div className="flex min-h-8 items-center gap-2 border-b px-4 py-1">
      {!solo && (
        <div className="flex min-w-0 items-center gap-1.5">
          {participants.map((p) => (
            <span key={p.user_id} className="group relative shrink-0" data-testid="chat-participant">
              <ActorAvatar
                actorType="member"
                actorId={p.user_id}
                name={p.name}
                avatarUrl={p.avatar_url}
                size="sm"
              />
              {p.online === true && (
                <span
                  aria-label={t(($) => $.participants.online)}
                  className="absolute -bottom-0.5 -right-0.5 size-2 rounded-full bg-success ring-2 ring-background"
                />
              )}
              {(isCreator || p.user_id === selfId) && p.role !== "owner" && (
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="absolute -right-2 -top-2 size-4 rounded-full bg-background p-0 text-muted-foreground opacity-0 focus-visible:opacity-100 group-hover:opacity-100 hover:opacity-100"
                  aria-label={
                    p.user_id === selfId
                      ? t(($) => $.participants.leave)
                      : t(($) => $.participants.remove, { name: p.name })
                  }
                  onClick={() => removeParticipant.mutate(p.user_id)}
                >
                  <X className="size-3" />
                </Button>
              )}
            </span>
          ))}
        </div>
      )}

      {isCreator && (
        <Popover>
          <PopoverTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                className="shrink-0 text-muted-foreground"
                aria-label={t(($) => $.participants.add)}
              />
            }
          >
            <UserPlus className="size-4" />
          </PopoverTrigger>
          <PopoverContent align="start" className="w-56 p-1">
            {addable.length === 0 ? (
              <p className="px-2 py-1.5 text-caption text-muted-foreground">
                {t(($) => $.participants.nobody_to_add)}
              </p>
            ) : (
              addable.map((m) => (
                <button
                  key={m.user_id}
                  type="button"
                  className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-body hover:bg-accent"
                  onClick={() => addParticipant.mutate(m.user_id)}
                >
                  <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
                  <span className="truncate">{m.name || m.email}</span>
                </button>
              ))
            )}
          </PopoverContent>
        </Popover>
      )}

      {typingLabel && (
        <span className="ml-auto truncate text-caption text-muted-foreground" aria-live="polite">
          {typingLabel}
        </span>
      )}
    </div>
  );
}

/**
 * Throttled "I am typing" pinger for the composer. One POST per
 * TYPING_THROTTLE_MS while the draft changes, and none at all before a session
 * exists — a chat that has not been created yet has nobody to notify.
 *
 * Deliberately not debounced: a debounce would delay the first ping by the
 * whole window, which is exactly the moment the indicator is worth showing.
 */
export function useThrottledChatTyping(sessionId: string | null): () => void {
  const lastSentRef = useRef(0);

  return useCallback(() => {
    if (!sessionId) return;
    const now = Date.now();
    if (now - lastSentRef.current < TYPING_THROTTLE_MS) return;
    lastSentRef.current = now;
    sendChatTypingPing(sessionId);
  }, [sessionId]);
}

/**
 * Builds the `resolveAuthorName` ChatMessageList expects. Lives here rather
 * than inside the list because the list is also mounted by surfaces without a
 * workspace provider, and because one roster subscription per transcript is
 * enough — see the prop's doc comment.
 *
 * Returns undefined for a solo session so the list can skip the badge branch
 * entirely and a one-person chat renders byte-identically to before K31.
 */
export function useChatAuthorNames(
  wsId: string,
  sessionId: string | null,
): ((message: ChatMessage) => string | null) | undefined {
  const { data: roster } = useChatParticipants(wsId, sessionId ?? "");
  const solo = isSoloChatSession(roster);
  return useMemo(() => {
    if (solo) return undefined;
    return (message: ChatMessage) =>
      resolveMessageAuthor(roster, message.author_user_id)?.name ?? null;
  }, [roster, solo]);
}
