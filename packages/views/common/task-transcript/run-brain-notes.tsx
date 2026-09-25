"use client";

import { useQuery } from "@tanstack/react-query";
import { BrainCircuit } from "lucide-react";
import { taskNoteUsageOptions } from "@multica/core/brain/queries";
import { isTaskMessageTaskId } from "@multica/core/chat/queries";
import { useT } from "../../i18n";

/**
 * "Brain notes" row of a run's transcript (JEF-413): the notes the run was
 * given, found or opened. Renders nothing until there is something to show —
 * most runs of an empty Brain have no notes, and a row saying so is noise.
 */
export function RunBrainNotes({ wsId, taskId }: { wsId: string; taskId: string }) {
  const { t } = useT("agents");
  const enabled = wsId !== "" && isTaskMessageTaskId(taskId);
  const { data } = useQuery({ ...taskNoteUsageOptions(wsId, taskId), enabled });
  const notes = data?.notes ?? [];
  if (!enabled || notes.length === 0) return null;
  return (
    <section
      className="flex shrink-0 flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-2 text-caption"
      aria-label={t(($) => $.transcript.brain_notes_heading)}
    >
      <span className="inline-flex items-center gap-1 font-medium text-muted-foreground">
        <BrainCircuit aria-hidden="true" className="size-3.5" />
        {t(($) => $.transcript.brain_notes_heading)}
      </span>
      {notes.map((note) => (
        <span key={note.note_id} className="min-w-0 max-w-full truncate">
          <span className={note.deleted === true ? "italic text-muted-foreground" : undefined}>
            {note.deleted === true || note.title === ""
              ? `${t(($) => $.transcript.brain_note_deleted)} (${note.note_id.slice(0, 8)})`
              : note.title}
          </span>
          <span className="text-muted-foreground">
            {" · "}
            {note.kinds
              .map((kind) =>
                kind === "injected"
                  ? t(($) => $.transcript.brain_kind_injected)
                  : kind === "retrieved"
                    ? t(($) => $.transcript.brain_kind_retrieved)
                    : kind === "opened"
                      ? t(($) => $.transcript.brain_kind_opened)
                      : kind === "cited"
                        ? t(($) => $.transcript.brain_kind_cited)
                        // Unknown kind (e.g. one a newer server added):
                        // render its raw value instead of dropping it.
                        : kind,
              )
              .join(", ")}
          </span>
        </span>
      ))}
    </section>
  );
}
