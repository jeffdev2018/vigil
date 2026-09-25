"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import type { ChatSession } from "@multica/core/types";
import { useT } from "../../i18n";

/**
 * The archive sequence both chat surfaces run: move the selection off the
 * conversation being archived, fire the mutation, and — when the server
 * refuses — put the selection back and say so.
 *
 * It lives here because the Chat page and the floating window are two
 * independent state machines (the window builds on the chat store directly,
 * not on `useChatController`), and each had its own copy of the "advance to
 * the next chat" step. Only the page had the rollback, so the same refused
 * archive left the floating window reading another conversation while the one
 * the user archived was still in the list.
 *
 * The selection move is part of the same optimistic step as the status patch
 * in `useSetChatSessionArchived`, so it rolls back the same way: the mutation
 * restores the list, this restores what was on screen. The advance has to be
 * computed BEFORE the mutation, not after it resolves — it looks the session
 * up in the non-archived history, which the optimistic patch has already
 * removed by then.
 */
export interface ArchiveSessionFlowDeps {
  activeSessionId: string | null;
  /** Non-archived history, in the order this surface displays it. */
  history: ChatSession[];
  /** Selecting a session, not just setting its id: the caller's own select
   *  keeps side state (the picked agent, the thread list) in sync. */
  selectSession: (session: ChatSession) => void;
  clearSelection: () => void;
  archive: (sessionId: string, options: { onError: () => void }) => void;
}

export interface ArchiveSessionOptions {
  /** Replaces the default advance-to-the-neighbour move (the Chat page drops
   *  back to the list when compact rather than opening an unrelated chat). */
  moveSelection?: () => void;
  /** Extra caller state to restore beside the selection when the archive is
   *  refused. Runs only when the selection actually moved. */
  rollback?: () => void;
}

export function useArchiveSessionFlow({
  activeSessionId,
  history,
  selectSession,
  clearSelection,
  archive,
}: ArchiveSessionFlowDeps) {
  const { t } = useT("chat");

  return useCallback(
    (session: ChatSession, options?: ArchiveSessionOptions) => {
      const movedOffArchived = activeSessionId === session.id;
      if (movedOffArchived) {
        if (options?.moveSelection) {
          options.moveSelection();
        } else {
          // Mirror the Inbox list: advance to the next chat in the displayed
          // history, fall back to the previous one, clear only when nothing
          // is left.
          const index = history.findIndex((s) => s.id === session.id);
          const next = history[index + 1] ?? history[index - 1] ?? null;
          if (next) selectSession(next);
          else clearSelection();
        }
      }
      archive(session.id, {
        onError: () => {
          if (movedOffArchived) {
            selectSession(session);
            options?.rollback?.();
          }
          toast.error(t(($) => $.page.archive_failed));
        },
      });
    },
    [activeSessionId, history, selectSession, clearSelection, archive, t],
  );
}
