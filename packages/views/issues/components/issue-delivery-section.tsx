"use client";

import { useEffect, useId, useState } from "react";
import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { deliveryHistoryOptions, deliveryTasksOptions, issueDeliveryOptions, useReviewDelivery, useStartDeliveryCorrection, useUpdateDeliveryCriteria, type DeliveryReview, type ReviewDeliveryInput } from "@multica/core/issues";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";
import { collectUnmappedModels, formatUsd, summarizeTaskUsageAcross } from "../../runtimes/utils";
import { IssueUsageDialog } from "./issue-usage-dialog";
import { TranscriptButton } from "../../common/task-transcript";
import { TeachFromReviewButton } from "../../agents/components/tabs/memory-tab";
import { TeachProjectFromReviewButton } from "../../projects/components/project-memory-section";

type ReviewDraft = ReviewDeliveryInput & { criteria: string[]; startedAtMs: number };

export function IssueDeliverySection({
  wsId,
  issueId,
  identifier,
  getActorName,
  boardStatusIsReview = false,
  canProposeDone = false,
  onMarkDone,
  projectId,
}: {
  wsId: string;
  issueId: string;
  identifier: string;
  getActorName?: (type: "member" | "agent", id: string) => string;
  /** True when the issue's status category is `in_review` (board signal only). */
  boardStatusIsReview?: boolean;
  /** True when the issue is not already done/cancelled — offer Done after accept. */
  canProposeDone?: boolean;
  /** Optional board status write; never called unless the human confirms. */
  onMarkDone?: () => void;
  /** When set, a changes_requested review can promote shared project memory. */
  projectId?: string | null;
}) {
  const { t } = useT("issues");
  const { data, isPending, isError, refetch } = useQuery(issueDeliveryOptions(wsId, issueId));
  const { data: tasks = [], isError: costError } = useQuery(deliveryTasksOptions(wsId, issueId));
  const criteriaMutation = useUpdateDeliveryCriteria(wsId, issueId);
  const reviewMutation = useReviewDelivery(wsId, issueId);
  const correctionMutation = useStartDeliveryCorrection(wsId, issueId);
  const [criteriaDraft, setCriteriaDraft] = useState<{ text: string; revision: number } | null>(null);
  const [reviewDraft, setReviewDraft] = useState<ReviewDraft | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [costOpen, setCostOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  const [proposeDone, setProposeDone] = useState(false);
  const history = useInfiniteQuery({ ...deliveryHistoryOptions(wsId, issueId), enabled: historyOpen && Boolean(wsId && issueId) });
  const id = useId();
  // Pricing helpers read the store directly; subscribe so edits reprice the card.
  useCustomPricingStore((s) => s.pricings);
  const cost = summarizeTaskUsageAcross(tasks.map((task) => task.usage));
  const slices = tasks.flatMap((task) => task.usage ?? []);
  const incompleteCost = tasks.some((task) => !task.usage?.length) || collectUnmappedModels(slices).length > 0;
  const estimatedCost = slices.some((slice) => slice.cost_usd_ticks == null);
  const criteria = (criteriaDraft?.text ?? "").split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  const validCriteria = criteria.length <= 20 && criteria.every((criterion) => [...criterion].length <= 500);
  const busy = criteriaMutation.isPending || reviewMutation.isPending || correctionMutation.isPending;
  const draftStale = reviewDraft !== null && data !== undefined && (
    reviewDraft.snapshotToken !== data.snapshotToken || reviewDraft.expectedReviewId !== (data.latestReview?.id ?? "")
  );
  const unavailable = isError || (!isPending && !data);
  const runTask = tasks.find((task) => task.id === data?.run?.id);
  const reviewedTask = tasks.find((task) => task.id === data?.latestReview?.snapshot.run?.id);
  const runReady = data?.run?.status === "completed" && Boolean(data.run.completedAt) && data.criteria.length > 0;
  const needsHumanDecision = Boolean(runReady && data && (
    data.reviewStale ||
    data.latestReview == null ||
    data.latestReview.decision !== "accepted"
  ));
  const needsCorrectionLaunch = Boolean(
    data?.latestReview?.decision === "changes_requested" &&
      !data.latestReview.correctionTaskId &&
      !data.reviewStale &&
      data.latestReview.snapshotToken === data.snapshotToken,
  );
  const showProposeDone = proposeDone && canProposeDone && Boolean(onMarkDone);

  useEffect(() => {
    if (!canProposeDone) setProposeDone(false);
  }, [canProposeDone]);

  const failed = (err: Error) => setError(err instanceof ApiError && err.status === 409
    ? t(($) => $.delivery.conflict) : t(($) => $.delivery.save_failed));
  const startReview = () => {
    if (!data) return;
    setError(null);
    setReviewDraft({ reviewId: crypto.randomUUID(), expectedReviewId: data.latestReview?.id ?? "",
      snapshotToken: data.snapshotToken, decision: "accepted", feedback: "", criteria: [...data.criteria],
      assessments: data.criteria.map(() => ({ passed: false, evidence: "" })), startedAtMs: Date.now() });
  };
  const decide = (decision: ReviewDeliveryInput["decision"]) => {
    if (!reviewDraft || draftStale) return;
    setError(null);
    const humanEffortSeconds = Math.min(24 * 60 * 60, Math.max(0, Math.round((Date.now() - reviewDraft.startedAtMs) / 1000)));
    const { criteria: _criteria, startedAtMs: _startedAtMs, ...input } = reviewDraft;
    reviewMutation.mutate({ ...input, decision, humanEffortSeconds }, {
      onSuccess: () => {
        setReviewDraft(null);
        setProposeDone(decision === "accepted" && canProposeDone && Boolean(onMarkDone));
      },
      onError: failed,
    });
  };

  return <section className="mt-8 min-w-0 space-y-4 rounded-lg border p-4" aria-label={t(($) => $.delivery.title)}>
    <div className="flex flex-wrap items-center justify-between gap-3">
      <h2 className="text-body font-medium">{t(($) => $.delivery.title)}</h2>
      {data && !unavailable && <Button variant="outline" size="sm" disabled={busy} onClick={() => {
        setError(null); setCriteriaDraft({ text: data.criteria.join("\n"), revision: data.revision });
      }}>{t(($) => $.delivery.edit_criteria)}</Button>}
    </div>
    <p className="text-caption text-muted-foreground">{t(($) => $.delivery.description)}</p>
    <p className="text-caption text-muted-foreground">{t(($) => $.delivery.honesty)}</p>
    {boardStatusIsReview && (
      <p role="note" className="rounded-md border border-border/60 bg-muted/40 px-3 py-2 text-caption text-muted-foreground">
        {t(($) => $.delivery.board_status_note)}
      </p>
    )}
    {needsHumanDecision && !unavailable && data && (
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/50 px-3 py-2">
        <p className="text-caption">{needsCorrectionLaunch
          ? t(($) => $.delivery.loop_correction_pending)
          : t(($) => $.delivery.loop_review_pending)}</p>
        {needsCorrectionLaunch ? (
          <Button size="sm" disabled={busy} onClick={() => {
            if (!data.latestReview) return;
            setError(null);
            correctionMutation.mutate(data.latestReview.id);
          }}>{t(($) => $.delivery.start_correction)}</Button>
        ) : (
          <Button size="sm" disabled={busy || !runReady} onClick={startReview}>
            {t(($) => $.delivery.review)}
          </Button>
        )}
      </div>
    )}
    {showProposeDone && (
      <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/50 px-3 py-2">
        <div className="min-w-0 space-y-1">
          <p className="text-caption font-medium">{t(($) => $.delivery.propose_done_title)}</p>
          <p className="text-caption text-muted-foreground">{t(($) => $.delivery.propose_done_hint)}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button variant="ghost" size="sm" disabled={busy} onClick={() => setProposeDone(false)}>
            {t(($) => $.delivery.propose_done_dismiss)}
          </Button>
          <Button size="sm" disabled={busy} onClick={() => {
            onMarkDone?.();
            setProposeDone(false);
          }}>
            {t(($) => $.delivery.propose_done_confirm)}
          </Button>
        </div>
      </div>
    )}
    {isPending ? <p className="text-caption">{t(($) => $.delivery.loading)}</p> : unavailable ? <div role="alert" className="space-y-2">
      <p className="text-caption">{t(($) => $.delivery.load_failed)}</p>
      <Button variant="outline" size="sm" onClick={() => void refetch()}>{t(($) => $.delivery.reload)}</Button>
    </div> : data && <>
      {data.criteria.length === 0 ? <p className="text-caption text-muted-foreground">{t(($) => $.delivery.no_criteria)}</p> :
        <ol className="list-decimal space-y-2 pl-5 text-body">{data.criteria.map((criterion, index) => <li key={index} className="break-words">{criterion}</li>)}</ol>}
      {data.run ? <div className="space-y-2">
        <div className="flex items-center gap-2">
          <h3 className="text-caption font-medium">{t(($) => $.delivery.result)}</h3>
          {runTask && <TranscriptButton key={runTask.id} task={runTask} agentName={getActorName?.("agent", runTask.agent_id) || runTask.agent_id}
            title={t(($) => $.delivery.view_transcript)} isLive={!["completed", "failed", "cancelled"].includes(runTask.status)} />}
        </div>
        <p className="text-caption text-muted-foreground">{data.run.completedAt
          ? t(($) => $.delivery.run_completed, { date: new Date(data.run.completedAt).toLocaleString() })
          : t(($) => $.delivery.run_pending)}</p>
        <DeliveryResult result={data.run.result} />
        {data.run.error && <p className="whitespace-pre-wrap break-words text-caption text-destructive">{data.run.error}</p>}
      </div> : <p className="text-caption text-muted-foreground">{t(($) => $.delivery.no_run)}</p>}
      {data.pullRequests.length > 0 && <div className="space-y-2">
        <h3 className="text-caption font-medium">{t(($) => $.delivery.proof_sources)}</h3>
        {data.pullRequests.map((pr) => <div key={pr.id} className="min-w-0 space-y-1 text-caption">
          {/^https?:\/\//i.test(pr.html_url) ? <a href={pr.html_url} target="_blank" rel="noreferrer" className="break-words font-medium underline underline-offset-4">{pr.title}</a> : <span>{pr.title}</span>}
          <p className="break-all text-muted-foreground">{pr.headSha || t(($) => $.delivery.unknown_commit)}</p>
          <p className="text-muted-foreground">{pr.snapshot_available === false || pr.checks_total === 0
            ? t(($) => $.delivery.checks_unavailable)
            : t(($) => $.delivery.checks, { passed: pr.checks_passed, total: pr.checks_total })}
            {pr.snapshot_stale === true && <> · {t(($) => $.delivery.stale_checks)}</>}</p>
          {pr.snapshot_fetched_at && <p className="text-muted-foreground">{t(($) => $.delivery.fetched_at, { date: new Date(pr.snapshot_fetched_at).toLocaleString() })}</p>}
        </div>)}
      </div>}
      <div className="flex flex-wrap items-center gap-2 text-caption">
        <span className="text-muted-foreground">{t(($) => $.delivery.cost)}</span>
        {cost && !costError ? <>
          <Button variant="ghost" size="sm" onClick={() => setCostOpen(true)}>{formatUsd(cost.cost)}</Button>
          {estimatedCost && <span>{t(($) => $.delivery.estimated)}</span>}
          {incompleteCost && <span>{t(($) => $.delivery.incomplete)}</span>}
        </> : <span>{t(($) => $.delivery.cost_unavailable)}</span>}
      </div>
      {data.latestReview && <div className="space-y-2 rounded-md bg-muted/50 p-3 text-caption">
        <p className="font-medium">{data.reviewStale ? t(($) => $.delivery.outdated_review)
          : data.latestReview.decision === "accepted" ? t(($) => $.delivery.accepted) : t(($) => $.delivery.changes_requested)}</p>
        <p className="break-words text-muted-foreground">{t(($) => $.delivery.reviewed_by, {
          name: getActorName?.("member", data.latestReview.reviewedBy) || data.latestReview.reviewedBy,
          date: new Date(data.latestReview.createdAt).toLocaleString(),
        })}</p>
        <DeliveryUsageAtReview review={data.latestReview} />
        {data.latestReview.feedback && <p className="whitespace-pre-wrap break-words">{data.latestReview.feedback}</p>}
        {data.latestReview.decision === "changes_requested" && reviewedTask?.agent_id && (
          <TeachFromReviewButton
            key={data.latestReview.id}
            wsId={wsId}
            agentId={reviewedTask.agent_id}
            sourceTaskId={reviewedTask.id}
            review={data.latestReview}
          />
        )}
        {data.latestReview.decision === "changes_requested" && projectId && (
          <TeachProjectFromReviewButton key={`project-${data.latestReview.id}`} wsId={wsId} projectId={projectId} review={data.latestReview} />
        )}
        <details>
          <summary className="cursor-pointer">{t(($) => $.delivery.review_evidence)}</summary>
          {reviewedTask && <TranscriptButton key={reviewedTask.id} task={reviewedTask} agentName={getActorName?.("agent", reviewedTask.agent_id) || reviewedTask.agent_id}
            title={t(($) => $.delivery.view_reviewed_transcript)} />}
          <ol className="mt-2 list-decimal space-y-2 pl-4">{data.latestReview.snapshot.criteria.map((criterion, i) => <li key={i}>
            <p className="break-words font-medium">{criterion}</p>
            <p className="whitespace-pre-wrap break-words">{data.latestReview?.assessments[i]?.evidence}</p>
          </li>)}</ol>
        </details>
        {data.latestReview.decision === "changes_requested" && (data.latestReview.correctionTaskId
          ? <p role="status">{t(($) => $.delivery.correction_started)}</p>
          : !needsCorrectionLaunch && <div className="space-y-2">
            <p className="text-muted-foreground">{t(($) => $.delivery.correction_hint)}</p>
            <Button variant="outline" size="sm" disabled={busy || data.reviewStale || data.latestReview.snapshotToken !== data.snapshotToken}
              onClick={() => {
                if (!data.latestReview) return;
                setError(null);
                correctionMutation.mutate(data.latestReview.id);
              }}>{t(($) => $.delivery.start_correction)}</Button>
          </div>)}
      </div>}
      {data.metrics && data.metrics.reviewCount > 0 && <div className="space-y-2 text-caption">
        <p>{t(($) => $.delivery.result_counts, { accepted: data.metrics.acceptedResults, reviewed: data.metrics.reviewedResults })}</p>
        <p>{t(($) => $.delivery.correction_counts, { corrections: data.metrics.correctionRequests, reversals: data.metrics.acceptanceReversals })}</p>
        <p className="text-muted-foreground">{t(($) => $.delivery.metrics_hint)}</p>
      </div>}
      <details open={historyOpen} onToggle={(event) => setHistoryOpen(event.currentTarget.open)} className="space-y-3 text-caption">
        <summary className="cursor-pointer">{t(($) => $.delivery.history)}</summary>
        {historyOpen && <>
          {history.isPending && <p>{t(($) => $.delivery.loading)}</p>}
          {history.isError && <div role="alert"><p>{t(($) => $.delivery.load_failed)}</p><Button variant="outline" onClick={() => void history.refetch()}>{t(($) => $.delivery.reload)}</Button></div>}
          {!history.isPending && !history.isError && history.data?.pages.every((page) => page.reviews.length === 0) && <p>{t(($) => $.delivery.history_empty)}</p>}
          {history.data?.pages.flatMap((page) => page.reviews).map((review) => {
            const historicalTask = tasks.find((task) => task.id === review.snapshot.run?.id);
            return <div key={review.id} className="space-y-3 rounded-lg border p-3">
              <p className="font-medium">{review.decision === "accepted" ? t(($) => $.delivery.accepted) : t(($) => $.delivery.changes_requested)}</p>
              <p className="break-words text-muted-foreground">{t(($) => $.delivery.reviewed_by, { name: getActorName?.("member", review.reviewedBy) || review.reviewedBy, date: new Date(review.createdAt).toLocaleString() })}</p>
              {review.feedback && <p className="whitespace-pre-wrap break-words">{review.feedback}</p>}
              {review.decision === "changes_requested" && historicalTask?.agent_id && (
                <TeachFromReviewButton wsId={wsId} agentId={historicalTask.agent_id} sourceTaskId={historicalTask.id} review={review} />
              )}
              {review.decision === "changes_requested" && projectId && (
                <TeachProjectFromReviewButton wsId={wsId} projectId={projectId} review={review} />
              )}
              <DeliveryUsageAtReview review={review} />
              <details className="space-y-2">
                <summary className="cursor-pointer">{t(($) => $.delivery.review_evidence)}</summary>
                {historicalTask && <TranscriptButton task={historicalTask} agentName={getActorName?.("agent", historicalTask.agent_id) || historicalTask.agent_id} title={t(($) => $.delivery.view_reviewed_transcript)} />}
                {review.snapshot.run && <DeliveryResult result={review.snapshot.run.result} />}
                <ol className="list-decimal space-y-2 pl-4">{review.snapshot.criteria.map((criterion, index) => <li key={index} className="break-words"><p className="font-medium">{criterion}</p><p className="whitespace-pre-wrap">{review.assessments[index]?.evidence}</p></li>)}</ol>
                {review.snapshot.pullRequests.map((pr) => <p key={pr.id} className="break-all">{/^https?:\/\//i.test(pr.html_url) ? <a href={pr.html_url} target="_blank" rel="noreferrer" className="underline">{pr.title}</a> : pr.title} · {pr.headSha}</p>)}
              </details>
            </div>;
          })}
          {history.hasNextPage && <Button variant="outline" disabled={history.isFetchingNextPage} onClick={() => void history.fetchNextPage()}>{t(($) => $.delivery.older_reviews)}</Button>}
        </>}
      </details>
      {correctionMutation.isError && correctionMutation.variables === data.latestReview?.id && !data.latestReview?.correctionTaskId &&
        <p role="alert" className="text-caption text-destructive">{t(($) => $.delivery.correction_failed)}</p>}
      {!(needsHumanDecision && !needsCorrectionLaunch) && (
        <Button variant="outline" disabled={busy || !runReady} onClick={startReview}>
          {t(($) => $.delivery.review)}
        </Button>
      )}
    </>}
    <Dialog open={criteriaDraft !== null} onOpenChange={(open) => { if (!open && !busy) setCriteriaDraft(null); }}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader><DialogTitle>{t(($) => $.delivery.edit_criteria)}</DialogTitle><DialogDescription>{t(($) => $.delivery.criteria_hint)}</DialogDescription></DialogHeader>
        <Label htmlFor={`${id}-criteria`}>{t(($) => $.delivery.criteria)}</Label>
        <Textarea id={`${id}-criteria`} rows={6} maxLength={20040} disabled={busy} value={criteriaDraft?.text ?? ""}
          onChange={(event) => setCriteriaDraft((draft) => draft && { ...draft, text: event.target.value })} />
        {error && <p role="alert" className="text-caption text-destructive">{error}</p>}
        <DialogFooter>
          <Button variant="ghost" disabled={busy} onClick={() => setCriteriaDraft(null)}>{t(($) => $.delivery.cancel)}</Button>
          <Button disabled={busy || !validCriteria} onClick={() => {
            if (!criteriaDraft) return;
            setError(null);
            criteriaMutation.mutate({ criteria, expectedRevision: criteriaDraft.revision }, { onSuccess: () => setCriteriaDraft(null), onError: failed });
          }}>{t(($) => $.delivery.save)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
    <Dialog open={reviewDraft !== null} onOpenChange={(open) => { if (!open && !busy) setReviewDraft(null); }}>
      <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-xl">
        <DialogHeader><DialogTitle>{t(($) => $.delivery.review)}</DialogTitle><DialogDescription>{t(($) => $.delivery.review_hint)}</DialogDescription></DialogHeader>
        {reviewDraft?.criteria.map((criterion, index) => <div key={index} className="space-y-2">
          <div className="flex items-start gap-2">
            <Checkbox id={`${id}-pass-${index}`} disabled={busy} checked={reviewDraft.assessments[index]?.passed === true}
              onCheckedChange={(checked) => setReviewDraft((draft) => draft && { ...draft, assessments: draft.assessments.map((a, i) => i === index ? { ...a, passed: checked === true } : a) })} />
            <Label htmlFor={`${id}-pass-${index}`} className="break-words leading-relaxed">{criterion}</Label>
          </div>
          <Label htmlFor={`${id}-evidence-${index}`}>{t(($) => $.delivery.evidence, { number: index + 1 })}</Label>
          <Textarea id={`${id}-evidence-${index}`} maxLength={2000} rows={2} disabled={busy} value={reviewDraft.assessments[index]?.evidence ?? ""}
            onChange={(event) => setReviewDraft((draft) => draft && { ...draft, assessments: draft.assessments.map((a, i) => i === index ? { ...a, evidence: event.target.value } : a) })} />
        </div>)}
        <Label htmlFor={`${id}-feedback`}>{t(($) => $.delivery.feedback)}</Label>
        <Textarea id={`${id}-feedback`} rows={3} maxLength={4000} disabled={busy} value={reviewDraft?.feedback ?? ""}
          onChange={(event) => setReviewDraft((draft) => draft && { ...draft, feedback: event.target.value })} />
        {(error || draftStale) && <p role="alert" className="text-caption text-destructive">{draftStale ? t(($) => $.delivery.conflict) : error}</p>}
        <DialogFooter className="flex-wrap">
          <Button variant="ghost" disabled={busy} onClick={() => setReviewDraft(null)}>{t(($) => $.delivery.cancel)}</Button>
          <Button variant="outline" disabled={busy || draftStale || !reviewDraft?.feedback.trim()} onClick={() => decide("changes_requested")}>{t(($) => $.delivery.request_changes)}</Button>
          <Button disabled={busy || draftStale || !reviewDraft?.assessments.every((a) => a.passed === true && a.evidence.trim().length > 0)} onClick={() => decide("accepted")}>{t(($) => $.delivery.accept)}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
    {costOpen && <IssueUsageDialog open onOpenChange={setCostOpen} identifier={identifier} tasks={tasks} />}
  </section>;
}

function DeliveryResult({ result }: { result: unknown }) {
  const { t } = useT("issues");
  if (result == null) return <p className="text-caption text-muted-foreground">{t(($) => $.delivery.result_unavailable)}</p>;
  const summary = typeof result === "object" && "summary" in result ? result.summary : result;
  const text = typeof summary === "string" ? summary : JSON.stringify(result, null, 2);
  return <p className="max-h-64 overflow-auto whitespace-pre-wrap break-words text-body">{text}</p>;
}

function DeliveryUsageAtReview({ review }: { review: DeliveryReview }) {
  const { t } = useT("issues");
  const usage = review.usageSnapshot;
  return <div className="space-y-2 text-caption">
    <p className="font-medium">{review.decision === "accepted" ? t(($) => $.delivery.accepted_cost) : t(($) => $.delivery.review_cost)}: {usage?.availableUsd != null ? <span title={usage.availableUsd}>{formatUsd(Number(usage.availableUsd))}</span> : t(($) => $.delivery.cost_unavailable)}</p>
    {usage && <>
      <p>{usage.status === "reported" ? t(($) => $.delivery.reported_cost) : usage.status === "estimated" ? t(($) => $.delivery.estimated) : usage.status === "partial" ? t(($) => $.delivery.incomplete) : t(($) => $.delivery.cost_unavailable)}</p>
      <p className="text-muted-foreground">{t(($) => $.delivery.cost_scope, { count: usage.runIds.length })}</p>
      <details>
        <summary className="cursor-pointer">{t(($) => $.delivery.cost_breakdown)}</summary>
        <p>{t(($) => $.delivery.cost_split, { reported: formatUsd(Number(usage.reportedUsd)), estimated: formatUsd(Number(usage.estimatedUsd)) })}</p>
        <p>{t(($) => $.delivery.cost_gaps, { missing: usage.runsWithoutUsage, unpriced: usage.unpricedSlices, running: usage.nonterminalRuns })}</p>
        <p className="text-muted-foreground">{t(($) => $.delivery.cost_snapshot_hint, { date: new Date(usage.capturedAt).toLocaleString() })}</p>
      </details>
    </>}
    {review.reviewDelaySeconds != null && <p className="text-muted-foreground">{t(($) => $.delivery.review_delay, { minutes: (review.reviewDelaySeconds / 60).toLocaleString(undefined, { maximumFractionDigits: 1 }) })}</p>}
    {review.humanEffortSeconds != null && <p className="text-muted-foreground">{t(($) => $.delivery.human_effort, { seconds: review.humanEffortSeconds.toLocaleString() })}</p>}
  </div>;
}
