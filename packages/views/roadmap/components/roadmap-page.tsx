"use client";

import { useEffect, useMemo, useState } from "react";
import { Loader2, Route, Waypoints } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import { projectDependencyIssuesOptions } from "@multica/core/issues/queries";
import { issueDependencyEdgesOptions } from "@multica/core/issues/dependency-edges";
import { buildRoadmapTimeline } from "@multica/core/roadmap";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { AppLink } from "../../navigation";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../../layout/collection-page";
import { useT } from "../../i18n";
import { ProjectTimeline } from "./project-timeline";
import { DependencyGraph } from "./dependency-graph";

type RoadmapTab = "timeline" | "graph";

function LoadingRow({ label }: { label: string }) {
  return (
    <div
      role="status"
      className="flex flex-1 items-center justify-center gap-2 py-16 text-caption text-muted-foreground"
    >
      <Loader2 className="size-4 animate-spin" aria-hidden="true" />
      {label}
    </div>
  );
}

function TimelineTab({ projects }: { projects: Project[] }) {
  const { t } = useT("roadmap");
  const paths = useWorkspacePaths();
  const timeline = useMemo(() => buildRoadmapTimeline(projects), [projects]);

  if (projects.length === 0) {
    return (
      <CollectionPageState
        icon={Route}
        title={t(($) => $.page.empty_projects)}
        description={t(($) => $.page.empty_projects_description)}
      />
    );
  }

  return (
    <div className="flex-1 overflow-y-auto">
      {timeline.rows.length === 0 ? (
        <CollectionPageState
          icon={Route}
          title={t(($) => $.page.empty_scheduled)}
          description={t(($) => $.page.empty_scheduled_description)}
        />
      ) : (
        <div className="px-4 py-2">
          <ProjectTimeline timeline={timeline} />
        </div>
      )}
      {timeline.unscheduled.length > 0 && (
        <section data-testid="roadmap-unscheduled" className="px-4 py-3">
          <h2 className="mb-1 text-caption font-medium text-muted-foreground">
            {t(($) => $.page.unscheduled)}
          </h2>
          <div className="flex flex-wrap gap-2">
            {timeline.unscheduled.map((project) => (
              <AppLink
                key={project.id}
                href={paths.projectDetail(project.id)}
                className="rounded-md border border-border/60 px-2 py-1 text-caption hover:bg-accent/50"
              >
                {project.title}
              </AppLink>
            ))}
          </div>
        </section>
      )}
    </div>
  );
}

const EDGE_KINDS = ["blocks", "related", "duplicate"] as const;

function EdgeLegend() {
  const { t } = useT("roadmap");
  return (
    <div className="ml-auto flex items-center gap-3">
      {EDGE_KINDS.map((kind) => (
        <span
          key={kind}
          className="flex items-center gap-1.5 text-micro text-muted-foreground"
        >
          <span
            aria-hidden="true"
            className="h-0 w-6 border-t-2"
            style={{
              borderColor:
                kind === "blocks"
                  ? "var(--foreground)"
                  : kind === "related"
                    ? "var(--muted-foreground)"
                    : "var(--border)",
              borderTopStyle: kind === "related" ? "dashed" : "solid",
            }}
          />
          {t(($) => $.graph.legend[kind])}
        </span>
      ))}
    </div>
  );
}

function GraphTab({ projects }: { projects: Project[] }) {
  const { t } = useT("roadmap");
  const wsId = useWorkspaceId();
  const [projectId, setProjectId] = useState("");

  // The common case is "show me the graph" with no extra click: land on the
  // first project, the selector is there to switch.
  useEffect(() => {
    if (!projectId && projects.length > 0) setProjectId(projects[0]!.id);
  }, [projects, projectId]);

  const { data: issues = [], isLoading: issuesLoading } = useQuery({
    ...projectDependencyIssuesOptions(wsId, projectId),
    enabled: projectId !== "",
  });
  const issueIds = useMemo(() => issues.map((issue) => issue.id), [issues]);
  const { data: edges = [] } = useQuery({
    ...issueDependencyEdgesOptions(wsId, issueIds),
    enabled: issueIds.length > 0,
  });

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <div className="flex h-11 shrink-0 items-center gap-3 border-b px-4">
        <Select
          items={projects.map((p) => ({ value: p.id, label: p.title }))}
          value={projectId}
          onValueChange={(value) => value !== null && setProjectId(value)}
        >
          <SelectTrigger size="sm" aria-label={t(($) => $.graph.project)}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {projects.map((p) => (
              <SelectItem key={p.id} value={p.id}>
                {p.title}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {projectId !== "" && !issuesLoading && issues.length > 0 && edges.length === 0 && (
          <span className="text-caption text-muted-foreground">
            {t(($) => $.graph.no_edges)}
          </span>
        )}
        <EdgeLegend />
      </div>

      {projectId === "" ? (
        <CollectionPageState
          icon={Waypoints}
          title={t(($) => $.graph.pick_project)}
        />
      ) : issuesLoading ? (
        <LoadingRow label={t(($) => $.graph.loading_issues)} />
      ) : issues.length === 0 ? (
        <CollectionPageState
          icon={Waypoints}
          title={t(($) => $.graph.empty_issues)}
        />
      ) : (
        <div className="flex-1 min-h-0" data-testid="dependency-graph">
          <DependencyGraph issues={issues} edges={edges} />
        </div>
      )}
    </div>
  );
}

export function RoadmapPage() {
  const { t } = useT("roadmap");
  const wsId = useWorkspaceId();
  const [tab, setTab] = useState<RoadmapTab>("timeline");
  const { data: projects = [], isLoading, isError } = useQuery(
    projectListOptions(wsId),
  );

  return (
    <Tabs
      value={tab}
      onValueChange={(value) => setTab(value as RoadmapTab)}
      className="flex flex-1 min-h-0 flex-col gap-0"
    >
      <CollectionPageHeader
        icon={Route}
        title={t(($) => $.page.title)}
        count={projects.length}
      />
      <div className="flex h-12 shrink-0 items-center border-b px-4">
        <TabsList variant="line" className="gap-0 p-0 group-data-horizontal/tabs:h-full">
          <TabsTrigger
            value="timeline"
            className="h-full rounded-none px-2.5 text-label group-data-horizontal/tabs:after:bottom-0"
          >
            {t(($) => $.page.tab_timeline)}
          </TabsTrigger>
          <TabsTrigger
            value="graph"
            className="h-full rounded-none px-2.5 text-label group-data-horizontal/tabs:after:bottom-0"
          >
            {t(($) => $.page.tab_graph)}
          </TabsTrigger>
        </TabsList>
      </div>

      {isLoading ? (
        <LoadingRow label={t(($) => $.page.loading)} />
      ) : isError ? (
        <CollectionPageState
          icon={Route}
          tone="destructive"
          title={t(($) => $.page.load_error)}
        />
      ) : (
        <>
          <TabsContent value="timeline" className="flex flex-1 min-h-0 flex-col">
            <TimelineTab projects={projects} />
          </TabsContent>
          <TabsContent value="graph" className="flex flex-1 min-h-0 flex-col">
            <GraphTab projects={projects} />
          </TabsContent>
        </>
      )}
    </Tabs>
  );
}
