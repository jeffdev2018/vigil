"use client";

import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, CircleDashed } from "lucide-react";
import { issuePRStackOptions } from "@multica/core/github/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

/**
 * PR stack (F10): the issue and everything that must land before it, one
 * indented row per level of blocking issue, each with its PR readiness.
 * Nothing renders when the issue has no blockers.
 */
export function PRStackList({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const paths = useWorkspacePaths();
  const { data } = useQuery(issuePRStackOptions(issueId));
  if (!data || data.nodes.length <= 1) return null;

  return (
    <div data-testid="pr-stack" className="flex flex-col gap-1 py-1 text-caption">
      <div className="font-medium text-muted-foreground">{t(($) => $.detail.pr_stack_title)}</div>
      {data.cyclic === true && (
        <div className="text-warning">{t(($) => $.detail.pr_stack_cyclic)}</div>
      )}
      <ul className="flex flex-col">
        {data.nodes.map((node) => (
          <li
            key={node.issue_id}
            data-depth={node.depth}
            style={{ paddingLeft: `${node.depth * 12}px` }}
            className="flex items-center gap-1.5 py-0.5"
          >
            {node.ready === true ? (
              <CheckCircle2 aria-hidden className="h-3.5 w-3.5 shrink-0 text-success" />
            ) : (
              <CircleDashed aria-hidden className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            )}
            <AppLink
              href={paths.issueDetail(node.issue_id)}
              className={cn("flex min-w-0 items-center gap-1", node.issue_id === issueId ? "font-medium" : "")}
            >
              <span className="shrink-0 text-muted-foreground">{node.identifier}</span>
              <span className="truncate">{node.title}</span>
            </AppLink>
            {node.prs.length > 0 && (
              <span className="shrink-0 text-muted-foreground">
                {node.prs.map((pr) => `#${pr.number}`).join(" ")}
              </span>
            )}
          </li>
        ))}
      </ul>
      {data.truncated === true && (
        <div className="text-muted-foreground">
          {t(($) => $.detail.pr_stack_truncated, { depth: data.nodes.length - 1 })}
        </div>
      )}
    </div>
  );
}
