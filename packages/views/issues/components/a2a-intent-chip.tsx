"use client";

import { a2aRecipientFromContent, knownA2AIntent } from "@multica/core/issues/a2a-message";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * The chip on an agent-to-agent message (F19 / JEF-32): what one agent is
 * asking another for, and who is being asked.
 *
 * Three deliberate differences from the quick-action card:
 *
 *   - The BODY STAYS FULLY VISIBLE. A quick action's body is a prompt, an audit
 *     record worth folding away. An A2A message is conversation — the reason it
 *     rides on a comment at all is so a person reading the issue sees what the
 *     agents said to each other.
 *   - An intent this build cannot label renders NOTHING. The column carries no
 *     CHECK (migration 828), so a newer backend's value must degrade to an
 *     ordinary comment rather than to a chip with an empty label.
 *   - The recipient is read back out of the mention markup the server composed,
 *     not from a separate field. The mention IS the address, so the chip and the
 *     run that was actually enqueued cannot disagree.
 */
export function A2AIntentChip({
  intent,
  content,
  className,
}: {
  intent: string | null | undefined;
  /** The comment body, which carries the server-composed recipient mention. */
  content: string | null | undefined;
  className?: string;
}) {
  const { t } = useT("issues");
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();

  const known = knownA2AIntent(intent);
  const recipient = a2aRecipientFromContent(content ?? "");
  if (!known || !recipient) return null;

  const label =
    known === "question"
      ? t(($) => $.comment.a2a_question)
      : known === "review"
        ? t(($) => $.comment.a2a_review)
        : t(($) => $.comment.a2a_handoff);

  // An ARCHIVED recipient no longer resolves through the workspace directory, so
  // getActorName returns nothing useful. Fall back to the label the server wrote
  // into the markup at send time: the name it HAD is the honest answer, and a
  // blank chip would erase who the message was for.
  const name = getActorName("agent", recipient.agentId) || recipient.label;

  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded bg-muted px-1.5 py-0.5 text-caption text-muted-foreground",
        className,
      )}
    >
      <span>{label}</span>
      {/* The BASE avatar, not the views wrapper: this is a 16px decoration
          inside a chip, and the wrapper's hover card and profile link would drag
          a workspace-scoped route requirement into a component that only needs
          to draw a face. The name beside it is the affordance. */}
      <ActorAvatarBase
        name={name}
        initials={getActorInitials("agent", recipient.agentId, name)}
        avatarUrl={getActorAvatarUrl("agent", recipient.agentId)}
        isAgent
        size="xs"
      />
      <span className="max-w-32 truncate text-foreground">{name}</span>
    </span>
  );
}
