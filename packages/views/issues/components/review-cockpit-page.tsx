"use client";

import { useId, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, Circle, Clock, ExternalLink } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useCanonicalIssue } from "@multica/core/issues/canonical-id";
import { cockpitChecksPending, reviewCockpitOptions, usdFromTicks } from "@multica/core/issues/cockpit";
import { useCreateComment, useUpdateIssue } from "@multica/core/issues/mutations";
import { issueKeys } from "@multica/core/issues/queries";
import type { ReviewCockpit } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { formatTokens, formatUsd } from "../../runtimes/utils";
import { LoadErrorState } from "../../common/load-error-state";
import { IssueNotFound, IssueDetailSkeleton } from "./issue-detail";
import { CommentTriggerChips } from "./comment-trigger-chips";
import { useCommentTriggerPreview } from "../hooks/use-comment-trigger-preview";
import { useStatusLabel } from "../utils/status-label";
import { taskStatusLabel } from "./task-run-labels";
import { BlockerLabel } from "./merge-readiness-panel";

/**
 * Review cockpit (K16): the reviewer's single screen for an issue's run —
 * the run, its pull requests and merge readiness, its cost, the questions
 * still open, the acceptance criteria with their proofs, the plan
 * verification — from one query. Approval and change requests are the
 * ordinary status moves, so every gate (F17, K12) still applies.
 */
export function ReviewCockpitRoute({ routeId }: { routeId: string }) {
  const wsId = useWorkspaceId();
  const { canonicalId, isResolving, notFound, loadFailed, retry } = useCanonicalIssue(wsId, routeId);
  if (isResolving) return <IssueDetailSkeleton />;
  if (loadFailed) return <LoadErrorState onRetry={retry} />;
  if (notFound || !canonicalId) return <IssueNotFound showBackLink />;
  return <ReviewCockpit issueId={canonicalId} />;
}

