"use client";

import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { inboxDecisionsOptions, inboxKeys, type InboxDecision } from "@multica/core/inbox/queries";
import { useRespondIssueDecision } from "@multica/core/issues/decisions";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DecisionAnswer } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Inbox zero (K63): the Decision Cards waiting for me, at most five with
 * the total, answered in one click from the list through the ordinary
 * respond endpoint (K01). The server orders them (risk, then deadline);
 * an answered card leaves the list on the refetch.
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
        data.decisions.map((d) => <DecisionCard key={d.decision.id} item={d} />)
      )}
      {data.total > data.decisions.length && (
        <p data-testid="inbox-decisions-more" className="text-center text-caption text-muted-foreground">{t(($) => $.decisions.more, { count: data.total - data.decisions.length })}</p>
      )}
    </div>
  );
}

function DecisionCard({ item }: { item: InboxDecision }) {
  const { t } = useT("inbox");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const qc = useQueryClient();
  const respond = useRespondIssueDecision(wsId);
  const [other, setOther] = useState<string | null>(null);
  const [done, setDone] = useState(false);
  const d = item.decision;
  const answer = (a: DecisionAnswer) =>
    respond.mutate(
      { issueId: item.issue_id, decisionId: d.id, answer: a },
      {
        onSuccess: () => {
          setDone(true);
          qc.invalidateQueries({ queryKey: inboxKeys.decisions(wsId) });
          qc.invalidateQueries({ queryKey: inboxKeys.attention(wsId) });
        },
        onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.decisions.respond_failed)),
      },
    );
  const urgency: "high" | "normal" | "low" = d.urgency === "high" ? "high" : d.urgency === "low" ? "low" : "normal";
  return (
    <div data-testid="inbox-decision" data-answered={done} className={cn("flex flex-col gap-1.5 rounded-md border p-2 text-caption", d.urgency === "high" ? "border-warning/60" : "border-border")}>
      <div className="flex items-center gap-2 text-muted-foreground">
        <AppLink href={paths.issueDetail(item.issue_id)} className="font-mono hover:underline">{item.issue_identifier || item.issue_id.slice(0, 8)}</AppLink>
        <span className="min-w-0 flex-1 truncate">{item.issue_title}</span>
        <span className={cn("rounded px-1", d.urgency === "high" ? "bg-warning/20 text-warning" : "bg-muted")}>{t(($) => $.decisions.urgency[urgency])}</span>
        {d.sla_deadline_at && <span>{t(($) => $.decisions.due, { when: timeAgo(d.sla_deadline_at) })}</span>}
      </div>
      <p className="font-medium text-foreground">{d.question}</p>
      {done ? (
        <span className="text-success">{t(($) => $.decisions.answered)}</span>
      ) : other === null ? (
        <div className="flex flex-wrap gap-1">
          {d.options.map((o) => (
            <Button key={o.id} type="button" size="sm" variant={o.id === d.recommended_option_id ? "default" : "outline"} disabled={respond.isPending} onClick={() => answer({ option_id: o.id })}>
              {o.label}{o.id === d.recommended_option_id ? ` · ${t(($) => $.decisions.recommended)}` : ""}
            </Button>
          ))}
          <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => setOther("")}>{t(($) => $.decisions.other)}</button>
        </div>
      ) : (
        <form className="flex gap-1" onSubmit={(e) => { e.preventDefault(); if (other.trim()) answer({ modified_text: other.trim() }); }}>
          <input aria-label={t(($) => $.decisions.other_label)} className="flex-1 rounded-md border border-input bg-transparent px-2 py-1" value={other} onChange={(e) => setOther(e.target.value)} />
          <Button type="submit" size="sm" disabled={!other.trim() || respond.isPending}>{t(($) => $.decisions.send)}</Button>
          <Button type="button" size="sm" variant="ghost" onClick={() => setOther(null)}>{t(($) => $.decisions.cancel)}</Button>
        </form>
      )}
    </div>
  );
}
