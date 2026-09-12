"use client";

import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { openCreateIssueWithPreference } from "@multica/core/issues/stores";
import type { Cycle } from "@multica/core/types";
import { useNavigation } from "../../navigation";

/**
 * Where a generic "New issue" entry point files the issue, from the page the
 * user is on: a project page seeds its project; a cycle page seeds the
 * cycle's project and plans the issue into the cycle. `cycleProjectId` reads
 * the cycle the page has already loaded; an unknown cycle seeds nothing, since
 * the server refuses a cycle without its project.
 */
export function issueCreateContext(
  pathname: string,
  cycleProjectId: (cycleId: string) => string | undefined,
): Record<string, string> | undefined {
  const match = /^\/[^/]+\/(projects|cycles)\/([^/]+)\/?$/.exec(pathname);
  if (!match) return undefined;
  const id = decodeURIComponent(match[2]!);
  if (match[1] === "projects") return { project_id: id };
  const projectId = cycleProjectId(id);
  return projectId ? { project_id: projectId, cycle_id: id } : undefined;
}

/**
 * Opens the create-issue flow (in the user's preferred mode) seeded with the
 * current page's project and cycle. The sidebar button, the command palette
 * and the `c` shortcut all go through this, so none of them files an issue
 * outside the project the user is looking at. The cycle is read from what
 * the cycle page already loaded; cycle ids are unique across workspaces, so
 * no workspace id is needed to find it.
 */
export function useOpenContextualCreateIssue() {
  const { pathname } = useNavigation();
  const qc = useQueryClient();
  return useCallback(
    () =>
      openCreateIssueWithPreference(
        issueCreateContext(pathname, (cycleId) =>
          qc
            // Every cycle query (detail and lists) lives under ["cycles", wsId, ...].
            .getQueriesData<Cycle | Cycle[]>({ queryKey: ["cycles"] })
            .flatMap(([, data]) => (Array.isArray(data) ? data : data ? [data] : []))
            .find((cycle) => cycle.id === cycleId && typeof cycle.project_id === "string")
            ?.project_id,
        ),
      ),
    [pathname, qc],
  );
}
