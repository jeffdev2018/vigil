"use client";

import { useQuery } from "@tanstack/react-query";
import { Command as CommandPrimitive } from "cmdk";
import { toast } from "sonner";
import { BrainCircuit, Plus } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { noteSearchOptions } from "@multica/core/brain/queries";
import { useCaptureText } from "@multica/core/brain/mutations";
import { snippetText } from "@multica/core/brain/snippet";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { HighlightText } from "./highlight-text";

/**
 * Brain rows in the palette: the ranked note search, plus the one action that
 * belongs next to it — capture what was just typed, without leaving the
 * palette. Self-contained (its own query, its own group) the way
 * WhySearchGroup is, so the palette's closed `SearchResults` shape stays
 * closed.
 */
export function BrainSearchGroup({
  query,
  groupClassName,
  onNavigated,
}: {
  query: string;
  groupClassName: string;
  onNavigated: () => void;
}) {
  const { t } = useT("search");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { push } = useNavigation();
  const capture = useCaptureText(wsId);
  const trimmed = query.trim();
  const { data } = useQuery(noteSearchOptions(wsId, trimmed, { limit: 5 }));

  if (trimmed === "") return null;
  const notes = data?.notes ?? [];

  return (
    <CommandPrimitive.Group
      heading={t(($) => $.groups.brain)}
      className={groupClassName}
    >
      <CommandPrimitive.Item
        value={`brain:capture:${trimmed}`}
        data-testid="brain-capture"
        onSelect={async () => {
          try {
            await capture.mutateAsync({ content: trimmed });
            toast.success(t(($) => $.brain.captured));
            onNavigated();
          } catch {
            toast.error(t(($) => $.brain.capture_failed));
          }
        }}
        className="flex cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-caption aria-selected:bg-accent"
      >
        <Plus aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        <span className="truncate">
          {t(($) => $.brain.capture, { text: trimmed })}
        </span>
      </CommandPrimitive.Item>

      {notes.map((note) => (
        <CommandPrimitive.Item
          key={`brain:note:${note.id}`}
          value={`brain:note:${note.id}`}
          data-testid="brain-note"
          onSelect={() => {
            push(`${paths.brain()}?note=${encodeURIComponent(note.id)}`);
            onNavigated();
          }}
          className="flex cursor-pointer items-start gap-2 rounded-md px-3 py-2 text-caption aria-selected:bg-accent"
        >
          <BrainCircuit
            aria-hidden="true"
            className="mt-0.5 size-4 shrink-0 text-muted-foreground"
          />
          <span className="flex min-w-0 flex-1 flex-col gap-0.5">
            <span className="truncate">
              <HighlightText text={note.title} query={trimmed} />
            </span>
            {note.snippet || note.passage_heading ? (
              // The marks are dropped here: the palette highlights the query
              // itself, and the raw snippet must never reach innerHTML. The
              // section it comes from goes first: `Section · excerpt`.
              <span className="line-clamp-1 text-muted-foreground">
                {[note.passage_heading, snippetText(note.snippet)]
                  .filter((part) => part !== "")
                  .join(" · ")}
              </span>
            ) : null}
          </span>
        </CommandPrimitive.Item>
      ))}
    </CommandPrimitive.Group>
  );
}
