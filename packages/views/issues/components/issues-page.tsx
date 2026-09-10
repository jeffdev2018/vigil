"use client";

import { ListTodo } from "lucide-react";
import type {
  Issue,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
  WorkingAgentSummary,
} from "@multica/core/types";
import { useIssuesScope } from "@multica/core/issues/stores/issues-scope-store";
import { useViewStore } from "@multica/core/issues/stores/view-store-context";
import { useWorkspaceId } from "@multica/core/hooks";
import { paths, useCurrentWorkspace } from "@multica/core/paths";
import { RunHaltBanner } from "../../approvals";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { AppLink } from "../../navigation";
import { GettingStartedCard } from "../../onboarding";
import { IssueSurface } from "../surface/issue-surface";
import { IssuesHeader } from "./issues-header";

function IssuesSurfaceHeader({
  issues,
  workingAgents,
  isRefreshing,
  facetCountsExact,
  tableFacetCounts,
  onTableFacetChange,
}: {
  issues: Issue[];
  workingAgents: WorkingAgentSummary[] | undefined;
  isRefreshing: boolean;
  facetCountsExact: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange: (facet: IssueTableFacetSpec | null) => void;
}) {
  const dateFilter = useViewStore((s) => s.dateFilter);
  const setDateFilter = useViewStore((s) => s.setDateFilter);

  return (
    <IssuesHeader
      scopedIssues={issues}
      workingAgents={workingAgents}
      dateFilter={dateFilter}
      onDateFilterChange={setDateFilter}
      isRefreshing={isRefreshing}
      facetCountsExact={facetCountsExact}
      tableFacetCounts={tableFacetCounts}
      onTableFacetChange={onTableFacetChange}
    />
  );
}

export function IssuesPage() {
  const { t } = useT("issues");
  const scope = useIssuesScope("issues");
  const wsId = useWorkspaceId();
  const workspace = useCurrentWorkspace();

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader>
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-body font-medium">{t(($) => $.page.breadcrumb_title)}</h1>
      </PageHeader>

      <RunHaltBanner wsId={wsId} />

      {/* Getting-started checklist (OS plan, chantier 5): mounted on the
          page a fresh workspace lands on. There is no separate dashboard
          route — the root workspace route and the post-onboarding redirect
          both point at /issues. */}
      {workspace && <GettingStartedCard wsId={wsId} wsSlug={workspace.slug} />}

      <IssueSurface
        scope={{ type: "workspace", actorKind: scope }}
        modes={["board", "list", "table", "swimlane", "gantt", "calendar"]}
        batchToolbar="list"
        renderHeader={({ controller }) => (
          <IssuesSurfaceHeader
            issues={controller.surfaceIssues}
            workingAgents={controller.workingAgents}
            isRefreshing={controller.isRefreshing}
            facetCountsExact={controller.facetCountsExact}
            tableFacetCounts={controller.tableFacetCounts}
            onTableFacetChange={controller.setActiveTableFacet}
          />
        )}
        renderEmpty={() => (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-faint-foreground" />
            <p className="text-body">{t(($) => $.page.empty_title)}</p>
            <p className="text-caption">{t(($) => $.page.empty_hint)}</p>
            {workspace && (
              <p className="text-caption">
                {t(($) => $.page.empty_run_explainer)}{" "}
                <AppLink
                  href={paths.workspace(workspace.slug).newAgent()}
                  className="underline decoration-muted-foreground/30 underline-offset-4 hover:text-foreground"
                >
                  {t(($) => $.page.empty_run_explainer_link)}
                </AppLink>
              </p>
            )}
          </div>
        )}
      />
    </div>
  );
}
