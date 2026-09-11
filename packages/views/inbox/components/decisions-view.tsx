"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { inboxDecisionsOptions, type InboxDecision } from "@multica/core/inbox/queries";
import type { ApprovalGoalQuestion, ApprovalItem, ApprovalTransition } from "@multica/core/approvals";
import { ApprovalCard } from "../../approvals/approval-card";
import { useT } from "../../i18n";

// JEF-244: the view asks for every pending ask, not only Decision Cards.
const INCLUDED_SOURCES = ["transitions", "goal_questions"];

/**
 * Inbox zero (K63): the asks waiting for me — Decision Cards, held status
 * transitions and goal-loop questions (JEF-244) — at most five with the
 * total, answered in one click through the same `ApprovalCard` the
 * timeline, inbox detail pane and chat panel use. The server orders them
 * (risk, then deadline); an answered card leaves the list on the
 * approval:decided invalidation.
 */
export function DecisionsView() {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const { data, isLoading, error } = useQuery(inboxDecisionsOptions(wsId, INCLUDED_SOURCES));
  if (isLoading) return <p className="p-4 text-caption text-muted-foreground">{t(($) => $.decisions.loading)}</p>;
  if (error || !data) return <p className="p-4 text-caption text-muted-foreground">{t(($) => $.decisions.load_failed)}</p>;
  return (
    <div data-testid="inbox-decisions" className="flex flex-1 min-h-0 flex-col gap-2 overflow-y-auto p-3">
      {data.decisions.length === 0 ? (
        <p data-testid="inbox-decisions-empty" className="py-8 text-center text-caption text-muted-foreground">{t(($) => $.decisions.empty)}</p>
      ) : (
        data.decisions.map((d) => {
          const approval = inboxDecisionToApproval(d);
          return approval ? <ApprovalCard key={d.inbox_item_id || `${approval.source}:${approval.id}`} approval={approval} wsId={wsId} showIssue /> : null;
        })
      )}
      {data.total > data.decisions.length && (
        <p data-testid="inbox-decisions-more" className="text-center text-caption text-muted-foreground">{t(($) => $.decisions.more, { count: data.total - data.decisions.length })}</p>
      )}
    </div>
  );
}

/**
 * The decisions endpoint returns its own shape (risk-ordered, capped
 * server-side) rather than the general approvals feed — adapted here so the
 * list can render the one shared card instead of keeping a parallel
 * implementation of "answer a pending ask". Each source fills the fields
 * ApprovalCard reads for it, mirroring how the server builds the same
 * sources for the approvals feed; an entry whose payload is missing (a
 * mid-rollout server) is skipped rather than rendered broken.
 */
function inboxDecisionToApproval(item: InboxDecision): ApprovalItem | null {
  if (item.source === "transition") {
    return item.transition ? inboxTransitionToApproval(item, item.transition) : null;
  }
  if (item.source === "goal_question") {
    return item.goal_question ? inboxGoalQuestionToApproval(item, item.goal_question) : null;
  }
  const d = item.decision;
  if (!d) return null;
  return {
    id: d.id,
    source: "decision",
    kind: "decision",
    issue: { id: item.issue_id, identifier: item.issue_identifier, title: item.issue_title, status: "" },
    task_id: d.task_id ?? "",
    asked_by: { type: d.asked_by_type, id: d.asked_by_id, name: "" },
    question: d.question,
    options: d.options.map((o) => ({ id: o.id, label: o.label, impact: o.impact ?? "" })),
    recommended_option_id: d.recommended_option_id ?? "",
    urgency: d.urgency,
    created_at: d.created_at,
    expires_at: null,
    sla_deadline_at: d.sla_deadline_at ?? null,
    can_decide: true,
    cannot_decide_reason: "",
    decision: d,
    gate: null,
    transition: null,
    goal_question: null,
  };
}

/** A held status change: the card settles it through the transition hooks. */
function inboxTransitionToApproval(item: InboxDecision, tr: ApprovalTransition): ApprovalItem {
  return {
    id: tr.request_id,
    source: "transition",
    kind: "transition",
    issue: { id: item.issue_id, identifier: item.issue_identifier, title: item.issue_title, status: "" },
    task_id: "",
    asked_by: { type: "", id: "", name: "" },
    question: `Move ${item.issue_identifier} from ${tr.from_status} to ${tr.to_status}`,
    options: [
      { id: "approve", label: "Approve", impact: `the issue moves to ${tr.to_status}` },
      { id: "reject", label: "Reject", impact: "the issue stays where it is" },
    ],
    recommended_option_id: "",
    urgency: "normal",
    created_at: "",
    expires_at: null,
    sla_deadline_at: null,
    can_decide: true,
    cannot_decide_reason: "",
    decision: null,
    gate: null,
    transition: tr,
    goal_question: null,
  };
}

/** A goal-loop question: the card answers it through the goal-answer hook. */
function inboxGoalQuestionToApproval(item: InboxDecision, q: ApprovalGoalQuestion): ApprovalItem {
  return {
    id: item.inbox_item_id || q.run_id,
    source: "goal_question",
    kind: "goal_question",
    issue: { id: item.issue_id, identifier: item.issue_identifier, title: item.issue_title, status: "" },
    task_id: q.run_id,
    asked_by: { type: "agent", id: "", name: "" },
    question: q.prompt,
    options: q.options.map((o, i) => ({ id: String(i), label: o, impact: "" })),
    recommended_option_id: "",
    urgency: "normal",
    created_at: q.asked_at,
    expires_at: null,
    sla_deadline_at: null,
    can_decide: true,
    cannot_decide_reason: "",
    decision: null,
    gate: null,
    transition: null,
    goal_question: q,
  };
}
