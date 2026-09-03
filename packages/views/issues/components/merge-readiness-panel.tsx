"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, CircleDashed, TriangleAlert } from "lucide-react";
import { issueMergeReadinessOptions } from "@multica/core/github/queries";
import { effectiveMergeReady } from "@multica/core/github/merge-readiness";
import type { MergeBlocker } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

const BLOCKERS_FOLDED = 5;

/**
 * Merge readiness (F10): one chip answering "can this be merged" and the
 * ordered blockers that say why not. Read-only; the data refreshes on the
 * existing PR, comment and issue events.
 */
export function MergeReadinessPanel({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const [expanded, setExpanded] = useState(false);
  const { data } = useQuery(issueMergeReadinessOptions(issueId));
  if (!data) return null;

  const ready = effectiveMergeReady(data);
  const stale = data.prs.some((pr) => pr.stale_snapshot === true);
  const blockers = expanded ? data.blockers : data.blockers.slice(0, BLOCKERS_FOLDED);
  const hidden = data.blockers.length - blockers.length;

  return (
    <div data-testid="merge-readiness" className="flex flex-col gap-1 py-1 text-caption">
      <span
        data-ready={ready ? "true" : "false"}
        className={cn("inline-flex items-center gap-1 font-medium", ready ? "text-success" : "text-muted-foreground")}
      >
        {ready ? (
          <CheckCircle2 aria-hidden className="h-3.5 w-3.5" />
        ) : stale ? (
          <TriangleAlert aria-hidden className="h-3.5 w-3.5 text-warning" />
        ) : (
          <CircleDashed aria-hidden className="h-3.5 w-3.5" />
        )}
        {ready ? t(($) => $.detail.merge_ready) : t(($) => $.detail.merge_not_ready)}
        {stale && (
          <span className="font-normal opacity-70">· {t(($) => $.detail.merge_stale_hint)}</span>
        )}
      </span>
      {blockers.length > 0 && (
        <ul className="flex flex-col gap-0.5 pl-4 text-muted-foreground">
          {blockers.map((b, i) => (
            <li key={`${b.kind}-${i}`} className="truncate" title={b.label}>
              <BlockerLabel blocker={b} />
            </li>
          ))}
        </ul>
      )}
      {hidden > 0 && (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="self-start pl-4 text-muted-foreground hover:text-foreground"
        >
          {t(($) => $.detail.merge_blockers_more, { count: hidden })}
        </button>
      )}
    </div>
  );
}

function BlockerLabel({ blocker }: { blocker: MergeBlocker }) {
  const { t } = useT("issues");
  const pr = blocker.pr_number ?? 0;
  const count = blocker.count ?? 0;
  switch (blocker.kind) {
    case "checks_failing":
      return <>{t(($) => $.detail.blocker_checks_failing, { pr })}</>;
    case "checks_pending":
      return <>{t(($) => $.detail.blocker_checks_pending, { pr })}</>;
    case "merge_conflict":
      return <>{t(($) => $.detail.blocker_merge_conflict, { pr })}</>;
    case "merge_not_clean":
      return <>{t(($) => $.detail.blocker_merge_not_clean, { pr })}</>;
    case "stale_snapshot":
      return <>{t(($) => $.detail.blocker_stale_snapshot, { pr })}</>;
    case "unresolved_threads":
      return <>{t(($) => $.detail.blocker_unresolved_threads, { count })}</>;
    case "open_todos":
      return <>{t(($) => $.detail.blocker_open_todos, { count })}</>;
    case "blocking_issue":
      return <>{t(($) => $.detail.blocker_blocking_issue, { identifier: blocker.issue_identifier ?? "" })}</>;
    case "no_pr":
      return <>{t(($) => $.detail.blocker_no_pr)}</>;
    default:
      // A kind this build predates: still a blocker, shown as the server named it.
      return <>{blocker.label || blocker.kind}</>;
  }
}
