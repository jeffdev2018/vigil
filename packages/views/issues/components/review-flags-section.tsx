"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Flag, MoreHorizontal, RotateCcw } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  reviewFlagsOptions,
  useSetReviewFlagState,
  flagLocation,
  isSettled,
  isStale,
  middleTruncatePath,
  normalizeSeverity,
  normalizeState,
  totalOpen,
  type ReviewFlag,
  type ReviewFlagCounts,
  type ReviewFlagFilter,
  type ReviewFlagSeverity,
} from "@multica/core/review-flags";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { AnchorAskButton, AnchorComposer } from "./diff-anchor-thread";

/**
 * Review flags by severity (F06 / JEF-19).
 *
 * The section under the walkthrough that answers "what did the review actually
 * find". Rows arrive already sorted by the server — severity, then confidence
 * — and this component does not re-sort them: a second opinion about reading
 * order would diverge from the counts the badge shows.
 *
 * Collapsed by default, like the walkthrough above it, and hidden entirely
 * when the issue has nothing flagged.
 */

// Severity dots use semantic tokens, so the palette follows the theme rather
// than being pinned to a hex the dark theme has never seen.
const SEVERITY_DOT: Record<ReviewFlagSeverity, string> = {
  bug: "bg-destructive",
  warning: "bg-warning",
  info: "bg-muted-foreground",
};

