"use client";

import { useId, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { inboxDecisionsOptions, useAnswerIssueDecision, useResumeIssueDecision, type IssueDecision } from "@multica/core/inbox/decisions";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import { PageHeader } from "../../layout/page-header";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";

export function InboxDecisions() {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { replace } = useNavigation();
  const [history, setHistory] = useState(false);
  const query = useInfiniteQuery(inboxDecisionsOptions(wsId, history));
  const rows = query.data?.pages.flatMap((page) => page.decisions) ?? [];
  return <div className="flex h-full min-h-0 flex-col">
    <PageHeader>
      <h1 className="flex-1 text-body font-semibold">{t(($) => $.decisions.title)}</h1>
      <Button variant="ghost" size="sm" onClick={() => replace(paths.inbox())}>{t(($) => $.decisions.notifications)}</Button>
    </PageHeader>
    <div className="min-h-0 flex-1 overflow-y-auto p-4">
      <div className="mx-auto max-w-3xl space-y-4">
        <p className="text-body text-muted-foreground">{t(($) => $.decisions.description)}</p>
        <div className="flex gap-2" role="group" aria-label={t(($) => $.decisions.title)}>
          <Button size="sm" variant={history ? "ghost" : "secondary"} aria-pressed={!history} onClick={() => setHistory(false)}>{t(($) => $.decisions.pending)}</Button>
          <Button size="sm" variant={history ? "secondary" : "ghost"} aria-pressed={history} onClick={() => setHistory(true)}>{t(($) => $.decisions.history)}</Button>
        </div>
        {query.isPending && <p role="status" className="text-body">{t(($) => $.decisions.loading)}</p>}
        {query.isError && <div role="alert" className="space-y-2 text-body">
          <p>{t(($) => $.decisions.load_failed)}</p>
          <Button variant="outline" size="sm" onClick={() => void query.refetch()}>{t(($) => $.decisions.retry)}</Button>
        </div>}
        {!query.isPending && !query.isError && rows.length === 0 && <p className="rounded-lg border p-6 text-body text-muted-foreground">{history ? t(($) => $.decisions.empty_history) : t(($) => $.decisions.empty)}</p>}
        {rows.map((row) => <DecisionCard key={row.id} wsId={wsId} decision={row} />)}
        {query.hasNextPage && <Button variant="outline" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>{t(($) => $.decisions.load_more)}</Button>}
      </div>
    </div>
  </div>;
}

export function DecisionCard({ wsId, decision }: { wsId: string; decision: IssueDecision }) {
  const { t } = useT("inbox");
  const paths = useWorkspacePaths();
  const inputId = useId();
  const answerMutation = useAnswerIssueDecision(wsId, decision.issueId, decision.id);
  const resumeMutation = useResumeIssueDecision(wsId, decision.issueId, decision.id);
  const [draft, setDraft] = useState("");
  const [cancelConfirm, setCancelConfirm] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const row = resumeMutation.data ?? (decision.status === "open" ? answerMutation.data : undefined) ?? decision;
  const busy = answerMutation.isPending || resumeMutation.isPending;
  const answer = draft.trim();
  const respond = (status: "answered" | "cancelled") => {
    setError(null);
    answerMutation.mutate({ status, answer: status === "answered" ? answer : "" }, {
      onSuccess: () => { setDraft(""); setCancelConfirm(false); },
      onError: (err) => setError(err instanceof ApiError && err.status === 409 ? t(($) => $.decisions.answer_conflict) : t(($) => $.decisions.answer_failed)),
    });
  };
  return <article className="min-w-0 space-y-3 rounded-lg border p-4" aria-labelledby={`${inputId}-question`}>
    <div className="flex flex-wrap items-center justify-between gap-2 text-caption text-muted-foreground">
      <AppLink href={paths.issueDetail(row.issueId)} className="underline underline-offset-4">{t(($) => $.decisions.open_issue)}</AppLink>
      <time dateTime={row.createdAt}>{new Date(row.createdAt).toLocaleString()}</time>
    </div>
    <h2 id={`${inputId}-question`} className="whitespace-pre-wrap break-words text-body font-medium">{row.question}</h2>
    {row.context && <p className="whitespace-pre-wrap break-words text-body text-muted-foreground">{row.context}</p>}
    {row.status === "open" && <>
      {row.options.length > 0 && <div className="flex flex-wrap gap-2" role="group" aria-label={t(($) => $.decisions.suggestions)}>
        {row.options.map((option) => <Button key={option} variant="outline" size="sm" className="h-auto max-w-full whitespace-normal break-words py-2 text-left" disabled={busy} onClick={() => setDraft(option)}>{option}</Button>)}
      </div>}
      <div className="space-y-2">
        <Label htmlFor={inputId}>{t(($) => $.decisions.answer_label)}</Label>
        <Textarea id={inputId} value={draft} disabled={busy} onChange={(event) => setDraft(event.target.value)} placeholder={t(($) => $.decisions.answer_placeholder)} />
        <p className="text-caption text-muted-foreground">{t(($) => $.decisions.answer_hint)}</p>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={busy || !answer || [...answer].length > 4000} onClick={() => respond("answered")}>{t(($) => $.decisions.save_answer)}</Button>
        {!cancelConfirm ? <Button size="sm" variant="ghost" disabled={busy} onClick={() => setCancelConfirm(true)}>{t(($) => $.decisions.cancel)}</Button> : <>
          <Button size="sm" variant="destructive" disabled={busy} onClick={() => respond("cancelled")}>{t(($) => $.decisions.confirm_cancel)}</Button>
          <Button size="sm" variant="ghost" disabled={busy} onClick={() => setCancelConfirm(false)}>{t(($) => $.decisions.keep)}</Button>
        </>}
      </div>
    </>}
    {row.status === "answered" && <div className="space-y-3">
      <p className="text-caption font-medium">{t(($) => $.decisions.answer_saved)}</p>
      <p className="whitespace-pre-wrap break-words text-body">{row.answer}</p>
      {row.resumeTaskId ? <p className="break-all text-caption text-muted-foreground">{t(($) => $.decisions.receipt, { id: row.resumeTaskId })}</p> : <>
        <p className="text-caption text-muted-foreground">{t(($) => $.decisions.resume_hint)}</p>
        <Button size="sm" disabled={busy} onClick={() => {
          setError(null);
          resumeMutation.mutate(undefined, { onError: (err) => setError(err instanceof ApiError && err.status === 409
            ? t(($) => $.decisions.resume_unavailable) : err instanceof ApiError && err.status === 403
              ? t(($) => $.decisions.resume_forbidden) : t(($) => $.decisions.resume_failed)) });
        }}>{t(($) => $.decisions.resume)}</Button>
      </>}
    </div>}
    {row.status === "cancelled" && <p className="text-body text-muted-foreground">{t(($) => $.decisions.cancelled)}</p>}
    {error && <p role="alert" className="text-body text-destructive">{error}</p>}
    <details className="text-caption text-muted-foreground">
      <summary className="cursor-pointer">{t(($) => $.decisions.trace)}</summary>
      <dl className="mt-2 space-y-1 break-all">
        <div><dt className="inline font-medium">{t(($) => $.decisions.decision_id)}: </dt><dd className="inline">{row.id}</dd></div>
        <div><dt className="inline font-medium">{t(($) => $.decisions.source_run)}: </dt><dd className="inline">{row.sourceTaskId}</dd></div>
        <div><dt className="inline font-medium">{t(($) => $.decisions.requester)}: </dt><dd className="inline">{row.requestedBy}</dd></div>
        {row.answeredBy && <div><dt className="inline font-medium">{t(($) => $.decisions.respondent)}: </dt><dd className="inline">{row.answeredBy} · {row.answeredAt && new Date(row.answeredAt).toLocaleString()}</dd></div>}
      </dl>
    </details>
  </article>;
}
