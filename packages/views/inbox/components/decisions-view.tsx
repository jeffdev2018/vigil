"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { inboxDecisionsOptions, type InboxDecision } from "@multica/core/inbox/queries";
import type { ApprovalItem } from "@multica/core/approvals";
import { ApprovalCard } from "../../approvals/approval-card";
import { useT } from "../../i18n";

/**
 * Inbox zero (K63): the Decision Cards waiting for me, at most five with
 * the total, answered in one click through the same `ApprovalCard` the
 * timeline, inbox detail pane and chat panel use. The server orders them
 * (risk, then deadline); an answered card leaves the list on the refetch.
 */
export function DecisionsView() {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const { data, isLoading, error } = useQuery(inboxDecisionsOptions(wsId));
  if (isLoading) return <p className="p-4 text-caption text-muted-foreground">{t(($) => $.decisions.loading)}</p>;
  if (error || !data) return <p className="p-4 text-caption text-muted-foreground">{t(($) => $.decisions.load_failed)}</p>;
  return (
    <div data-testid="inbox-decisions" className="flex flex-1 min-h-0 flex-col gap-2 overflow-y-auto p-3">
      {data.decisions.length === 0 ? (
        <p data-testid="inbox-decisions-empty" className="py-8 text-center text-caption text-muted-foreground">{t(($) => $.decisions.empty)}</p>
      ) : (
        data.decisions.map((d) => <ApprovalCard key={d.decision.id} approval={inboxDecisionToApproval(d)} wsId={wsId} showIssue />)
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
 * implementation of "answer a Decision Card".
 */
function inboxDecisionToApproval(item: InboxDecision): ApprovalItem {
  const d = item.decision;
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
