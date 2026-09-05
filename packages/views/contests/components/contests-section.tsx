"use client";

import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueContestsOptions, type Contest } from "@multica/core/issues/contest";
import { useT } from "../../i18n";
import { ContestPanel } from "./contest-panel";

/** Every contest raised on one of this issue's outputs, newest first, each panel collapsible. */
export function ContestsSection({ issueId }: { issueId: string }) {
  const wsId = useWorkspaceId();
  const { data: contests = [] } = useQuery(issueContestsOptions(wsId, issueId));
  return <ContestList contests={contests} />;
}

export function ContestList({ contests }: { contests: Contest[] }) {
  const { t } = useT("contests");
  if (contests.length === 0) return null;
  const sorted = [...contests].sort((a, b) => b.created_at.localeCompare(a.created_at));
  return (
    <div data-testid="contests-section" className="flex flex-col gap-1.5 text-caption">
      <div className="font-medium">{t(($) => $.section.count, { count: sorted.length })}</div>
      {sorted.map((c, i) => (
        <details key={c.id} open={i === 0} className="group">
          <summary className="cursor-pointer select-none text-muted-foreground hover:text-foreground">
            {t(($) => $.target[c.target_type])} · {t(($) => $.status[c.status])}
          </summary>
          <div className="mt-1">
            <ContestPanel contest={c} />
          </div>
        </details>
      ))}
    </div>
  );
}
