"use client";

import { useQuery, useInfiniteQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDecisionsOptions } from "@multica/core/issues/decisions";
import { issueUsageOptions } from "@multica/core/issues/queries";
import { usdFromTicks } from "@multica/core/issues/cockpit";
import { ISSUE_ROLE_VIEWS, useIssueRoleViewStore, type IssueRoleView } from "@multica/core/issues/role-view-store";
import { auditLogInfiniteOptions } from "@multica/core/workspace/audit";
import type { Issue } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { AgentScorecardSection } from "../../agents/components/agent-scorecard-section";
import { useT, useTimeAgo } from "../../i18n";
import { AcceptanceCriteriaSection } from "./acceptance-criteria-section";
import { DecisionCardsSection } from "./decision-cards-section";
import { ExecutionLogSection } from "./execution-log-section";
import { MergeReadinessPanel } from "./merge-readiness-panel";
import { PlanVerificationSection } from "./plan-verification-section";

/**
 * Role views (K32): the same issue read three ways from data the page already
 * loads. PM leads with decisions and proofs, QA with criteria and the
 * verification report, CTO with cost, the agent's scorecard and the audit
 * trail. "Full" is the page as it always was. Pure display: nothing is
 * hidden from anyone, only reordered.
 */
export function RoleViewTabs() {
  const { t } = useT("issues");
  const view = useIssueRoleViewStore((s) => s.view);
  const setView = useIssueRoleViewStore((s) => s.setView);
  return (
    <div role="tablist" aria-label={t(($) => $.role_view.label)} className="mb-2 flex gap-1 px-2">
      {ISSUE_ROLE_VIEWS.map((v) => (
        <button
          key={v}
          type="button"
          role="tab"
          aria-selected={v === view}
          data-active={v === view ? "" : undefined}
          onClick={() => setView(v)}
          className={cn(
            "rounded-md px-2 py-0.5 text-caption transition-colors hover:bg-accent/70",
            v === view ? "bg-accent font-medium text-foreground" : "text-muted-foreground",
          )}
        >
          {t(($) => $.role_view[v])}
        </button>
      ))}
    </div>
  );
}

export function RoleView({ view, issue }: { view: Exclude<IssueRoleView, "full">; issue: Issue }) {
  switch (view) {
    case "pm":
      return <PMView issue={issue} />;
    case "qa":
      return <QAView issue={issue} />;
    case "cto":
      return <CTOView issue={issue} />;
    default:
      return null;
  }
}

function PMView({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { data: decisions = [], isFetched } = useQuery(issueDecisionsOptions(wsId, issue.id));
  const noDecision = isFetched && decisions.length === 0;
  return (
    <div data-testid="role-view-pm" className="flex flex-col gap-3">
      <DecisionCardsSection issueId={issue.id} />
      {noDecision && <p data-testid="pm-empty" className="px-2 text-caption text-muted-foreground">{t(($) => $.role_view.pm_empty)}</p>}
      <AcceptanceCriteriaSection issueId={issue.id} />
      <PlanVerificationSection issueId={issue.id} />
      {noDecision && <ExecutionLogSection issueId={issue.id} identifier={issue.identifier} />}
    </div>
  );
}

function QAView({ issue }: { issue: Issue }) {
  return (
    <div data-testid="role-view-qa" className="flex flex-col gap-3">
      <AcceptanceCriteriaSection issueId={issue.id} />
      <PlanVerificationSection issueId={issue.id} />
      <div className="pl-2">
        <MergeReadinessPanel issueId={issue.id} />
      </div>
    </div>
  );
}

function CTOView({ issue }: { issue: Issue }) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const usage = useQuery(issueUsageOptions(issue.id));
  const audit = useInfiniteQuery(auditLogInfiniteOptions(wsId, { entity_id: issue.id }));
  const entries = audit.data?.pages[0]?.entries ?? [];
  const cost = usdFromTicks(usage.data?.cost_usd_ticks ?? null);
  const agentId = issue.assignee_type === "agent" ? issue.assignee_id : null;
  return (
    <div data-testid="role-view-cto" className="flex flex-col gap-3 text-caption">
      <div>
        <div className="mb-1 px-2 font-medium">{t(($) => $.role_view.cto_cost)}</div>
        <div className="px-2 text-muted-foreground" data-testid="cto-cost">
          {usage.isError
            ? t(($) => $.role_view.block_failed)
            : usage.data
              ? t(($) => $.role_view.cto_cost_value, {
                  cost: cost === null ? "—" : `$${cost.toFixed(2)}`,
                  tokens: (usage.data.total_input_tokens + usage.data.total_output_tokens).toLocaleString(),
                })
              : "…"}
        </div>
      </div>
      <div>
        <div className="mb-1 px-2 font-medium">{t(($) => $.role_view.cto_scorecard)}</div>
        {agentId ? <AgentScorecardSection agentId={agentId} /> : <p className="px-2 text-muted-foreground">{t(($) => $.role_view.cto_no_agent)}</p>}
      </div>
      <div>
        <div className="mb-1 px-2 font-medium">{t(($) => $.role_view.cto_audit)}</div>
        {audit.isError ? (
          <p className="px-2 text-muted-foreground">{t(($) => $.role_view.block_failed)}</p>
        ) : entries.length === 0 ? (
          <p className="px-2 text-muted-foreground">{audit.isFetched ? t(($) => $.role_view.cto_audit_empty) : "…"}</p>
        ) : (
          <ul className="flex flex-col gap-0.5 px-2">
            {entries.slice(0, 10).map((e) => (
              <li key={e.id} data-testid="cto-audit-row" className="flex gap-2">
                <span className="shrink-0 text-muted-foreground" title={e.occurred_at}>{timeAgo(e.occurred_at)}</span>
                <span className="font-mono">{e.action}</span>
                <span className="truncate text-muted-foreground">{e.actor_type}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
