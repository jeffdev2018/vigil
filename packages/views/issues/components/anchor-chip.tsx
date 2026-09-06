"use client";

import { useCallback } from "react";
import { middleTruncate } from "@multica/core/pr-walkthrough";
import type { CommentAnchor } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * The anchor chip of a diff-anchored thread (F07 / JEF-21).
 *
 * Kept in its own module because the ordinary CommentCard draws it: putting it
 * next to DiffAnchorThread — which itself renders a CommentCard — would make
 * the two files import each other.
 */

/** The only anchor kind this build renders a chip for. */
const KNOWN_ANCHOR_KIND = "diff_line";

/** DOM id of a thread rendered inline under its hunk — the chip's scroll target. */
export function anchoredThreadDomId(rootId: string): string {
  return `anchored-thread-${rootId}`;
}

/** True when this build knows how to point at the place the anchor describes. */
export function isRenderableAnchor(anchor: CommentAnchor | null | undefined): boolean {
  return (
    !!anchor &&
    anchor.kind === KNOWN_ANCHOR_KIND &&
    !!anchor.file_path &&
    anchor.line_start > 0
  );
}

/** `path:41` or `path:41-44`, with the path middle-truncated for a chip. */
export function anchorLocation(anchor: CommentAnchor, truncate = true): string {
  const path = truncate ? middleTruncate(anchor.file_path, 32) : anchor.file_path;
  const end = anchor.line_end || anchor.line_start;
  return end > anchor.line_start
    ? `${path}:${anchor.line_start}-${end}`
    : `${path}:${anchor.line_start}`;
}

/**
 * The `path:line` chip. In the timeline it scrolls to the inline thread when
 * the walkthrough is showing it; when it is not — the hunk is collapsed, or
 * the file is outside the walkthrough — it stays a plain label rather than a
 * button that does nothing.
 */
export function AnchorChip({
  anchor,
  stale,
  rootId,
  className,
}: {
  anchor: CommentAnchor | null | undefined;
  stale?: boolean;
  /** Thread root id, so the chip can find the inline copy of this thread. */
  rootId?: string;
  className?: string;
}) {
  const { t } = useT("issues");
  const scrollToHunk = useCallback(() => {
    if (!rootId || typeof document === "undefined") return;
    document.getElementById(anchoredThreadDomId(rootId))?.scrollIntoView({
      behavior: "smooth",
      block: "center",
    });
  }, [rootId]);

  if (!isRenderableAnchor(anchor)) return null;
  const location = anchorLocation(anchor!);
  const full = anchorLocation(anchor!, false);
  const chipClass = "rounded bg-muted px-1 font-mono text-caption text-muted-foreground";

  return (
    <span className={cn("inline-flex items-center gap-1", className)}>
      {rootId ? (
        // Best-effort jump: the inline copy of this thread exists only while
        // the walkthrough is open and covers this file. When it is not there
        // the click is a no-op rather than a navigation to nowhere.
        <button
          type="button"
          title={t(($) => $.anchor.go_to_hunk)}
          aria-label={full}
          onClick={scrollToHunk}
          className={cn(chipClass, "transition-colors hover:bg-accent hover:text-foreground")}
        >
          {location}
        </button>
      ) : (
        // Nowhere to jump: a chip with no target is a label, not a control.
        <span className={chipClass} title={full}>
          {location}
        </span>
      )}
      {stale === true && (
        <span
          className="rounded bg-muted px-1 text-caption text-muted-foreground"
          data-testid="anchor-stale"
        >
          {t(($) => $.anchor.stale)}
        </span>
      )}
    </span>
  );
}

