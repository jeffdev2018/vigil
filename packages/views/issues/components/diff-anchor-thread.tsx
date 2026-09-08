"use client";

import { useCallback, useMemo, useState } from "react";
import { MessageSquarePlus } from "lucide-react";
import {
  useCreateComment,
  useDeleteComment,
  useResolveComment,
  useToggleCommentReaction,
  useUpdateComment,
} from "@multica/core/issues/mutations";
import type { AnchoredThread, Comment, CreateCommentAnchor, TimelineEntry } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { anchoredThreadDomId } from "./anchor-chip";
import { CommentCard } from "./comment-card";
import { ReplyInput } from "./reply-input";
import { useT } from "../../i18n";

/** Replies shown before the thread folds. Older ones are one click away. */
const VISIBLE_REPLIES = 3;

/**
 * Comment threads anchored to a diff line (F07 / JEF-21).
 *
 * A reviewer reading a hunk asks about THAT line, and the answer belongs next
 * to it. This renders such a thread twice over: inline under the hunk it is
 * anchored to, and — through the anchor chip on its comment card — in the
 * issue timeline, where it stays a first-class comment, not a side channel.
 *
 * Three rules shape what is here:
 *
 *   - an anchor whose `kind` this build does not know renders the thread
 *     WITHOUT a chip, never hides it. Losing a discussion because a newer
 *     server named its anchor something else would be the worst possible
 *     failure mode of an additive field.
 *   - a stale anchor (the head moved) stays readable and repliable. The push
 *     is often the answer to the question, and the reviewer needs both.
 *   - the thread renders through the ordinary {@link CommentCard}, so editing,
 *     reactions, resolution and mention triggers behave exactly as they do in
 *     the timeline instead of quietly diverging in a second implementation.
 */

/**
 * Turn an API comment into the timeline shape {@link CommentCard} reads. The
 * card is the one renderer for a comment in this product; adapting here is
 * cheaper than a second card that would drift from it.
 */
function toTimelineEntry(comment: Comment): TimelineEntry {
  return {
    type: "comment",
    id: comment.id,
    actor_type: comment.author_type,
    actor_id: comment.author_id,
    content: comment.content,
    comment_type: comment.type,
    quick_action_id: comment.quick_action_id,
    a2a_intent: comment.a2a_intent,
    parent_id: comment.parent_id,
    created_at: comment.created_at,
    updated_at: comment.updated_at,
    revision: comment.revision,
    reactions: comment.reactions ?? [],
    attachments: comment.attachments ?? [],
    resolved_at: comment.resolved_at,
    resolved_by_type: comment.resolved_by_type,
    resolved_by_id: comment.resolved_by_id,
    source_task_id: comment.source_task_id,
    anchor: comment.anchor,
    anchor_stale: comment.anchor_stale,
  };
}

/**
 * The composer that opens a NEW anchored thread. It is the ordinary reply
 * composer with no parent — same editor, same `/` menu, same mention trigger
 * preview — because a question about a line is an ordinary comment that
 * happens to know where it is about.
 */
export function AnchorComposer({
  issueId,
  anchor,
  location,
  currentUserId,
  onDone,
}: {
  issueId: string;
  anchor: CreateCommentAnchor;
  /** Human-readable `path:line`, shown in the placeholder. */
  location: string;
  currentUserId?: string;
  onDone: () => void;
}) {
  const { t } = useT("issues");
  const createComment = useCreateComment(issueId);

  const submit = useCallback(
    async (content: string, attachmentIds?: string[], suppressAgentIds?: string[]) => {
      try {
        const created = await createComment.mutateAsync({
          content,
          parentId: undefined,
          attachmentIds,
          suppressAgentIds,
          anchor,
        });
        onDone();
        return created.id;
      } catch {
        // The composer keeps the text on a false return, so the question is
        // never lost to a failed request.
        return false;
      }
    },
    [createComment, anchor, onDone],
  );

  return (
    <div className="space-y-1" data-testid="anchor-composer">
      <ReplyInput
        issueId={issueId}
        parentId=""
        placeholder={t(($) => $.anchor.ask_placeholder, { location })}
        avatarType="member"
        avatarId={currentUserId ?? ""}
        onSubmit={submit}
        size="sm"
      />
      <Button type="button" size="sm" variant="ghost" className="h-6 px-2 text-caption" onClick={onDone}>
        {t(($) => $.anchor.cancel)}
      </Button>
    </div>
  );
}

