"use client";

import { useQuery } from "@tanstack/react-query";
import { Check, X } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  issueDependenciesOptions,
  useRemoveIssueDependency,
} from "@multica/core/issues/dependencies";
import { issueBehavesAs, issueStatusCategory } from "@multica/core/issues";
import { useWorkspacePaths } from "@multica/core/paths";
import type { IssueDependency } from "@multica/core/types";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { StatusIcon } from "./status-icon";

/**
 * "Blocks" / "Blocked by" lists in the issue detail sidebar. Renders nothing
 * when the issue has neither; links are added from the actions menu
 * (Relations submenu), like the parent issue.
 */
export function IssueDependenciesSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data } = useQuery(issueDependenciesOptions(wsId, issueId));
  const remove = useRemoveIssueDependency(wsId);

  const blocks = data?.blocks ?? [];
  const blockedBy = data?.blocked_by ?? [];
  if (blocks.length === 0 && blockedBy.length === 0) return null;

  const renderList = (label: string, items: IssueDependency[]) =>
    items.length > 0 && (
      <div>
        <div className="px-2 py-1 text-caption font-medium text-muted-foreground mb-1">{label}</div>
        <div className="pl-2">
          {items.map((dep) => {
            const done = issueBehavesAs(dep.issue, "done");
            return (
              <div
                key={dep.id}
                data-done={done ? "true" : undefined}
                className="flex items-center gap-0.5 rounded-md px-2 -mx-2 hover:bg-accent/50 transition-colors group"
              >
                <AppLink
                  href={paths.issueDetail(dep.issue.id)}
                  className={`flex flex-1 min-w-0 items-center gap-1.5 py-1.5 text-caption ${done ? "text-muted-foreground line-through" : ""}`}
                >
                  <StatusIcon
                    status={dep.issue.status}
                    category={issueStatusCategory(dep.issue) ?? undefined}
                    className="h-3.5 w-3.5 shrink-0"
                  />
                  <span className="text-muted-foreground shrink-0">{dep.issue.identifier}</span>
                  <span className="truncate group-hover:text-foreground">{dep.issue.title}</span>
                  {done && <Check aria-hidden className="h-3.5 w-3.5 shrink-0" />}
                </AppLink>
                <button
                  type="button"
                  title={t(($) => $.actions.remove_dependency)}
                  aria-label={t(($) => $.actions.remove_dependency)}
                  onClick={() => remove.mutate({ issueId, dependencyId: dep.id })}
                  className="shrink-0 rounded p-1 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground focus-visible:opacity-100 group-hover:opacity-100"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              </div>
            );
          })}
        </div>
      </div>
    );

  return (
    <div className="flex flex-col gap-2">
      {renderList(t(($) => $.detail.section_blocked_by), blockedBy)}
      {renderList(t(($) => $.detail.section_blocks), blocks)}
    </div>
  );
}