export function ReviewCockpit({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [runId, setRunId] = useState<string | undefined>(undefined);
  const { data, isLoading, isError, refetch } = useQuery(reviewCockpitOptions(wsId, issueId, runId));
  const update = useUpdateIssue();
  const createComment = useCreateComment(issueId);
  const qc = useQueryClient();
  const statusLabel = useStatusLabel(wsId);
  const formId = useId();
  // Request changes is a written request, not a bare status move: the reviewer
  // says what must change, it is posted on the issue (where the assignee picks
  // it up), then the issue goes back to In progress. A bare move gave no
  // feedback at all and, on an issue already In progress, changed nothing.
  const [changes, setChanges] = useState<string | null>(null);
  const [suppressedAgentIds, setSuppressedAgentIds] = useState<Set<string>>(() => new Set());
  const triggerPreview = useCommentTriggerPreview({ issueId, content: changes ?? "" });

  if (isLoading) return <IssueDetailSkeleton />;
  if (isError || !data) {
    return (
      <div className="flex flex-col items-start gap-2 p-6 text-body">
        <p>{t(($) => $.review_cockpit.error)}</p>
        <Button type="button" size="sm" variant="outline" onClick={() => void refetch()}>
          {t(($) => $.review_cockpit.retry)}
        </Button>
      </div>
    );
  }

  const refreshCockpit = () => qc.invalidateQueries({ queryKey: issueKeys.cockpit(wsId, issueId).slice(0, -1) });
  const move = (status: string, onSuccess?: () => void) =>
    update.mutate(
      { id: issueId, status },
      {
        onSuccess,
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.review_cockpit.move_failed)),
        onSettled: () => void refreshCockpit(),
      },
    );
  const checksPending = cockpitChecksPending(data);
  const busy = update.isPending || createComment.isPending;
  const sendChanges = async () => {
    const feedback = changes?.trim();
    if (!feedback) return;
    const suppressAgentIds = triggerPreview.agents.filter((a) => suppressedAgentIds.has(a.id)).map((a) => a.id);
    try {
      await createComment.mutateAsync({ content: feedback, suppressAgentIds: suppressAgentIds.length > 0 ? suppressAgentIds : undefined });
    } catch {
      toast.error(t(($) => $.review_cockpit.request_changes_failed));
      return;
    }
    setChanges(null);
    setSuppressedAgentIds(new Set());
    const done = () => toast.success(t(($) => $.review_cockpit.changes_requested));
    if (data.issue.status === "in_progress") done();
    else move("in_progress", done);
  };

  return (
    <div data-testid="review-cockpit" className="flex h-full flex-col gap-4 overflow-y-auto p-6 text-body">
      <div className="flex flex-wrap items-center gap-3">
        <AppLink href={paths.issueDetail(issueId)} className="inline-flex items-center gap-1 text-caption text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-3.5" />
          {t(($) => $.review_cockpit.back)}
        </AppLink>
        <span className="font-mono text-caption text-muted-foreground">{data.issue.identifier}</span>
        <h1 className="min-w-0 flex-1 truncate text-title font-semibold">{data.issue.title}</h1>
        <span className="rounded-md border px-2 py-0.5 text-caption">{statusLabel(data.issue.status)}</span>
      </div>

      {data.failed_sections.length > 0 && (
        <div data-testid="cockpit-failed" className="rounded-md border border-warning/50 px-3 py-2 text-caption text-warning">
          {t(($) => $.review_cockpit.failed_sections, { sections: data.failed_sections.join(", ") })}
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" size="sm" disabled={busy || checksPending} onClick={() => move("done")} title={checksPending ? t(($) => $.review_cockpit.approve_blocked) : undefined}>
          {t(($) => $.review_cockpit.approve)}
        </Button>
        <Button type="button" size="sm" variant={changes === null ? "outline" : "secondary"} aria-pressed={changes !== null}
          aria-controls={formId} disabled={busy} onClick={() => setChanges((draft) => draft ?? "")}>
          {t(($) => $.review_cockpit.request_changes)}
        </Button>
        {checksPending && <span className="text-caption text-muted-foreground">{t(($) => $.review_cockpit.approve_blocked)}</span>}
      </div>

      {changes !== null && (
        <form id={formId} data-testid="cockpit-request-changes" className="flex max-w-2xl flex-col gap-2 rounded-md border p-3"
          onSubmit={(event) => { event.preventDefault(); void sendChanges(); }}>
          <Label htmlFor={`${formId}-feedback`}>{t(($) => $.review_cockpit.changes_label)}</Label>
          <Textarea id={`${formId}-feedback`} autoFocus rows={3} maxLength={4000} disabled={busy} value={changes}
            onChange={(event) => setChanges(event.target.value)} />
          <p className="text-caption text-muted-foreground">{t(($) => $.review_cockpit.changes_hint)}</p>
          <CommentTriggerChips agents={triggerPreview.agents} blocked={triggerPreview.blocked} draftContent={changes}
            suppressedAgentIds={suppressedAgentIds}
            onToggle={(agentId) => setSuppressedAgentIds((prev) => {
              const next = new Set(prev);
              if (next.has(agentId)) next.delete(agentId); else next.add(agentId);
              return next;
            })} />
          <div className="flex flex-wrap justify-end gap-2">
            <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => setChanges(null)}>
              {t(($) => $.review_cockpit.changes_cancel)}
            </Button>
            <Button type="submit" size="sm" disabled={busy || !changes.trim()}>
              {t(($) => $.review_cockpit.changes_send)}
            </Button>
          </div>
        </form>
      )}

      <div className="grid gap-4 md:grid-cols-2">
        <Section title={t(($) => $.review_cockpit.run)} testId="cockpit-run">
          {data.runs.length > 1 && (
            <Select
              items={data.runs.map((r) => ({
                value: r.id,
                label: `${taskStatusLabel(t, r.status)} · ${r.created_at.slice(0, 16).replace("T", " ")}`,
              }))}
              value={runId ?? data.run?.id ?? ""}
              onValueChange={(value) => value !== null && setRunId(value || undefined)}
            >
              <SelectTrigger size="sm" className="mb-2" aria-label={t(($) => $.review_cockpit.select_run)}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {data.runs.map((r) => (
                  <SelectItem key={r.id} value={r.id}>
                    {taskStatusLabel(t, r.status)} · {r.created_at.slice(0, 16).replace("T", " ")}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          {data.run ? <RunSummary run={data.run} /> : <Muted>{t(($) => $.review_cockpit.runs_none)}</Muted>}
        </Section>

        <Section title={t(($) => $.review_cockpit.cost)} testId="cockpit-cost">
          {data.usage ? <UsageSummary usage={data.usage} /> : <Muted>{t(($) => $.review_cockpit.cost_none)}</Muted>}
        </Section>

        <Section title={t(($) => $.review_cockpit.pull_requests)} testId="cockpit-prs">
          <PullRequests cockpit={data} />
        </Section>

        <Section title={t(($) => $.review_cockpit.open_questions)} testId="cockpit-questions">
          {data.open_questions.length === 0 ? (
            <Muted>{t(($) => $.review_cockpit.none_open)}</Muted>
          ) : (
            <ul className="flex flex-col gap-1">
              {data.open_questions.map((q) => (
                <li key={q.id} className="flex items-start gap-2">
                  <span className="shrink-0 uppercase text-warning">{q.urgency}</span>
                  <span className="min-w-0 flex-1">{q.question}</span>
                </li>
              ))}
            </ul>
          )}
        </Section>

        <Section title={t(($) => $.review_cockpit.criteria)} testId="cockpit-criteria">
          {data.criteria.length === 0 ? (
            <Muted>{t(($) => $.review_cockpit.criteria_none)}</Muted>
          ) : (
            <ul className="flex flex-col gap-1">
              {data.criteria.map((c) => (
                <li key={c.id} className="flex items-start gap-2" data-state={c.proof_state}>
                  {c.proof_state === "satisfied" ? (
                    <Check className="mt-0.5 size-3.5 shrink-0 text-success" />
                  ) : c.proof_state === "pending_human" ? (
                    <Clock className="mt-0.5 size-3.5 shrink-0 text-warning" />
                  ) : (
                    <Circle className="mt-0.5 size-3.5 shrink-0 text-faint-foreground" />
                  )}
                  <span className="min-w-0 flex-1">
                    {c.text}
                    {c.proof_ref && <span className="text-muted-foreground"> · {c.proof_ref}</span>}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Section>

        <Section title={t(($) => $.review_cockpit.plan_verification)} testId="cockpit-plan">
          {data.plan_verification ? (
            <div className="flex flex-col gap-1">
              <span className={cn(data.plan_verification.critical_count > 0 ? "text-destructive" : "text-success")}>
                {t(($) => $.review_cockpit.plan_counts, {
                  critical: data.plan_verification.critical_count,
                  major: data.plan_verification.major_count,
                  minor: data.plan_verification.minor_count,
                })}
              </span>
              {data.plan_verification.summary && <span className="whitespace-pre-wrap text-muted-foreground">{data.plan_verification.summary}</span>}
            </div>
          ) : (
            <Muted>{t(($) => $.review_cockpit.plan_none)}</Muted>
          )}
        </Section>

        <Section title={t(($) => $.review_cockpit.self_review)} testId="cockpit-self-review">
          <Muted>{t(($) => $.review_cockpit.self_review_none)}</Muted>
        </Section>
      </div>
    </div>
  );
}

function Section({ title, testId, children }: { title: string; testId: string; children: React.ReactNode }) {
  return (
    <section data-testid={testId} className="flex flex-col gap-2 rounded-md border p-3">
      <h2 className="text-caption font-medium text-muted-foreground">{title}</h2>
      {children}
    </section>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-caption text-muted-foreground">{children}</span>;
}

function RunSummary({ run }: { run: NonNullable<ReviewCockpit["run"]> }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  return (
    <div className="flex flex-col gap-1 text-caption">
      <span className="font-medium">{taskStatusLabel(t, run.status)}</span>
      <span className="text-muted-foreground">
        {timeAgo(run.created_at)}
        {run.completed_at && <span> · {t(($) => $.review_cockpit.run_completed, { ago: timeAgo(run.completed_at) })}</span>}
      </span>
      {run.error && <span className="text-destructive">{run.error}</span>}
      {run.handoff_note && <span className="line-clamp-4 whitespace-pre-wrap text-muted-foreground">{run.handoff_note}</span>}
    </div>
  );
}

function UsageSummary({ usage }: { usage: NonNullable<ReviewCockpit["usage"]> }) {
  const { t } = useT("issues");
  const usd = usdFromTicks(usage.cost_usd_ticks);
  return (
    <div className="flex flex-col gap-1 text-caption">
      <span className="text-title font-semibold">
        {usd === null ? t(($) => $.review_cockpit.cost_unpriced) : formatUsd(usd)}
        {usd !== null && usage.uncosted && <span className="text-caption font-normal text-muted-foreground"> {t(($) => $.review_cockpit.cost_floor)}</span>}
      </span>
      <span className="text-muted-foreground">
        {t(($) => $.review_cockpit.tokens, { input: formatTokens(usage.input_tokens), output: formatTokens(usage.output_tokens) })}
      </span>
    </div>
  );
}

function PullRequests({ cockpit }: { cockpit: ReviewCockpit }) {
  const { t } = useT("issues");
  const mr = cockpit.merge_readiness;
  if (!mr) return <Muted>{t(($) => $.review_cockpit.prs_unavailable)}</Muted>;
  return (
    <div className="flex flex-col gap-2 text-caption">
      {mr.prs.length === 0 ? (
        <Muted>{t(($) => $.review_cockpit.no_pr)}</Muted>
      ) : (
        <ul className="flex flex-col gap-1">
          {mr.prs.map((pr) => (
            <li key={pr.id} className="flex items-center gap-2">
              <a href={pr.html_url} target="_blank" rel="noreferrer" className="inline-flex min-w-0 items-center gap-1 hover:underline">
                <span className="truncate">#{pr.number} {pr.title}</span>
                <ExternalLink className="size-3 shrink-0" />
              </a>
              <span className="shrink-0 text-muted-foreground">{pr.state}</span>
              <span className={cn("shrink-0 tabular-nums", pr.checks.failed > 0 ? "text-destructive" : pr.checks.pending > 0 ? "text-warning" : "text-success")}>
                {t(($) => $.review_cockpit.checks, { passed: pr.checks.passed, total: pr.checks.total })}
              </span>
            </li>
          ))}
        </ul>
      )}
      <div className={cn("font-medium", mr.ready ? "text-success" : "text-warning")}>
        {mr.ready ? t(($) => $.review_cockpit.ready) : t(($) => $.review_cockpit.blockers, { count: mr.blockers.length })}
      </div>
      {mr.blockers.length > 0 && (
        <ul className="flex flex-col gap-0.5 text-muted-foreground">
          {mr.blockers.map((b, i) => (
            <li key={`${b.kind}-${i}`}>
              <BlockerLabel blocker={b} />
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
