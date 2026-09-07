"use client";

import { FolderKanban } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { goalProgressOptions } from "@multica/core/cycles";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

function ratio(done: number, total: number): number {
  return total > 0 ? Math.min(1, done / total) : 0;
}

/**
 * A goal's progress broken down by the projects linked to it (F29).
 *
 * This is what makes a goal usable as a cross-project initiative: the goal
 * tree already rolls issues up, but it could not say WHICH project is behind.
 * Done follows the workspace status catalogue server-side, so a custom
 * done-category status counts here too.
 *
 * The empty state is a call to action rather than a shrug: a goal no project
 * serves aggregates nothing, and linking one is the fix.
 */
export function GoalProgressSection({ goalId }: { goalId: string }) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data, isPending } = useQuery(goalProgressOptions(wsId, goalId));

  if (isPending) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.progress_section.loading)}
      </p>
    );
  }

  const projects = data?.projects ?? [];

  return (
    <section className="flex flex-col gap-1" data-testid="goal-progress-section">
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-caption font-medium text-muted-foreground">
          {t(($) => $.progress_section.header)}
        </h3>
        {projects.length > 0 && (
          <span className="text-caption tabular-nums text-muted-foreground">
            {t(($) => $.progress_section.total)}:{" "}
            {t(($) => $.progress_section.progress, {
              done: data?.done_count ?? 0,
              total: data?.total_count ?? 0,
            })}
          </span>
        )}
      </div>

      {projects.length === 0 ? (
        <div className="flex flex-wrap items-center gap-2">
          <p className="text-caption text-muted-foreground">
            {t(($) => $.progress_section.empty)}
          </p>
          <AppLink
            href={paths.projects()}
            className="text-caption text-primary hover:underline"
          >
            {t(($) => $.progress_section.cta)}
          </AppLink>
        </div>
      ) : (
        <ul className="flex flex-col gap-1">
          {projects.map((project) => {
            const pct = Math.round(ratio(project.done_count, project.total_count) * 100);
            return (
              <li key={project.project_id} className="flex items-center gap-2">
                <FolderKanban className="size-3.5 shrink-0 text-muted-foreground" />
                <AppLink
                  href={paths.projectDetail(project.project_id)}
                  className="min-w-0 flex-1 truncate text-body hover:underline"
                >
                  {project.name}
                </AppLink>
                <div
                  className="h-1.5 w-24 shrink-0 overflow-hidden rounded-full bg-muted"
                  role="progressbar"
                  aria-label={project.name}
                  aria-valuenow={pct}
                  aria-valuemin={0}
                  aria-valuemax={100}
                >
                  <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
                </div>
                <span className="shrink-0 text-caption tabular-nums text-muted-foreground">
                  {t(($) => $.progress_section.progress, {
                    done: project.done_count,
                    total: project.total_count,
                  })}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
