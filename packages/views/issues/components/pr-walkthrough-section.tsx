"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, RefreshCw, ScrollText, TriangleAlert } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { issuePullRequestsOptions } from "@multica/core/github";
import type { GitHubPullRequest } from "@multica/core/types";
import {
  anchoredThreadsOptions,
  anchorForLine,
  hunkContainsAnchor,
  hunkLines,
  prWalkthroughOptions,
  useRefreshPrWalkthrough,
  groupHunkCount,
  middleTruncate,
  normalizeKind,
  orderedGroups,
  type HunkLine,
  type PrWalkthrough,
  type PrWalkthroughGroup,
  type PrWalkthroughHunk,
} from "@multica/core/pr-walkthrough";
import type { AnchoredThread } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { AnchorAskButton, AnchorComposer, DiffAnchorThread } from "./diff-anchor-thread";

/**
 * Narrative pull request walkthrough (F05 / JEF-16).
 *
 * The section under the PR list that answers "which of these 40 files do I
 * actually have to read". One block per linked pull request, groups in the
 * fixed order core → test → generated → noise, each hunk shown with the
 * explanation of why it changed.
 *
 * Collapsed by default: it is a reading aid a reviewer opens deliberately, not
 * something that should push the rest of the issue off the screen.
 */
export function PrWalkthroughSection({ issueId, currentUserId, canModerate }: {
  issueId: string;
  /** Threaded through to the anchored discussions rendered under each hunk. */
  currentUserId?: string;
  canModerate?: boolean;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);
  const { data } = useQuery(issuePullRequestsOptions(issueId));
  const linked = data?.pull_requests ?? [];

  // Nothing to narrate without a linked pull request, and the header would be
  // a promise the section cannot keep.
  if (linked.length === 0) return null;

  return (
    <div>
      <button
        type="button"
        className={cn(
          "mb-2 flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors hover:bg-accent/70",
          open ? "" : "text-muted-foreground hover:text-foreground",
        )}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
      >
        <ScrollText className="!size-3 shrink-0 text-muted-foreground" />
        {t(($) => $.walkthrough.section)}
        <ChevronRight
          className={cn(
            "!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform",
            open && "rotate-90",
          )}
        />
      </button>
      {open && (
        <div className="space-y-4 pl-2">
          {linked.map((pr) => (
            <PrWalkthroughBlock
              key={pr.id}
              issueId={issueId}
              pr={pr}
              currentUserId={currentUserId}
              canModerate={canModerate}
            />
          ))}
        </div>
      )}
    </div>
  );
}

function PrWalkthroughBlock({ issueId, pr, currentUserId, canModerate }: {
  issueId: string;
  pr: GitHubPullRequest;
  currentUserId?: string;
  canModerate?: boolean;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: walkthrough, isLoading } = useQuery(prWalkthroughOptions(wsId, issueId, pr.id));
  const refresh = useRefreshPrWalkthrough(wsId, issueId, pr.id);
  // Every head's threads, not just the current one: a question about a
  // revision that has since been pushed over is still worth reading, marked
  // stale, next to the code that replaced it.
  const { data: anchored } = useQuery(anchoredThreadsOptions(wsId, issueId, pr.id));

  return (
    <section className="space-y-2" data-testid={`pr-walkthrough-${pr.id}`}>
      <header className="flex items-center gap-2">
        <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">
          #{pr.number} {pr.title}
        </span>
        <Button
          type="button"
          size="sm"
          variant="ghost"
          className="h-6 shrink-0 px-2 text-caption"
          disabled={refresh.isPending}
          aria-label={t(($) => $.walkthrough.refresh)}
          onClick={() => refresh.mutate()}
        >
          <RefreshCw className={cn("!size-3", refresh.isPending && "animate-spin")} />
          {t(($) => $.walkthrough.refresh)}
        </Button>
      </header>
      <PrWalkthroughBody
        walkthrough={walkthrough}
        isLoading={isLoading}
        issueId={issueId}
        prId={pr.id}
        threads={anchored?.threads ?? EMPTY_THREADS}
        currentUserId={currentUserId}
        canModerate={canModerate}
      />
    </section>
  );
}

