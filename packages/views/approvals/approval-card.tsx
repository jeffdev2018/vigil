"use client";

import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ChevronDown, ChevronRight, Clock, ShieldCheck } from "lucide-react";
import {
  approvalKeys,
  approvalSecondsLeft,
  formatCountdown,
  gateDetails,
  type ApprovalItem,
} from "@multica/core/approvals";
import { useRespondIssueDecision } from "@multica/core/issues/decisions";
import { useAnswerIssueGoal } from "@multica/core/issues/goal-loop";
import { useDecideIssueTransitionRequest } from "@multica/core/issue-transitions";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../navigation";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { useT } from "../i18n";

/**
 * ApprovalCard (OS plan, chantier 3): one pending ask, decidable in place.
 * The same card renders in the issue timeline, the inbox, the chat panel
 * and the decisions view; `showIssue` adds the issue link for surfaces
 * that are not the issue itself. Which controls appear follows the source:
 * a Decision Card gets its options and a free-text answer, a held
 * transition gets Approve / Reject with a note, a goal question gets its
 * choices or a text answer. A gate shows what it would do (arguments,
 * paths, blast radius, dual-approval progress) and counts down to its
 * expiry. Nothing here is optimistic: the server decides, the feed
 * refreshes on approval:decided.
 */
export function ApprovalCard({ approval, wsId, showIssue = false, compact = false }: { approval: ApprovalItem; wsId: string; showIssue?: boolean; compact?: boolean }) {
  const { t } = useT("issues");
  const qc = useQueryClient();
  const slug = useWorkspaceSlug();
  const respond = useRespondIssueDecision(wsId);
  const decideTransition = useDecideIssueTransitionRequest(wsId, approval.issue.id);
  const answerGoal = useAnswerIssueGoal(wsId, approval.issue.id);
  const [text, setText] = useState("");
  const [editing, setEditing] = useState(false);
  const [showDetails, setShowDetails] = useState(false);
  const [secondsLeft, setSecondsLeft] = useState(() => approvalSecondsLeft(approval));

  useEffect(() => {
    setSecondsLeft(approvalSecondsLeft(approval));
    if (approvalSecondsLeft(approval) === null) return;
    const id = setInterval(() => setSecondsLeft(approvalSecondsLeft(approval)), 30_000);
    return () => clearInterval(id);
  }, [approval.expires_at, approval.sla_deadline_at]); // eslint-disable-line react-hooks/exhaustive-deps

  const busy = respond.isPending || decideTransition.isPending || answerGoal.isPending;
  const refresh = () => {
    qc.invalidateQueries({ queryKey: approvalKeys.all(wsId) });
  };
  const fail = (e: unknown) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.approvals.answer_failed));
  const done = () => {
    setText("");
    setEditing(false);
    refresh();
    toast.success(t(($) => $.approvals.answered));
  };

  const answerDecision = (body: { option_id?: string; modified_text?: string }) =>
    respond.mutate({ issueId: approval.issue.id, decisionId: approval.id, answer: body }, { onSuccess: done, onError: fail, onSettled: refresh });
  const answerTransition = (decision: "approve" | "reject") =>
    decideTransition.mutate({ requestId: approval.id, decision, note: text.trim() || undefined }, { onSuccess: done, onError: fail, onSettled: refresh });
  const answerQuestion = (answer: string) => answerGoal.mutate(answer, { onSuccess: done, onError: fail, onSettled: refresh });

  const gate = gateDetails(approval.gate);
  const countdown = formatCountdown(secondsLeft);
  const expired = secondsLeft === 0;
  const kindLabel = approvalKindLabel(approval, t);
  const issueHref = slug && approval.issue.id ? paths.workspace(slug).issueDetail(approval.issue.id) : null;

  return (
    <div
      data-testid="approval-card"
      data-kind={approval.kind}
      data-source={approval.source}
      className={cn("flex flex-col gap-1.5 rounded-md border p-2 text-caption", expired ? "border-border opacity-80" : "border-warning/60 bg-warning/5", compact && "p-1.5")}
    >
      <div className="flex items-start gap-2">
        <ShieldCheck className="mt-0.5 size-3.5 shrink-0 text-warning" aria-hidden="true" />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
            <span className="font-medium">{kindLabel}</span>
            {approval.asked_by?.name ? <span className="text-muted-foreground">{t(($) => $.approvals.asked_by, { name: approval.asked_by.name })}</span> : null}
            {showIssue && approval.issue.identifier ? (
              issueHref ? (
                <AppLink href={issueHref} className="text-muted-foreground hover:text-foreground">
                  {approval.issue.identifier} · {approval.issue.title}
                </AppLink>
              ) : (
                <span className="text-muted-foreground">{approval.issue.identifier} · {approval.issue.title}</span>
              )
            ) : null}
            {approval.urgency === "high" ? <span className="uppercase text-destructive">{approval.urgency}</span> : null}
            {countdown ? (
              <span data-testid="approval-countdown" className={cn("ml-auto inline-flex items-center gap-1 font-mono tabular-nums", expired ? "text-destructive" : "text-muted-foreground")}>
                <Clock className="size-3" aria-hidden="true" />
                {expired ? t(($) => $.approvals.expired) : t(($) => $.approvals.expires_in, { time: countdown })}
              </span>
            ) : null}
          </div>
          <p className="mt-0.5 whitespace-pre-wrap">{approval.question}</p>
          {approval.transition ? (
            <p className="text-muted-foreground">
              {approval.transition.from_status} → {approval.transition.to_status}
            </p>
          ) : null}
        </div>
      </div>

      {approval.gate ? (
        <div className="pl-5">
          <button type="button" className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground" onClick={() => setShowDetails((v) => !v)} aria-expanded={showDetails}>
            {showDetails ? <ChevronDown className="size-3" aria-hidden="true" /> : <ChevronRight className="size-3" aria-hidden="true" />}
            {showDetails ? t(($) => $.approvals.details_hide) : t(($) => $.approvals.details_show)}
            {gate.requiredApprovals > 1 ? <span className="ml-1 font-mono tabular-nums">· {t(($) => $.approvals.dual_approval, { have: gate.approvals, need: gate.requiredApprovals })}</span> : null}
          </button>
          {showDetails ? (
            <div data-testid="approval-gate-details" className="mt-1 flex flex-col gap-1 rounded border bg-muted/40 p-2">
              <div>
                <span className="text-muted-foreground">{t(($) => $.approvals.gate_type)}: </span>
                <span className="font-mono">{approval.gate.gate_type}</span>
                {approval.gate.summary ? <span> · {approval.gate.summary}</span> : null}
              </div>
              {gate.paths.length > 0 ? (
                <div>
                  <span className="text-muted-foreground">{t(($) => $.approvals.paths)}: </span>
                  <span className="font-mono break-all">{gate.paths.join(", ")}</span>
                </div>
              ) : null}
              {gate.blastRadius ? (
                <div>
                  <span className="text-muted-foreground">{t(($) => $.approvals.blast_radius)}: </span>
                  <span>{gate.blastRadius}</span>
                </div>
              ) : null}
              {gate.params != null ? (
                <pre className="max-h-48 overflow-auto rounded bg-background p-1.5 font-mono text-caption">{JSON.stringify(gate.params, null, 2)}</pre>
              ) : null}
            </div>
          ) : null}
        </div>
      ) : null}

      {!approval.can_decide ? (
        <p data-testid="approval-cannot-decide" className="pl-5 text-muted-foreground">
          {approval.cannot_decide_reason === "gate_approvers_policy" ? t(($) => $.approvals.cannot_decide_policy) : t(($) => $.approvals.cannot_decide_role)}
        </p>
      ) : expired ? null : approval.source === "transition" ? (
        <div className="flex flex-col gap-1.5 pl-5 sm:flex-row sm:items-center">
          <Input value={text} onChange={(e) => setText(e.target.value)} placeholder={t(($) => $.approvals.note_placeholder)} className="h-7 flex-1 text-caption" />
          <div className="flex gap-1.5">
            <Button size="sm" disabled={busy} onClick={() => answerTransition("approve")}>{t(($) => $.approvals.approve)}</Button>
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => answerTransition("reject")}>{t(($) => $.approvals.reject)}</Button>
          </div>
        </div>
      ) : approval.source === "goal_question" ? (
        <div className="pl-5">
          {approval.goal_question?.kind === "choice" ? (
            <div className="flex flex-wrap gap-1">
              {(approval.goal_question.options ?? []).map((opt) => (
                <Button key={opt} type="button" size="sm" variant="outline" disabled={busy} onClick={() => answerQuestion(opt)}>{opt}</Button>
              ))}
            </div>
          ) : (
            <div className="flex gap-1.5">
              <Input value={text} onChange={(e) => setText(e.target.value)} placeholder={t(($) => $.approvals.answer_placeholder)} className="h-7 flex-1 text-caption" aria-label={t(($) => $.approvals.answer_placeholder)} />
              <Button size="sm" disabled={busy || !text.trim()} onClick={() => answerQuestion(text.trim())}>{t(($) => $.approvals.send)}</Button>
            </div>
          )}
        </div>
      ) : (
        <div className="flex flex-col gap-1 pl-5">
          <div className={cn("flex gap-1", compact ? "flex-row flex-wrap" : "flex-col")}>
            {approval.options.map((o) => {
              const recommended = o.id === approval.recommended_option_id || (approval.decision?.learned?.option_id === o.id && !approval.recommended_option_id);
              const destructive = o.id === "deny" || o.id === "reject" || o.id === "discard";
              return (
                <Button
                  key={o.id}
                  type="button"
                  size="sm"
                  variant={recommended ? "default" : destructive ? "ghost" : "outline"}
                  disabled={busy}
                  onClick={() => answerDecision({ option_id: o.id })}
                  className={cn("h-auto whitespace-normal py-1 text-left", compact ? "justify-center" : "justify-start")}
                  title={o.impact || undefined}
                >
                  <span className="flex flex-col items-start">
                    <span>
                      {o.label}
                      {recommended ? <span className="ml-1 opacity-70">· {t(($) => $.decisions.recommended)}</span> : null}
                    </span>
                    {o.impact && !compact ? <span className="text-caption font-normal opacity-70">{o.impact}</span> : null}
                  </span>
                </Button>
              );
            })}
          </div>
          {approval.decision?.learned ? (
            <p className="text-muted-foreground">{t(($) => $.decisions.learned, { label: approval.decision.learned.option_label, count: approval.decision.learned.count, total: approval.decision.learned.total })}</p>
          ) : null}
          {editing ? (
            <div className="flex flex-col gap-1">
              <Textarea aria-label={t(($) => $.decisions.modify_placeholder)} placeholder={t(($) => $.decisions.modify_placeholder)} value={text} onChange={(e) => setText(e.target.value)} rows={2} />
              <div className="flex gap-1">
                <Button type="button" size="sm" disabled={busy || text.trim() === ""} onClick={() => answerDecision({ modified_text: text.trim() })}>{t(($) => $.decisions.send)}</Button>
                <Button type="button" size="sm" variant="ghost" onClick={() => setEditing(false)}>{t(($) => $.decisions.cancel)}</Button>
              </div>
            </div>
          ) : (
            <button type="button" className="self-start text-muted-foreground hover:text-foreground" onClick={() => setEditing(true)}>{t(($) => $.decisions.modify)}</button>
          )}
        </div>
      )}
    </div>
  );
}

