"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  goalBlockerLabelKey,
  issueGoalOptions,
  useAnswerIssueGoal,
  usePauseIssueGoal,
  useResumeIssueGoal,
  useSetIssueGoal,
} from "@multica/core/issues/goal-loop";
import type { Issue, IssueGoalStatus } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";
import { WorkflowSummaryLine } from "./execution-log-section";

const STATUS_TONE: Record<IssueGoalStatus, string> = {
  active: "bg-info/15 text-info",
  paused: "bg-muted text-muted-foreground",
  waiting_user: "bg-warning/15 text-warning",
  satisfied: "bg-success/15 text-success",
  stopped: "bg-destructive/10 text-destructive",
};

/**
 * Goal loop: the agent works this issue toward a stated goal across bounded
 * continuations, stopping when it is satisfied, stuck, or needs the human.
 * Renders nothing until a goal is set — except a compact affordance to set
 * one, shown only while an agent is assigned (there is nothing to run the
 * loop with otherwise).
 */
export function GoalSection({ issueId, issue }: { issueId: string; issue: Pick<Issue, "assignee_type" | "assignee_id"> }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: goal, isPending } = useQuery(issueGoalOptions(wsId, issueId));
  const setGoal = useSetIssueGoal(wsId, issueId);
  const pause = usePauseIssueGoal(wsId, issueId);
  const resume = useResumeIssueGoal(wsId, issueId);
  const answer = useAnswerIssueGoal(wsId, issueId);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [maxContinuations, setMaxContinuations] = useState(8);
  const [showAllEvidence, setShowAllEvidence] = useState(false);
  const [answerText, setAnswerText] = useState("");

  const isAgentAssigned = issue.assignee_type === "agent";
  if (isPending) return null;
  if (!goal && !isAgentAssigned) return null;

  const fail = (e: unknown, fallback: string) => toast.error(e instanceof Error && e.message ? e.message : fallback);

  const startEdit = () => {
    setDraft(goal?.goal ?? "");
    setMaxContinuations(goal?.max_continuations ?? 8);
    setEditing(true);
  };

  const submit = () =>
    setGoal.mutate(
      { goal: draft, max_continuations: maxContinuations },
      {
        onSuccess: () => { setEditing(false); toast.success(t(($) => $.goal_loop.saved)); },
        onError: (e) => fail(e, t(($) => $.goal_loop.save_failed)),
      },
    );

  const goalForm = (
    <div className="flex flex-col gap-2">
      <Textarea
        rows={2}
        aria-label={t(($) => $.goal_loop.goal_label)}
        placeholder={t(($) => $.goal_loop.goal_placeholder)}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
      />
      <label className="flex items-center gap-2 text-muted-foreground">
        <span>{t(($) => $.goal_loop.max_continuations)}</span>
        <input
          type="number"
          min={1}
          max={20}
          aria-label={t(($) => $.goal_loop.max_continuations)}
          className="w-16 rounded border bg-background p-1"
          value={maxContinuations}
          onChange={(e) => setMaxContinuations(Number(e.target.value))}
        />
      </label>
      <div className="flex gap-2">
        <Button type="button" size="sm" disabled={!draft.trim() || setGoal.isPending} onClick={submit}>
          {t(($) => $.goal_loop.save)}
        </Button>
        {goal && (
          <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>
            {t(($) => $.goal_loop.cancel)}
          </Button>
        )}
      </div>
    </div>
  );

  if (!goal) {
    // Compact "set a goal" affordance — the issue has an agent assignee but
    // no goal yet.
    return (
      <div data-testid="goal-section" className="flex flex-col gap-1.5 rounded-md border p-2 text-caption">
        <div className="font-medium">{t(($) => $.goal_loop.section)}</div>
        <p className="text-muted-foreground">{t(($) => $.goal_loop.intro)}</p>
        {goalForm}
      </div>
    );
  }

  const evidenceShown = showAllEvidence ? goal.evidence : goal.evidence.slice(0, 3);
  const blockerKey = goal.last_blocker ? goalBlockerLabelKey(goal.last_blocker) : "other";
  const canPause = goal.status === "active" || goal.status === "waiting_user";
  const canResume = goal.status === "paused" || goal.status === "stopped" || goal.status === "satisfied";
  const question = goal.question;
  const answeredWhen = question?.answer ? timeAgo(question.answered_at || goal.updated_at) : "";
  const answeredLine = question?.answer
    ? question.answered_by
      ? t(($) => $.goal_loop.question.answered_by_line, { by: question.answered_by_name || question.answered_by, when: answeredWhen })
      : t(($) => $.goal_loop.question.answered_line, { when: answeredWhen })
    : "";

  return (
    <div data-testid="goal-section" data-status={goal.status} className="flex flex-col gap-2 rounded-md border p-2 text-caption">
      <div className="flex flex-wrap items-center gap-2">
        <span className="font-medium">{t(($) => $.goal_loop.section)}</span>
        <span className={cn("rounded px-1.5 font-medium", STATUS_TONE[goal.status])}>
          {t(($) => $.goal_loop.status[goal.status])}
        </span>
        <div className="ml-auto flex items-center gap-2">
          {canPause && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={pause.isPending}
              onClick={() =>
                pause.mutate(undefined, {
                  onSuccess: () => toast.success(t(($) => $.goal_loop.paused)),
                  onError: (e) => fail(e, t(($) => $.goal_loop.pause_failed)),
                })
              }
            >
              {t(($) => $.goal_loop.pause)}
            </Button>
          )}
          {canResume && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={resume.isPending}
              onClick={() =>
                resume.mutate(undefined, {
                  onSuccess: () => toast.success(t(($) => $.goal_loop.resumed)),
                  onError: (e) => fail(e, t(($) => $.goal_loop.resume_failed)),
                })
              }
            >
              {t(($) => $.goal_loop.resume)}
            </Button>
          )}
          <Button type="button" size="sm" variant="ghost" onClick={() => (editing ? setEditing(false) : startEdit())}>
            {editing ? t(($) => $.goal_loop.cancel) : t(($) => $.goal_loop.edit)}
          </Button>
        </div>
      </div>

      {editing ? goalForm : <p className="whitespace-pre-wrap">{goal.goal}</p>}

      <div className="flex items-center gap-2 tabular-nums text-muted-foreground">
        <span>{t(($) => $.goal_loop.continuation_progress, { n: goal.continuation, max: goal.max_continuations })}</span>
        <span className="flex flex-wrap gap-0.5" aria-hidden="true">
          {Array.from({ length: goal.max_continuations }).map((_, i) => (
            <span key={i} className={cn("h-1.5 w-1.5 shrink-0 rounded-full", i < goal.continuation ? "bg-foreground" : "bg-muted")} />
          ))}
        </span>
      </div>

      {goal.last_blocker && (
        <div>
          <span className="font-medium">{t(($) => $.goal_loop.blocker)}: </span>
          {blockerKey === "other" ? goal.last_blocker : t(($) => $.goal_loop.blockers[blockerKey as "other"])}
        </div>
      )}
      {goal.last_reason && (
        <div><span className="font-medium">{t(($) => $.goal_loop.reason)}: </span>{goal.last_reason}</div>
      )}
      {goal.next_step && (
        <div><span className="font-medium">{t(($) => $.goal_loop.next_step)}: </span>{goal.next_step}</div>
      )}

      {goal.evidence.length > 0 && (
        <div className="flex flex-col gap-1">
          <span className="font-medium">{t(($) => $.goal_loop.evidence)}</span>
          <ul className="list-disc pl-4">
            {evidenceShown.map((e, i) => <li key={i}>{e}</li>)}
          </ul>
          {goal.evidence.length > 3 && (
            <button
              type="button"
              className="self-start text-muted-foreground underline hover:text-foreground"
              onClick={() => setShowAllEvidence((v) => !v)}
            >
              {showAllEvidence ? t(($) => $.goal_loop.evidence_collapse) : t(($) => $.goal_loop.evidence_show_all, { count: goal.evidence.length })}
            </button>
          )}
        </div>
      )}

      {goal.chain_root_task_id && <WorkflowSummaryLine rootTaskId={goal.chain_root_task_id} />}

      {question && (goal.status === "waiting_user" || question.answer) && (
        <div data-testid="goal-question" className="flex flex-col gap-1.5 rounded border p-2">
          <p>{question.prompt}</p>
          {question.answer ? (
            <p className="text-muted-foreground">
              {question.answer} — {answeredLine}
            </p>
          ) : question.kind === "choice" ? (
            <div className="flex flex-wrap gap-1">
              {(question.options ?? []).map((opt) => (
                <Button
                  key={opt}
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={answer.isPending}
                  onClick={() =>
                    answer.mutate(opt, {
                      onSuccess: () => toast.success(t(($) => $.goal_loop.question.answered)),
                      onError: (e) => fail(e, t(($) => $.goal_loop.question.answer_failed)),
                    })
                  }
                >
                  {opt}
                </Button>
              ))}
            </div>
          ) : (
            <div className="flex gap-1.5">
              <input
                type="text"
                aria-label={t(($) => $.goal_loop.question.answer)}
                placeholder={t(($) => $.goal_loop.question.answer_placeholder)}
                className="flex-1 rounded border bg-background p-1"
                value={answerText}
                onChange={(e) => setAnswerText(e.target.value)}
              />
              <Button
                type="button"
                size="sm"
                disabled={!answerText.trim() || answer.isPending}
                onClick={() =>
                  answer.mutate(answerText, {
                    onSuccess: () => { setAnswerText(""); toast.success(t(($) => $.goal_loop.question.answered)); },
                    onError: (e) => fail(e, t(($) => $.goal_loop.question.answer_failed)),
                  })
                }
              >
                {t(($) => $.goal_loop.question.answer)}
              </Button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
