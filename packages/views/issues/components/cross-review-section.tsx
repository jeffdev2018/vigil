"use client";

import { useQuery } from "@tanstack/react-query";
import { Check, ShieldCheck, X } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { crossReviewSignalOptions, crossReviewState, issueCrossReviewsOptions, useRetryCrossReview } from "@multica/core/issues/cross-review";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

/**
 * Cross-provider self-review (K15): the latest review run by another
 * provider on this issue's last diff — a discreet badge while it runs, a
 * retry button when it failed, the structured report (verdict, risks,
 * questions, suggestions) once done — with the reviewer's provider named.
 * Hidden when no review exists (one provider configured, or no diff yet).
 */
export function CrossReviewSection({ issueId, canManage = true }: { issueId: string; canManage?: boolean }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: reviews = [] } = useQuery(issueCrossReviewsOptions(wsId, issueId));
  const { data: signal = null } = useQuery(crossReviewSignalOptions(wsId, issueId));
  const retry = useRetryCrossReview(wsId, issueId);
  const latest = reviews[0];
  if (!latest) return null;
  const state = crossReviewState(latest);
  const report = latest.report;
  return (
    <div data-testid="cross-review" data-state={state} className={cn("flex flex-col gap-1.5 rounded-md border p-2 text-caption", state === "failed" ? "border-destructive/50" : "border-border")}>
      <div className="flex items-center gap-2 font-medium">
        <ShieldCheck className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span>{t(($) => $.cross_review.section)}</span>
        <span className="font-normal text-muted-foreground">{t(($) => $.cross_review.by, { name: latest.reviewer_name || latest.reviewer_agent_id.slice(0, 8), provider: latest.reviewer_provider || "?" })}</span>
        {state === "in_progress" && <span data-testid="cross-review-badge" className="ml-auto rounded bg-muted px-1 text-muted-foreground animate-pulse">{t(($) => $.cross_review.in_progress)}</span>}
        {state === "failed" && <span className="ml-auto rounded bg-destructive/15 px-1 text-destructive">{t(($) => $.cross_review.failed)}</span>}
        {state === "done" && report && (
          <span className={cn("ml-auto rounded px-1", report.verdict === "approve" ? "bg-success/15 text-success" : report.verdict === "request_changes" ? "bg-warning/20 text-warning" : "bg-muted text-muted-foreground")}>{t(($) => $.cross_review.verdict[report.verdict])}</span>
        )}
      </div>
      {signal?.kind === "rework" && (
        <p data-testid="cross-review-rework" className="rounded bg-warning/20 px-1.5 py-0.5 text-warning">{t(($) => $.cross_review.rework, { cycle: signal.cycle ?? 0 })}</p>
      )}
      {signal?.kind === "escalated" && (
        <p data-testid="cross-review-escalated" className="rounded bg-destructive/15 px-1.5 py-0.5 text-destructive">{t(($) => $.cross_review.escalated, { cycles: signal.cycles ?? 0 })}</p>
      )}
      {state === "done" && report && (
        <div className="flex flex-col gap-1">
          {report.summary && <p className="text-muted-foreground">{report.summary}</p>}
          {(report.checklist_results?.length ?? 0) > 0 && (
            <div data-testid="cross-review-checklist">
              <div className="font-medium">{t(($) => $.cross_review.checklist)}</div>
              <ul className="flex flex-col gap-0.5">
                {(report.checklist_results ?? []).map((r, i) => (
                  <li key={i} data-pass={r.pass} className="flex items-start gap-1.5">
                    {r.pass
                      ? <Check className="mt-0.5 h-3.5 w-3.5 shrink-0 text-success" aria-hidden="true" />
                      : <X className="mt-0.5 h-3.5 w-3.5 shrink-0 text-destructive" aria-hidden="true" />}
                    <span className="min-w-0">
                      {r.item}
                      {r.note && <span className="text-muted-foreground"> — {r.note}</span>}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
          {([["risks", report.risks], ["questions", report.questions], ["suggestions", report.suggestions]] as const).map(([key, items]) => items.length > 0 && (
            <div key={key} data-testid={`cross-review-${key}`}>
              <div className="font-medium">{t(($) => $.cross_review[key])}</div>
              <ul className="list-disc pl-4">{items.map((item, i) => <li key={i}>{item}</li>)}</ul>
            </div>
          ))}
        </div>
      )}
      {state === "failed" && canManage && (
        <Button type="button" size="sm" variant="outline" className="self-start" disabled={retry.isPending} onClick={() => retry.mutate(undefined, { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.cross_review.retry_failed)) })}>
          {t(($) => $.cross_review.retry)}
        </Button>
      )}
      {reviews.length > 1 && <p className="text-muted-foreground">{t(($) => $.cross_review.previous, { count: reviews.length - 1 })}</p>}
    </div>
  );
}