export function ReviewFlagsSection({ issueId, currentUserId }: {
  issueId: string;
  /** Passed to the composer that opens a discussion anchored to a flag (F07). */
  currentUserId?: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState<ReviewFlagFilter>("open");

  const { data, isLoading, isError, refetch } = useQuery(reviewFlagsOptions(wsId, issueId, filter));
  const setState = useSetReviewFlagState(wsId, issueId);

  const counts: ReviewFlagCounts = data?.counts ?? { bug: 0, warning: 0, info: 0 };
  const flags = data?.flags ?? [];
  const openCount = totalOpen(counts);

  // Nothing to show yet: rendering the header during the first fetch would
  // flash a section that is about to disappear on most issues.
  if (isLoading) return null;
  // Nothing flagged and nothing settled: the header would be a promise the
  // section cannot keep. An error still renders — a silent section would read
  // as "the review found nothing".
  if (!isError && openCount === 0 && flags.length === 0 && filter === "open") return null;

  return (
    <div data-testid="review-flags">
      <button
        type="button"
        className={cn(
          "mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70",
          open ? "" : "text-muted-foreground hover:text-foreground",
        )}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <Flag className="!size-3 shrink-0 text-muted-foreground" />
        {t(($) => $.review_flags.section)}
        <ReviewFlagCountBadge counts={counts} />
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
      </button>
      {open && (
        <div className="space-y-2 pl-2">
          <ReviewFlagFilterTabs value={filter} onChange={setFilter} />
          {isError ? (
            <div className="flex items-center gap-2 px-2 py-1">
              <p className="text-caption text-muted-foreground">{t(($) => $.review_flags.error)}</p>
              <Button type="button" size="sm" variant="ghost" className="h-6 px-2 text-caption" onClick={() => void refetch()}>
                <RotateCcw className="!size-3" />
                {t(($) => $.review_flags.retry)}
              </Button>
            </div>
          ) : flags.length === 0 ? (
            <p className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.review_flags.empty)}</p>
          ) : (
            <ul className="space-y-1">
              {flags.map((flag) => (
                <ReviewFlagRow
                  key={flag.id}
                  flag={flag}
                  issueId={issueId}
                  currentUserId={currentUserId}
                  disabled={setState.isPending}
                  onSetState={(state) => setState.mutate({ flagId: flag.id, state })}
                />
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  );
}

/** Open bug / warning / info, never the settled ones. */
function ReviewFlagCountBadge({ counts }: { counts: ReviewFlagCounts }) {
  const { t } = useT("issues");
  if (totalOpen(counts) === 0) return null;
  return (
    <span className="flex items-center gap-1.5" data-testid="review-flags-counts">
      {(["bug", "warning", "info"] as const).map((severity) =>
        counts[severity] > 0 ? (
          <span
            key={severity}
            className="flex items-center gap-1 text-caption text-muted-foreground"
            title={t(($) => $.review_flags.severity[severity])}
          >
            <span className={cn("size-1.5 shrink-0 rounded-full", SEVERITY_DOT[severity])} aria-hidden />
            {counts[severity]}
          </span>
        ) : null,
      )}
    </span>
  );
}

function ReviewFlagFilterTabs({
  value,
  onChange,
}: {
  value: ReviewFlagFilter;
  onChange: (next: ReviewFlagFilter) => void;
}) {
  const { t } = useT("issues");
  return (
    <div className="flex items-center gap-1">
      {(["open", "all"] as const).map((option) => (
        <button
          key={option}
          type="button"
          data-active={value === option}
          aria-pressed={value === option}
          // The active tab is expressed in weight and text colour, which hover
          // does not touch, so hovering a selected tab never downgrades it to
          // looking like a plain hover.
          className={cn(
            "rounded-md px-2 py-0.5 text-caption transition-colors hover:bg-accent/70",
            value === option ? "font-medium text-foreground" : "text-muted-foreground hover:text-foreground",
          )}
          onClick={() => onChange(option)}
        >
          {t(($) => $.review_flags.filter[option])}
        </button>
      ))}
    </div>
  );
}

function ReviewFlagRow({
  flag,
  issueId,
  currentUserId,
  disabled,
  onSetState,
}: {
  flag: ReviewFlag;
  issueId: string;
  currentUserId?: string;
  disabled: boolean;
  onSetState: (state: "open" | "resolved" | "dismissed") => void;
}) {
  const { t } = useT("issues");
  // "Ask about this flag" (F07): opens a thread anchored to the flag's own
  // range AND to the flag itself, so the discussion and the finding stay tied
  // together and the run a reply triggers is told which finding it is about.
  const [asking, setAsking] = useState(false);
  const severity = normalizeSeverity(flag.severity);
  const state = normalizeState(flag.state);
  const stale = isStale(flag);
  const settled = isSettled(flag);
  const location = flagLocation({ ...flag, file_path: middleTruncatePath(flag.file_path) });

  return (
    <li
      data-testid="review-flag"
      data-severity={severity}
      data-state={state}
      className={cn(
        "group flex items-start gap-2 rounded-md px-2 py-1.5 transition-colors hover:bg-accent/50",
        // Stale and settled rows stay readable — a reviewer justifying a merge
        // needs to be able to read what was found — but recede.
        (stale || settled) && "opacity-70",
      )}
    >
      <span className={cn("mt-1.5 size-1.5 shrink-0 rounded-full", SEVERITY_DOT[severity])} aria-hidden />
      <div className="min-w-0 flex-1 space-y-0.5">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className={cn("text-body", settled && "line-through")}>{flag.title}</span>
          {flag.confidence !== null && (
            <span
              className="shrink-0 rounded bg-muted px-1 text-caption text-muted-foreground"
              data-testid="review-flag-confidence"
            >
              {t(($) => $.review_flags.confidence, { value: flag.confidence })}
            </span>
          )}
          {stale && (
            <span className="shrink-0 rounded bg-muted px-1 text-caption text-muted-foreground" data-testid="review-flag-stale">
              {t(($) => $.review_flags.stale)}
            </span>
          )}
        </div>
        <p className="font-mono text-caption text-muted-foreground" title={flagLocation(flag)}>
          {location}
        </p>
        {flag.body && (
          // Three lines, then the rest is in the thread the reviewer opens.
          <p className="line-clamp-3 text-caption text-muted-foreground">{flag.body}</p>
        )}
        {asking && (
          <AnchorComposer
            issueId={issueId}
            location={location}
            currentUserId={currentUserId}
            onDone={() => setAsking(false)}
            anchor={{
              pr_id: flag.pr_id,
              file_path: flag.file_path,
              line_start: flag.line_start,
              line_end: flag.line_end,
              side: flag.side,
              // The flag was written against ITS head, and the question is
              // about the code the flag describes — not about whatever the
              // pull request has moved on to since.
              head_sha: flag.head_sha,
              review_flag_id: flag.id,
            }}
          />
        )}
      </div>
      {!asking && (
        <AnchorAskButton
          label={t(($) => $.anchor.ask_flag)}
          onAsk={() => setAsking(true)}
          className="opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100"
        />
      )}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              className="shrink-0 text-muted-foreground"
              disabled={disabled}
              aria-label={t(($) => $.review_flags.actions)}
            >
              <MoreHorizontal className="!size-3.5" aria-hidden />
            </Button>
          }
        />
        <DropdownMenuContent align="end">
          {/* A stale flag can still be settled: the reviewer may have checked
              that the new head fixed it, and saying so is the point. */}
          {settled ? (
            <DropdownMenuItem onClick={() => onSetState("open")}>
              {t(($) => $.review_flags.reopen)}
            </DropdownMenuItem>
          ) : (
            <>
              <DropdownMenuItem onClick={() => onSetState("resolved")}>
                {t(($) => $.review_flags.resolve)}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onSetState("dismissed")}>
                {t(($) => $.review_flags.dismiss)}
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </li>
  );
}
