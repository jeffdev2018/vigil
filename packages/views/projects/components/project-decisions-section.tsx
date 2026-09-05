"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lightbulb } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectDecisionsOptions, type DecisionAuthorFilter } from "@multica/core/projects/decisions";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";

/**
 * Decision memory (K29): the decisions recorded on this project's issues,
 * newest first, each linking to the issue and naming the run message that
 * states it. Filterable by author (agent or member).
 */
export function ProjectDecisionsSection({ projectId }: { projectId: string }) {
  const { t } = useT("projects");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const [author, setAuthor] = useState<DecisionAuthorFilter>("");
  const { data: decisions = [], isLoading } = useQuery(projectDecisionsOptions(wsId, projectId, author));

  return (
    <div data-testid="project-decisions" className="mt-6 text-caption">
      <div className="mb-2 flex items-center gap-2 px-2">
        <Lightbulb className="h-4 w-4 text-muted-foreground" />
        <span className="font-medium">{t(($) => $.decisions.section)}</span>
        <span className="flex-1" />
        <select
          aria-label={t(($) => $.decisions.author_filter)}
          className="h-7 rounded-md border bg-background px-2"
          value={author}
          onChange={(e) => setAuthor(e.target.value as DecisionAuthorFilter)}
        >
          <option value="">{t(($) => $.decisions.author_any)}</option>
          <option value="agent">{t(($) => $.decisions.author_agent)}</option>
          <option value="member">{t(($) => $.decisions.author_member)}</option>
        </select>
      </div>
      {isLoading ? null : decisions.length === 0 ? (
        <p data-testid="project-decisions-empty" className="px-2 text-muted-foreground">{t(($) => $.decisions.empty)}</p>
      ) : (
        <ul className="flex flex-col gap-2 px-2">
          {decisions.map((d) => (
            <li key={d.id} data-testid="decision-record" className="rounded-md border p-2">
              <details>
                <summary className="flex cursor-pointer items-center gap-2">
                  <span className="min-w-0 flex-1 truncate font-medium">{d.title}</span>
                  <span className="shrink-0 text-muted-foreground">
                    {d.author_type === "agent" ? t(($) => $.decisions.author_agent) : t(($) => $.decisions.author_member)}
                    {" · "}
                    {timeAgo(d.created_at)}
                  </span>
                </summary>
                <div className="mt-2 flex flex-col gap-1 pl-1">
                  {d.context && <p className="whitespace-pre-wrap text-muted-foreground">{d.context}</p>}
                  <p className="whitespace-pre-wrap">{d.decision}</p>
                  {d.consequences && <p className="whitespace-pre-wrap text-muted-foreground">{d.consequences}</p>}
                  <p className="text-muted-foreground">
                    <AppLink href={paths.issueDetail(d.issue_id)} className="text-foreground hover:underline">
                      {d.issue_identifier || d.issue_id}
                    </AppLink>
                    {d.issue_title ? ` ${d.issue_title}` : ""}
                    {" · "}
                    {t(($) => $.decisions.source, { seq: d.source_message_seq })}
                  </p>
                </div>
              </details>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
