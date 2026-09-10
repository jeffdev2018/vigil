"use client";

import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { Loader2 } from "lucide-react";
import { ApiError } from "@multica/core/api";
import type { BrainCapture } from "@multica/core/types";
import { noteSearchOptions } from "@multica/core/brain/queries";
import { useOrganizeCapture } from "@multica/core/brain/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { useDebouncedValue } from "../../common/use-debounced-value";

/** Comma-separated tag input → the array the API takes. Same as the editor. */
function parseTags(raw: string): string[] {
  return raw
    .split(",")
    .map((tag) => tag.trim())
    .filter((tag) => tag !== "");
}

export type OrganizeAction = "note" | "merge";

/**
 * Turn one capture into a note, or append it to an existing one. Never
 * optimistic: the server writes (or extends) the note and decides the
 * resulting title/body, and the dialog stays open until it answers — a
 * capture removed from the inbox before the write landed would be a lie.
 */
/**
 * The title the server derives when the field is left empty: the capture's
 * first line, cut at a word boundary past 80 characters (mirrors
 * `firstLine` in server/internal/handler/brain_capture.go).
 */
export function derivedTitle(content: string): string {
  const line = (content.trim().split("\n")[0] ?? "").trim();
  const runes = Array.from(line);
  if (runes.length <= 80) return line;
  let cut = runes.slice(0, 80).join("");
  const space = cut.search(/[ \t][^ \t]*$/);
  if (space > 40) cut = cut.slice(0, space);
  return cut.replace(/[ \t,;:.-]+$/, "");
}

export function OrganizeDialog({
  wsId,
  capture,
  action,
  onClose,
}: {
  wsId: string;
  capture: BrainCapture;
  action: OrganizeAction;
  onClose: () => void;
}) {
  const { t } = useT("brain");
  const organize = useOrganizeCapture(wsId);
  const suggestion = capture.suggestion ?? null;

  const [title, setTitle] = useState(
    suggestion?.title || capture.title_hint || "",
  );
  const [tagsRaw, setTagsRaw] = useState((suggestion?.tags ?? []).join(", "));
  const [content, setContent] = useState("");
  const [pinned, setPinned] = useState(false);
  const [target, setTarget] = useState<{ id: string; title: string } | null>(
    suggestion?.merge_note ?? null,
  );
  const [query, setQuery] = useState("");
  const [error, setError] = useState("");

  const debounced = useDebouncedValue(query, 250);
  const search = useQuery({
    ...noteSearchOptions(wsId, debounced, { limit: 8 }),
    enabled: action === "merge" && debounced.trim() !== "",
  });

  // The suggestion's own candidates are the zero-typing shortlist.
  const candidates = useMemo(() => {
    if (debounced.trim() !== "") {
      return (search.data?.notes ?? []).map((note) => ({
        id: note.id,
        title: note.title,
      }));
    }
    const seen = new Set<string>();
    const list = [
      ...(suggestion?.merge_note ? [suggestion.merge_note] : []),
      ...(suggestion?.candidates ?? []),
    ];
    return list.filter((note) => {
      if (note.id === "" || seen.has(note.id)) return false;
      seen.add(note.id);
      return true;
    });
  }, [debounced, search.data, suggestion]);

  const submit = useCallback(async () => {
    setError("");
    try {
      await organize.mutateAsync({
        id: capture.id,
        input:
          action === "merge"
            ? {
                action: "merge",
                note_id: target?.id ?? "",
                tags: parseTags(tagsRaw),
                content: content.trim() || undefined,
              }
            : {
                action: "note",
                title: title.trim(),
                tags: parseTags(tagsRaw),
                content: content.trim() || undefined,
                pinned,
              },
      });
      toast.success(
        t(($) => (action === "merge" ? $.organize.merged_toast : $.organize.saved_toast)),
      );
      onClose();
    } catch (err) {
      // 409 means someone (or a run) sorted this capture first. That is not a
      // failure the user caused, and it must not read like one.
      setError(
        err instanceof ApiError && err.status === 409
          ? t(($) => $.organize.already_organized)
          : t(($) => $.organize.error),
      );
    }
  }, [action, capture.id, content, onClose, organize, pinned, t, tagsRaw, target, title]);

  // The server derives a missing title the same way: the title hint, then
  // the capture's first line. Show that default so an empty field is a
  // choice, not a blocker.
  const defaultTitle = capture.title_hint.trim() || derivedTitle(capture.content ?? "");
  const canSubmit =
    action === "merge" ? Boolean(target?.id) : title.trim() !== "" || defaultTitle !== "";

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {t(($) => (action === "merge" ? $.organize.merge_title : $.organize.note_title))}
          </DialogTitle>
          <DialogDescription>
            {t(($) =>
              action === "merge" ? $.organize.merge_description : $.organize.note_description,
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 py-2">
          {action === "note" ? (
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              aria-label={t(($) => $.detail.title_label)}
              placeholder={defaultTitle || t(($) => $.create.title_placeholder)}
            />
          ) : (
            <div className="flex flex-col gap-2">
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                aria-label={t(($) => $.organize.target_label)}
                placeholder={t(($) => $.organize.target_search_placeholder)}
              />
              <ul className="flex max-h-40 flex-col gap-1 overflow-y-auto">
                {candidates.map((note) => {
                  const isActive = note.id === target?.id;
                  return (
                    <li key={note.id}>
                      <button
                        type="button"
                        aria-pressed={isActive}
                        onClick={() => setTarget(note)}
                        className={cn(
                          "w-full rounded-md px-2 py-1 text-left text-caption transition-colors",
                          isActive
                            ? "bg-accent font-medium text-foreground"
                            : "text-muted-foreground hover:bg-accent",
                        )}
                      >
                        {note.title}
                      </button>
                    </li>
                  );
                })}
              </ul>
              {candidates.length === 0 ? (
                <p className="text-caption text-muted-foreground">
                  {search.isFetching
                    ? t(($) => $.search.loading)
                    : debounced.trim() === ""
                      ? t(($) => $.organize.target_none)
                      : t(($) => $.organize.target_empty)}
                </p>
              ) : null}
              {target ? (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.organize.append_preview, { title: target.title })}
                </p>
              ) : null}
            </div>
          )}

          <Input
            value={tagsRaw}
            onChange={(e) => setTagsRaw(e.target.value)}
            aria-label={t(($) => $.detail.tags_label)}
            placeholder={t(($) => $.detail.tags_hint)}
          />

          <Textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            aria-label={t(($) => $.detail.content_label)}
            placeholder={t(($) => $.organize.content_default)}
            className="min-h-32 resize-none font-mono text-body leading-relaxed"
          />

          {action === "note" ? (
            <label className="flex items-center gap-1.5 text-caption text-muted-foreground">
              <input
                type="checkbox"
                checked={pinned}
                onChange={(e) => setPinned(e.target.checked)}
                className="size-3.5 accent-primary"
              />
              {t(($) => $.organize.pinned)}
            </label>
          ) : null}

          {error ? (
            <p role="alert" className="text-caption text-destructive">
              {error}
            </p>
          ) : null}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={organize.isPending}>
            {t(($) => $.detail.cancel)}
          </Button>
          <Button onClick={() => void submit()} disabled={organize.isPending || !canSubmit}>
            {organize.isPending ? (
              <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
            ) : null}
            {organize.isPending
              ? t(($) => $.organize.submitting)
              : t(($) =>
                  action === "merge" ? $.organize.submit_merge : $.organize.submit_note,
                )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