/** Stable empty reference so a walkthrough with no threads never re-renders. */
const EMPTY_THREADS: AnchoredThread[] = [];

interface AnchorContext {
  issueId: string;
  prId: string;
  threads: AnchoredThread[];
  currentUserId?: string;
  canModerate?: boolean;
}

function PrWalkthroughBody({
  walkthrough,
  isLoading,
  issueId,
  prId,
  threads,
  currentUserId,
  canModerate,
}: {
  walkthrough: PrWalkthrough | undefined;
  isLoading: boolean;
} & AnchorContext) {
  const { t } = useT("issues");
  if (isLoading || !walkthrough) return <PrWalkthroughSkeleton />;

  // Server-driven enum: an unknown state from a newer build reads as pending
  // rather than blanking the block.
  switch (walkthrough.state) {
    case "failed":
      return (
        <p className="flex items-start gap-2 rounded-md bg-muted/50 px-3 py-2 text-caption text-muted-foreground">
          <TriangleAlert className="mt-0.5 !size-3 shrink-0" />
          <span>{walkthrough.error || t(($) => $.walkthrough.failed)}</span>
        </p>
      );
    case "ready":
      break;
    default:
      return <PrWalkthroughSkeleton />;
  }

  const groups = orderedGroups(walkthrough.groups ?? []);
  if (groups.length === 0) {
    return <p className="px-3 py-2 text-caption text-muted-foreground">{t(($) => $.walkthrough.empty)}</p>;
  }

  return (
    <div className="space-y-3">
      {walkthrough.truncated === true && (
        <p className="rounded-md bg-muted/50 px-3 py-2 text-caption text-muted-foreground">
          {t(($) => $.walkthrough.truncated, { count: walkthrough.omitted_files ?? 0 })}
        </p>
      )}
      {groups.map((group, index) => (
        <PrWalkthroughGroupBlock
          key={`${group.kind}-${index}`}
          group={group}
          issueId={issueId}
          prId={prId}
          threads={threads}
          currentUserId={currentUserId}
          canModerate={canModerate}
        />
      ))}
    </div>
  );
}

function PrWalkthroughGroupBlock({ group, ...anchors }: { group: PrWalkthroughGroup } & AnchorContext) {
  const { t } = useT("issues");
  const kind = normalizeKind(group.kind);
  const hunks = groupHunkCount(group);
  return (
    <article className="space-y-2 rounded-md border border-border/60 p-3" data-kind={kind}>
      <header className="space-y-1">
        <div className="flex items-baseline gap-2">
          <h4 className="min-w-0 flex-1 text-body font-medium">{group.title}</h4>
          <span className="shrink-0 text-caption text-muted-foreground">{t(($) => $.walkthrough.kind[kind])}</span>
        </div>
        {group.rationale && <p className="text-caption text-muted-foreground">{group.rationale}</p>}
        <p className="text-caption text-muted-foreground">{t(($) => $.walkthrough.hunk_count, { count: hunks })}</p>
      </header>
      <div className="space-y-2">
        {(group.files ?? []).map((file, fileIndex) => (
          <div key={`${file.path}-${fileIndex}`} className="space-y-1">
            <p className="font-mono text-caption text-muted-foreground" title={file.path}>
              {middleTruncate(file.path)}
            </p>
            {(file.hunks ?? []).map((hunk, hunkIndex) => (
              <PrWalkthroughHunkBlock
                key={hunkIndex}
                hunk={hunk}
                filePath={file.path}
                {...anchors}
              />
            ))}
          </div>
        ))}
      </div>
    </article>
  );
}

/**
 * One hunk: its explanation, its lines with real file line numbers, and the
 * discussions anchored into it.
 *
 * The body is rendered line by line rather than as one highlighted diff block
 * because the whole point of the feature is that a reviewer can point at a
 * LINE. A block gives nothing to point at, and computing the number a click
 * means from a rendered token stream would be guesswork.
 */
