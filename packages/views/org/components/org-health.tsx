"use client";

import { useQuery } from "@tanstack/react-query";
import { orgHealthOptions } from "@multica/core/org";
import { useWorkspaceId } from "@multica/core/hooks";
import { useActorName } from "@multica/core/workspace/hooks";
import type { OrgProposal } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";
import { orgFormatUsd, orgProposalBody, orgProposalMeasure, orgProposalTitle } from "../labels";

/**
 * Restructuring proposals carry raw domain values in `params` for
 * interpolation. `budget_spent` is the one money-shaped proposal — its
 * `spend`/`budget` params are `spend_usd_ticks`/`budget_usd_ticks` (see
 * server/internal/handler/org_ops.go orgHealth), 1,000,000 ticks = $1 (see
 * `orgFormatUsd`). Passed straight through, the interpolated sentence would
 * show a raw tick integer instead of a dollar amount — format them here
 * before they reach orgProposalMeasure/Body.
 */
function moneyParams(p: OrgProposal): Record<string, string | number> {
  if (p.code !== "budget_spent") return p.params ?? {};
  const params = p.params ?? {};
  return { ...params, spend: orgFormatUsd(Number(params.spend ?? 0)), budget: orgFormatUsd(Number(params.budget ?? 0)) };
}

/**
 * "Activity, last 7 days" — the inspector's version of the old settings-tab
 * health table. No nine-tile grid of zeroes: when nothing happened, one
 * sentence says so. Otherwise the non-zero counters in a compact list, the
 * restructuring proposals (translated), then per-team vacant roles and
 * saturated agents — by name, never by id (the server sends agent ids in
 * `saturated_agents`; `vacant_roles` already carries role names).
 */
export function OrgHealth({ structureId }: { structureId: string }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { getActorName } = useActorName();
  const { data: health, isPending, isError, refetch } = useQuery(orgHealthOptions(wsId, structureId));

  if (isPending) return <p className="text-caption text-muted-foreground">{t(($) => $.health.loading)}</p>;
  if (isError) {
    return (
      <div role="alert" className="flex items-center gap-2 text-caption text-destructive">
        <span>{t(($) => $.form.error)}</span>
        <Button size="sm" variant="outline" onClick={() => void refetch()}>{t(($) => $.catalog.retry)}</Button>
      </div>
    );
  }

  const totalActivity =
    health.routed + health.unrouted + health.escalations + Number(health.stacked_escalations) +
    health.reassigned_outside + health.market_short + health.breakers + health.human_review_items;
  // Vacant roles are already listed under "To watch", computed from the
  // definition being edited — fresher than this measured snapshot. Repeating
  // them here said the same thing up to three times, so they stay there only.
  const proposals = health.proposals.filter((p) => p.code !== "vacant_roles" && !p.key.startsWith("vacant:"));
  const notableUnits = health.units.filter((u) => u.saturated_agents.length > 0);
  const isEmpty = totalActivity === 0 && proposals.length === 0 && notableUnits.length === 0;

  if (isEmpty) {
    return <p data-testid="org-health-empty" className="text-caption text-muted-foreground">{t(($) => $.inspector.health.empty)}</p>;
  }

  const counters: [string, number, string][] = [
    [t(($) => $.health.routed), health.routed, String(health.routed)],
    [t(($) => $.health.unrouted), health.unrouted, String(health.unrouted)],
    [t(($) => $.health.escalations), health.escalations, String(health.escalations)],
    [t(($) => $.health.stacked), Number(health.stacked_escalations), String(health.stacked_escalations)],
    [t(($) => $.health.reassigned_outside), health.reassigned_outside, String(health.reassigned_outside)],
    [t(($) => $.health.market_short), health.market_short, String(health.market_short)],
    [t(($) => $.health.breakers), health.breakers, String(health.breakers)],
    [t(($) => $.health.human_review), health.human_review_items, String(health.human_review_items)],
    [t(($) => $.health.drift), Math.round(health.drift_rate * 100), `${Math.round(health.drift_rate * 100)}%`],
  ];

  return (
    <div data-testid="org-health" className="flex flex-col gap-3">
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-caption">
        {counters.filter(([, raw]) => raw !== 0).map(([label, , formatted]) => (
          <div key={label} className="contents">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="text-right tabular-nums">{formatted}</dd>
          </div>
        ))}
      </dl>
      {proposals.length > 0 && (
        <ul className="flex flex-col gap-1.5">
          {proposals.map((p) => (
            <li key={p.key} data-testid="org-proposal" className="rounded-md border border-surface-border p-2 text-caption">
              <div className="font-medium">{orgProposalTitle(t, p.title, p.code, moneyParams(p))}</div>
              <div className="text-muted-foreground">{orgProposalBody(t, p.body, p.code, moneyParams(p))}</div>
              {p.measure && <div className="text-muted-foreground">{t(($) => $.health.measure, { measure: orgProposalMeasure(t, p.measure, p.code, moneyParams(p)) })}</div>}
            </li>
          ))}
        </ul>
      )}
      {notableUnits.length > 0 && (
        <ul className="flex flex-col gap-1">
          {notableUnits.map((u) => (
            <li key={u.unit_id} data-testid="org-health-unit" className="text-caption">
              <span className="font-medium">{u.name}</span>
              {u.saturated_agents.length > 0 && (
                <span className="ml-1 text-muted-foreground">
                  {t(($) => $.inspector.health.unit_saturated, { agents: u.saturated_agents.map((id) => getActorName("agent", id)).join(", ") })}
                </span>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
