"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { morningBriefingOptions } from "@multica/core/inbox/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useWorkspacePaths } from "@multica/core/paths";
import type { BriefingItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Morning briefing (K30): done in the last day, awaiting review, blocked and
 * why. Sections without content are not shown. The one-click actions are the
 * product's ordinary moves: approve is the same status change as the review
 * cockpit's, and a blocked issue opens where its card waits.
 */
export function MorningBriefingView() {
  const { t } = useT("inbox");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const timeAgo = useTimeAgo();
  const { data, isLoading, isError } = useQuery(morningBriefingOptions(wsId));
  const update = useUpdateIssue();

  if (isLoading) return <div className="p-4 text-caption text-muted-foreground">{t(($) => $.briefing.loading)}</div>;
  if (isError || !data) return <div className="p-4 text-caption text-destructive">{t(($) => $.briefing.load_failed)}</div>;
  const empty = data.merged.length === 0 && data.awaiting_review.length === 0 && data.blocked.length === 0;

  const approve = (item: BriefingItem) =>
    update.mutate(
      { id: item.issue_id, status: "done" },
      { onError: (e) => toast.error(e instanceof Error && e.message ? e.message : t(($) => $.briefing.approve_failed)) },
    );

  return (
    <div data-testid="morning-briefing" className="flex flex-col gap-4 overflow-y-auto p-4 text-body">
      <div className="text-caption text-muted-foreground">
        {data.date}
        {data.sent_at ? ` · ${t(($) => $.briefing.sent, { ago: timeAgo(data.sent_at) })}` : ` · ${t(($) => $.briefing.not_sent)}`}
      </div>
      {empty && <div data-testid="briefing-empty" className="text-caption text-muted-foreground">{t(($) => $.briefing.empty)}</div>}
      {data.merged.length > 0 && (
        <Section testId="briefing-merged" title={t(($) => $.briefing.merged, { count: data.merged.length })}>
          {data.merged.map((it) => (
            <Row key={it.issue_id} item={it} href={paths.issueDetail(it.issue_id)} />
          ))}
        </Section>
      )}
      {data.awaiting_review.length > 0 && (
        <Section testId="briefing-review" title={t(($) => $.briefing.awaiting_review, { count: data.awaiting_review.length })}>
          {data.awaiting_review.map((it) => (
            <Row
              key={it.issue_id}
              item={it}
              href={paths.issueReview(it.issue_id)}
              action={
                <Button type="button" size="sm" variant="outline" disabled={update.isPending} onClick={() => approve(it)}>
                  {t(($) => $.briefing.approve)}
                </Button>
              }
            />
          ))}
        </Section>
      )}
      {data.blocked.length > 0 && (
        <Section testId="briefing-blocked" title={t(($) => $.briefing.blocked, { count: data.blocked.length })}>
          {data.blocked.map((it) => (
            <Row key={it.issue_id} item={it} href={paths.issueDetail(it.issue_id)} />
          ))}
        </Section>
      )}
    </div>
  );
}

function Section({ title, testId, children }: { title: string; testId: string; children: React.ReactNode }) {
  return (
    <section data-testid={testId} className="flex flex-col gap-1">
      <h2 className="text-caption font-medium text-muted-foreground">{title}</h2>
      <ul className="flex flex-col gap-1">{children}</ul>
    </section>
  );
}

function Row({ item, href, action }: { item: BriefingItem; href: string; action?: React.ReactNode }) {
  const { t } = useT("inbox");
  return (
    <li className="flex items-start gap-2 rounded-md border px-2 py-1.5 text-caption">
      <AppLink href={href} className="min-w-0 flex-1 hover:underline">
        <span className="font-mono text-muted-foreground">{item.identifier}</span> {item.title}
        {item.reason && <div className="truncate text-muted-foreground" title={item.reason}>{item.reason}</div>}
        {item.pending_decisions ? <div className="text-warning">{t(($) => $.briefing.pending_decisions, { count: item.pending_decisions })}</div> : null}
      </AppLink>
      {action}
    </li>
  );
}
