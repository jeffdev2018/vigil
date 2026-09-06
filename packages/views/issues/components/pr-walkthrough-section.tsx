"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, RefreshCw, ScrollText, TriangleAlert } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { issuePullRequestsOptions } from "@multica/core/github";
import type { GitHubPullRequest } from "@multica/core/types";
import {
  prWalkthroughOptions,
  useRefreshPrWalkthrough,
  groupHunkCount,
  middleTruncate,
  normalizeKind,
  orderedGroups,
  type PrWalkthrough,
  type PrWalkthroughGroup,
} from "@multica/core/pr-walkthrough";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { CodeBlockShell, StaticCodeBody } from "../../rich-content/rich-code-block";
import { useT } from "../../i18n";

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
export function PrWalkthroughSection({ issueId }: { issueId: string }) {
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
            <PrWalkthroughBlock key={pr.id} issueId={issueId} pr={pr} />
          ))}
        </div>
      )}
    </div>
  );
}

function PrWalkthroughBlock({ issueId, pr }: { issueId: string; pr: GitHubPullRequest }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: walkthrough, isLoading } = useQuery(prWalkthroughOptions(wsId, issueId, pr.id));
  const refresh = useRefreshPrWalkthrough(wsId, issueId, pr.id);

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
      <PrWalkthroughBody walkthrough={walkthrough} isLoading={isLoading} />
    </section>
  );
}

function PrWalkthroughBody({
  walkthrough,
  isLoading,
}: {
  walkthrough: PrWalkthrough | undefined;
  isLoading: boolean;
}) {
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
        <PrWalkthroughGroupBlock key={`${group.kind}-${index}`} group={group} />
      ))}
    </div>
  );
}

function PrWalkthroughGroupBlock({ group }: { group: PrWalkthroughGroup }) {
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
              <div key={hunkIndex} className="space-y-1">
                {hunk.moved_from && (
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.walkthrough.moved_from, { path: middleTruncate(hunk.moved_from) })}
                  </p>
                )}
                {hunk.explanation && <p className="text-caption">{hunk.explanation}</p>}
                {hunk.lines && (
                  <div className="overflow-x-auto">
                    <CodeBlockShell language="diff" code={hunk.lines}>
                      <StaticCodeBody language="diff" body={hunk.lines} />
                    </CodeBlockShell>
                  </div>
                )}
              </div>
            ))}
          </div>
        ))}
      </div>
    </article>
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
