"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { issueOrgOffersOptions, orgIsLive, orgResolveOptions, useEscalateIssue, useRouteIssueNow } from "@multica/core/org";
import { contestCostUsd } from "@multica/core/issues/contest";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useT } from "../../i18n";

const errorMessage = (e: unknown, fallback: string) => (e instanceof Error && e.message ? e.message : fallback);

/**
 * Org chart hooks on an issue (K75): the market offers it received, and the
 * escalate / route-now controls. Silent when no structure acts on the issue.
 */
export function IssueOrgSection({ issueId, issue }: { issueId: string; issue: Pick<Issue, "project_id" | "assignee_id"> }) {
  const { t } = useT("org");
  const wsId = useWorkspaceId();
  const { data: offers = [] } = useQuery(issueOrgOffersOptions(wsId, issueId));
  const { data: structure } = useQuery(orgResolveOptions(wsId, issue.project_id));
  const escalate = useEscalateIssue(wsId);
  const routeNow = useRouteIssueNow(wsId);
  const [escalating, setEscalating] = useState(false);
  const [reason, setReason] = useState("");

  const live = structure != null && orgIsLive(structure);
  if (offers.length === 0 && !live) return null;

  const submitEscalate = () =>
    escalate.mutate(
      { issueId, reason: reason.trim() },
      {
        onSuccess: () => {
          toast.success(t(($) => $.issue_section.escalated));
          setEscalating(false);
          setReason("");
        },
        onError: (e) => toast.error(errorMessage(e, t(($) => $.issue_section.error))),
      },
    );
  const route = () =>
    routeNow.mutate(issueId, {
      onSuccess: () => toast.success(t(($) => $.issue_section.routed)),
      onError: (e) => toast.error(errorMessage(e, t(($) => $.issue_section.error))),
    });

  return (
    <div data-testid="issue-org-section" className="flex flex-col gap-1.5 text-caption">
      {offers.length > 0 && (
        <>
          <div className="font-medium">{t(($) => $.issue_section.offers)}</div>
          <table className="w-full">
            <thead className="text-left text-muted-foreground">
              <tr>
                <th className="py-0.5 pr-2 font-normal">{t(($) => $.issue_section.agent)}</th>
                <th className="py-0.5 pr-2 font-normal">{t(($) => $.issue_section.confidence)}</th>
                <th className="py-0.5 pr-2 font-normal">{t(($) => $.issue_section.cost)}</th>
                <th className="py-0.5 pr-2 font-normal">{t(($) => $.issue_section.eta)}</th>
                <th className="py-0.5 font-normal">{t(($) => $.issue_section.status)}</th>
              </tr>
            </thead>
            <tbody>
              {offers.map((o) => (
                <tr key={o.id} data-testid="org-offer" className="border-t border-border/60">
                  <td className="py-0.5 pr-2 font-medium">{o.agent_name || o.agent_id}</td>
                  <td className="py-0.5 pr-2 tabular-nums">{`${Math.round(o.confidence * 100)}%`}</td>
                  <td className="py-0.5 pr-2 tabular-nums">{`$${contestCostUsd(o.cost_usd_ticks)}`}</td>
                  <td className="py-0.5 pr-2 tabular-nums">{t(($) => $.issue_section.eta_hours, { hours: o.eta_hours })}</td>
                  <td className="py-0.5">{t(($) => $.offer_status[o.status])}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
      {live && (
        <div className="flex items-center gap-1">
          <Button type="button" variant="outline" size="sm" onClick={() => setEscalating(true)}>{t(($) => $.issue_section.escalate)}</Button>
          {!issue.assignee_id && (
            <Button type="button" variant="outline" size="sm" disabled={routeNow.isPending} onClick={route}>{t(($) => $.issue_section.route_now)}</Button>
          )}
        </div>
      )}
      {escalating && (
        <Dialog open onOpenChange={(o) => { if (!o) setEscalating(false); }}>
          <DialogContent className="sm:max-w-md">
            <DialogHeader>
              <DialogTitle>{t(($) => $.issue_section.escalate_title)}</DialogTitle>
              <DialogDescription>{t(($) => $.issue_section.escalate_description)}</DialogDescription>
            </DialogHeader>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.issue_section.reason)}
              <Input value={reason} onChange={(e) => setReason(e.target.value)} placeholder={t(($) => $.issue_section.reason_placeholder)} autoFocus />
            </label>
            <DialogFooter>
              <Button type="button" variant="outline" size="sm" onClick={() => setEscalating(false)}>{t(($) => $.issue_section.cancel)}</Button>
              <Button type="button" size="sm" disabled={escalate.isPending || !reason.trim()} onClick={submitEscalate}>{t(($) => $.issue_section.submit)}</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}