function PrWalkthroughHunkBlock({
  hunk,
  filePath,
  issueId,
  prId,
  threads,
  currentUserId,
  canModerate,
}: { hunk: PrWalkthroughHunk; filePath: string } & AnchorContext) {
  const { t } = useT("issues");
  // The line a question is being written about, or null. Owned here rather
  // than per row so the composer renders under the hunk instead of inside a
  // table cell, and so opening one closes any other.
  const [asking, setAsking] = useState<{ line: number; side: "old" | "new" } | null>(null);
  const lines = useMemo(() => hunkLines(hunk), [hunk]);
  // A thread belongs to this hunk when its file matches and its range falls
  // inside the lines shown. A thread whose file the walkthrough does not cover
  // — or whose lines this hunk does not reach — stays in the timeline alone.
  const hunkThreads = useMemo(
    () =>
      threads.filter((th) => {
        const a = th.anchor;
        if (!a || a.file_path !== filePath) return false;
        return hunkContainsAnchor(lines, a.side, a.line_start, a.line_end || a.line_start);
      }),
    [threads, filePath, lines],
  );

  return (
    <div className="space-y-1">
      {hunk.moved_from && (
        <p className="text-caption text-muted-foreground">
          {t(($) => $.walkthrough.moved_from, { path: middleTruncate(hunk.moved_from) })}
        </p>
      )}
      {hunk.explanation && <p className="text-caption">{hunk.explanation}</p>}
      {lines.length > 0 && (
        <div className="overflow-x-auto rounded-md bg-muted/40">
          <table className="w-full border-separate border-spacing-0 font-mono text-caption">
            <tbody>
              {lines.map((line, i) => (
                <PrWalkthroughLineRow key={i} line={line} onAsk={setAsking} />
              ))}
            </tbody>
          </table>
        </div>
      )}
      {asking && (
        <AnchorComposer
          issueId={issueId}
          location={`${middleTruncate(filePath, 32)}:${asking.line}`}
          currentUserId={currentUserId}
          onDone={() => setAsking(null)}
          anchor={{
            pr_id: prId,
            file_path: filePath,
            line_start: asking.line,
            line_end: asking.line,
            side: asking.side,
          }}
        />
      )}
      {hunkThreads.length > 0 && (
        <div className="space-y-2 pt-1">
          {hunkThreads.map((th) => (
            <DiffAnchorThread
              key={th.root.id}
              issueId={issueId}
              thread={th}
              currentUserId={currentUserId}
              canModerate={canModerate}
            />
          ))}
        </div>
      )}
    </div>
  );
}

const LINE_TONE: Record<HunkLine["kind"], string> = {
  add: "bg-success/10",
  del: "bg-destructive/10",
  context: "",
  meta: "text-muted-foreground",
};

/** One diff line, with its numbers and the affordance that anchors a thread. */
function PrWalkthroughLineRow({
  line,
  onAsk,
}: {
  line: HunkLine;
  onAsk: (target: { line: number; side: "old" | "new" }) => void;
}) {
  const { t } = useT("issues");
  const target = anchorForLine(line);
  const label = t(($) => $.anchor.ask_line);

  return (
    <tr className={cn("group", LINE_TONE[line.kind])} data-line-kind={line.kind}>
      <td className="w-10 select-none px-1 text-right align-top text-muted-foreground tabular-nums">
        {line.oldLine || ""}
      </td>
      <td className="w-10 select-none px-1 text-right align-top text-muted-foreground tabular-nums">
        {line.newLine || ""}
      </td>
      <td className="w-6 align-top">
        {/* Revealed on hover, and kept focusable so it is reachable without a
            pointer — a keyboard reviewer must be able to ask too. */}
        {target && (
          <span className="opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
            <AnchorAskButton label={label} onAsk={() => onAsk(target)} />
          </span>
        )}
      </td>
      <td className="whitespace-pre px-1 align-top">{line.text}</td>
    </tr>
  );
}

function PrWalkthroughSkeleton() {
  const { t } = useT("issues");
  return (
    <div className="space-y-2" aria-busy="true" aria-label={t(($) => $.walkthrough.pending)}>
      <div className="h-3 w-1/2 animate-pulse rounded bg-muted" />
      <div className="h-3 w-3/4 animate-pulse rounded bg-muted" />
      <div className="h-3 w-2/3 animate-pulse rounded bg-muted" />
    </div>
  );
}