function approvalKindLabel(approval: ApprovalItem, t: ReturnType<typeof useT<"issues">>["t"]): string {
  switch (approval.kind) {
    case "gate": {
      const type = approval.gate?.gate_type ?? "";
      if (type === "git_push") return t(($) => $.approvals.kind_gate_git_push);
      if (type === "spend") return t(($) => $.approvals.kind_gate_spend);
      return t(($) => $.approvals.kind_gate_tool);
    }
    case "plan": return t(($) => $.approvals.kind_plan);
    case "interview": return t(($) => $.approvals.kind_interview);
    case "preview": return t(($) => $.approvals.kind_preview);
    case "watchdog": return t(($) => $.approvals.kind_watchdog);
    case "pipeline": return t(($) => $.approvals.kind_pipeline);
    case "goal_attach": return t(($) => $.approvals.kind_goal_attach);
    case "org_assign": return t(($) => $.approvals.kind_org_assign);
    case "transition": return t(($) => $.approvals.kind_transition);
    case "goal_question": return t(($) => $.approvals.kind_goal_question);
    default: return t(($) => $.approvals.kind_decision);
  }
}

/**
 * PendingApprovalsBar sits above a timeline and names how many asks wait;
 * clicking one scrolls to its card. Renders nothing when the list is empty.
 */
export function PendingApprovalsBar({ approvals, onSelect }: { approvals: ApprovalItem[]; onSelect: (id: string) => void }) {
  const { t } = useT("issues");
  if (approvals.length === 0) return null;
  return (
    <div data-testid="pending-approvals-bar" className="flex flex-wrap items-center gap-2 rounded-md border border-warning/60 bg-warning/10 px-2 py-1.5 text-caption">
      <ShieldCheck className="size-3.5 text-warning" aria-hidden="true" />
      <span className="font-medium">{t(($) => $.approvals.pending_banner, { count: approvals.length })}</span>
      {approvals.map((a) => (
        <button key={a.id} type="button" className="rounded border px-1.5 py-0.5 hover:bg-background" onClick={() => onSelect(a.id)}>
          {approvalShortLabel(a)}
        </button>
      ))}
    </div>
  );
}

function approvalShortLabel(a: ApprovalItem): string {
  if (a.source === "transition" && a.transition) return `${a.transition.from_status} → ${a.transition.to_status}`;
  const q = a.question.replace(/^Blocked action · /, "");
  return q.length > 48 ? q.slice(0, 47) + "…" : q;
}
