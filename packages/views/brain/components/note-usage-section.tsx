"use client";

import { useEffect, useRef } from "react";
import { useQuery } from "@tanstack/react-query";
import { Lock } from "lucide-react";
import { noteUsageOptions } from "@multica/core/brain/queries";
import { useRecordNoteView } from "@multica/core/brain/mutations";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useT, useTimeAgo } from "../../i18n";
import { OpenRunButton } from "../../common/task-transcript/open-run-button";

/**
 * Counts one read of the note shown, once per note for the life of the
 * reader (toggling the editor does not count again); the server also dedupes
 * by day. Fire-and-forget: a lost count is not the reader's problem.
 */
export function useRecordNoteViewOnce(wsId: string, noteId: string) {
  const recordView = useRecordNoteView(wsId);
  const recorded = useRef<string | null>(null);
  useEffect(() => {
    if (wsId === "" || noteId === "" || recorded.current === noteId) return;
    recorded.current = noteId;
    recordView.mutate(noteId);
  }, [noteId, recordView, wsId]);
}

/**
 * The note's "Usage" section (JEF-413): how many runs used it, how, when last,
 * and the most recent runs. People are counted, never listed.
 */
export function NoteUsageSection({ wsId, noteId }: { wsId: string; noteId: string }) {
  const { t } = useT("brain");
  const timeAgo = useTimeAgo();
  const usage = useQuery(noteUsageOptions(wsId, noteId));

  const data = usage.data;
  return (
    <section className="flex flex-col gap-2 border-t pt-4" aria-label={t(($) => $.usage.heading)}>
      <h3 className="text-caption font-medium text-muted-foreground">{t(($) => $.usage.heading)}</h3>
      {usage.isLoading ? (
        <Skeleton className="h-10 w-full" />
      ) : usage.isError ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.usage.error)}</p>
      ) : !data || (data.runs_count === 0 && data.viewers_count === 0) ? (
        <p className="text-caption text-muted-foreground">{t(($) => $.usage.empty)}</p>
      ) : (
        <>
          <p className="text-body">{t(($) => $.usage.used_by_runs, { count: data.runs_count })}</p>
          <p className="text-caption text-muted-foreground">
            {[
              `${t(($) => $.usage.kind_injected)} ${data.counts.injected}`,
              `${t(($) => $.usage.kind_retrieved)} ${data.counts.retrieved}`,
              `${t(($) => $.usage.kind_opened)} ${data.counts.opened}`,
              `${t(($) => $.usage.kind_cited)} ${data.counts.cited}`,
              t(($) => $.usage.viewers, { count: data.viewers_count }),
              data.last_used_at ? t(($) => $.usage.last_used, { time: timeAgo(data.last_used_at) }) : null,
            ]
              .filter(Boolean)
              .join(" · ")}
          </p>
          {data.runs.length > 0 ? (
            <ul className="flex flex-col gap-1.5" aria-label={t(($) => $.usage.recent_runs)}>
              {data.runs.map((run, index) => (
                <li
                  key={run.task_id || `private-${index}`}
                  className="flex min-w-0 flex-wrap items-center gap-2 text-caption"
                >
                  <span className="truncate font-medium">{run.agent_name}</span>
                  {run.issue_identifier ? (
                    <span className="text-muted-foreground">{run.issue_identifier}</span>
                  ) : null}
                  <span className="text-muted-foreground">
                    {run.kinds.map((kind) => kindLabel(t, kind)).join(", ")}
                    {run.first_at ? ` · ${timeAgo(run.first_at)}` : ""}
                  </span>
                  {run.private === true ? (
                    <span className="inline-flex items-center gap-1 text-muted-foreground">
                      <Lock aria-hidden="true" className="size-3" />
                      {t(($) => $.usage.private_run)}
                    </span>
                  ) : run.task_id && run.agent_id ? (
                    <OpenRunButton
                      wsId={wsId}
                      agentId={run.agent_id}
                      taskId={run.task_id}
                      label={t(($) => $.usage.open_run)}
                    />
                  ) : null}
                </li>
              ))}
            </ul>
          ) : null}
        </>
      )}
    </section>
  );
}

function kindLabel(t: ReturnType<typeof useT<"brain">>["t"], kind: string): string {
  switch (kind) {
    case "injected":
      return t(($) => $.usage.kind_injected);
    case "retrieved":
      return t(($) => $.usage.kind_retrieved);
    case "opened":
      return t(($) => $.usage.kind_opened);
    case "viewed":
      return t(($) => $.usage.kind_viewed);
    case "cited":
      return t(($) => $.usage.kind_cited);
    default:
      return kind;
  }
}