/**
 * The "Ask about this line / this flag" affordance. It only OPENS the
 * composer; the caller owns where the composer renders, because a composer
 * inside a diff table cell (or inside a flag row) would be unreadable.
 */
export function AnchorAskButton({
  label,
  onAsk,
  className,
}: {
  label: string;
  onAsk: () => void;
  className?: string;
}) {
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      className={cn("h-5 shrink-0 px-1 text-caption text-muted-foreground", className)}
      aria-label={label}
      title={label}
      onClick={onAsk}
    >
      <MessageSquarePlus className="!size-3" aria-hidden />
    </Button>
  );
}

/**
 * One anchored discussion rendered inline: the root, its replies, and a reply
 * box. Long threads fold to the last {@link VISIBLE_REPLIES} so a hunk stays
 * readable; the older replies are one click away, never dropped.
 */
export function DiffAnchorThread({
  issueId,
  thread,
  currentUserId,
  canModerate,
}: {
  issueId: string;
  thread: AnchoredThread;
  currentUserId?: string;
  canModerate?: boolean;
}) {
  const { t } = useT("issues");
  const [expanded, setExpanded] = useState(false);
  const createComment = useCreateComment(issueId);
  const updateComment = useUpdateComment(issueId);
  const deleteComment = useDeleteComment(issueId);
  const resolveComment = useResolveComment(issueId);
  const toggleReaction = useToggleCommentReaction(issueId);

  const replies = useMemo(() => thread.replies ?? [], [thread.replies]);
  const hidden = Math.max(0, replies.length - VISIBLE_REPLIES);
  const shown = useMemo(
    () => (expanded || hidden === 0 ? replies : replies.slice(hidden)),
    [expanded, hidden, replies],
  );
  const replyEntries = useMemo(() => shown.map(toTimelineEntry), [shown]);
  const rootEntry = useMemo(() => toTimelineEntry(thread.root), [thread.root]);

  const onReply = useCallback(
    async (parentId: string, content: string, attachmentIds?: string[], suppressAgentIds?: string[]) => {
      try {
        const created = await createComment.mutateAsync({ content, parentId, attachmentIds, suppressAgentIds });
        return created.id;
      } catch {
        return false;
      }
    },
    [createComment],
  );

  const onEdit = useCallback(
    async (commentId: string, content: string, attachmentIds: string[], suppressAgentIds?: string[], contentBase?: string) => {
      await updateComment.mutateAsync({ commentId, content, attachmentIds, suppressAgentIds, contentBase });
    },
    [updateComment],
  );

  const onDelete = useCallback((commentId: string) => deleteComment.mutate(commentId), [deleteComment]);

  const onToggleReaction = useCallback(
    (commentId: string, emoji: string) => {
      const target = commentId === thread.root.id
        ? thread.root
        : replies.find((r) => r.id === commentId);
      // The mutation needs the reaction ROW to remove, not a boolean: it
      // deletes by (comment, emoji) only when this member already reacted.
      const existing = (target?.reactions ?? []).find(
        (r) => r.emoji === emoji && r.actor_type === "member" && r.actor_id === currentUserId,
      );
      toggleReaction.mutate({ commentId, emoji, existing });
    },
    [toggleReaction, thread.root, replies, currentUserId],
  );

  const onResolveToggle = useCallback(
    (commentId: string, resolved: boolean) => resolveComment.mutate({ commentId, resolved }),
    [resolveComment],
  );

  return (
    <div
      id={anchoredThreadDomId(thread.root.id)}
      data-testid="diff-anchor-thread"
      data-stale={thread.anchor_stale === true}
      className="space-y-1 border-l-2 border-border/60 pl-3"
    >
      {hidden > 0 && (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          className="h-5 px-1 text-caption text-muted-foreground"
          data-testid="anchor-thread-fold"
          onClick={() => setExpanded(!expanded)}
        >
          {expanded
            ? t(($) => $.anchor.hide_replies)
            : t(($) => $.anchor.show_replies, { count: hidden })}
        </Button>
      )}
      <CommentCard
        issueId={issueId}
        entry={rootEntry}
        replies={replyEntries}
        currentUserId={currentUserId}
        canModerate={canModerate}
        onReply={onReply}
        onEdit={onEdit}
        onDelete={onDelete}
        onToggleReaction={onToggleReaction}
        onResolveToggle={onResolveToggle}
      />
    </div>
  );
}
