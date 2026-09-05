"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Archive,
  ArchiveRestore,
  Bot,
  BrainCircuit,
  Loader2,
  Pin,
  PinOff,
  Plus,
  Search,
  Sparkles,
  User,
  X,
} from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { ApiError } from "@multica/core/api";
import type { WorkspaceNote } from "@multica/core/types";
import { brainNotesOptions } from "@multica/core/brain/queries";
import {
  useCreateWorkspaceNote,
  useSetWorkspaceNoteArchived,
  useUpdateWorkspaceNote,
} from "@multica/core/brain/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";
import { RichContent } from "../../rich-content";

type BrainT = ReturnType<typeof useT<"brain">>["t"];

/** Comma-separated tag input → the array the API takes. */
function parseTags(raw: string): string[] {
  return raw
    .split(",")
    .map((tag) => tag.trim())
    .filter((tag) => tag !== "");
}

export function BrainPage() {
  const wsId = useWorkspaceId();
  const { t } = useT("brain");

  const [search, setSearch] = useState("");
  const [tag, setTag] = useState("");
  const [archived, setArchived] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const notesQuery = useQuery(brainNotesOptions(wsId, { search, tag, archived }));
  const items = useMemo(() => notesQuery.data?.items ?? [], [notesQuery.data]);
  const tags = notesQuery.data?.tags ?? [];
  const selected = useMemo(
    () => items.find((item) => item.id === selectedId) ?? null,
    [items, selectedId],
  );

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={BrainCircuit}
        title={t(($) => $.title)}
        count={items.length}
        description={t(($) => $.subtitle)}
        actions={
          <CollectionPageHeaderAction
            icon={Plus}
            label={t(($) => $.create.new)}
            variant="default"
            onClick={() => {
              setCreating(true);
              setSelectedId(null);
            }}
          />
        }
      />

      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-4 py-2">
        <div className="relative min-w-0 flex-1 md:max-w-xs">
          <Search
            aria-hidden="true"
            className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground"
          />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t(($) => $.search_placeholder)}
            aria-label={t(($) => $.search_placeholder)}
            className="h-8 pl-7 text-caption"
          />
        </div>
        <div className="flex min-w-0 flex-wrap items-center gap-1">
          <TagChip
            label={t(($) => $.filter.all_tags)}
            active={tag === ""}
            onClick={() => setTag("")}
          />
          {tags.map((candidate) => (
            <TagChip
              key={candidate}
              label={candidate}
              active={tag === candidate}
              onClick={() => setTag(tag === candidate ? "" : candidate)}
            />
          ))}
        </div>
        <label className="ml-auto flex shrink-0 items-center gap-1.5 text-caption text-muted-foreground">
          <input
            type="checkbox"
            checked={archived}
            onChange={(e) => setArchived(e.target.checked)}
            className="size-3.5 accent-primary"
          />
          {t(($) => $.filter.show_archived)}
        </label>
      </div>

      <div className="flex min-h-0 flex-1">
        <NoteList
          items={items}
          isLoading={notesQuery.isLoading}
          isError={notesQuery.isError}
          isFiltered={search !== "" || tag !== ""}
          selectedId={selectedId}
          onSelect={(id) => {
            setSelectedId(id);
            setCreating(false);
          }}
        />
        {creating ? (
          <NoteCreate wsId={wsId} onDone={() => setCreating(false)} />
        ) : (
          <NoteDetail note={selected} wsId={wsId} />
        )}
      </div>
    </div>
  );
}

function TagChip({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={cn(
        "rounded-md px-2 py-0.5 text-caption transition-colors",
        // The active chip stays identifiable while hovered: hover only moves
        // the background, so weight and text color carry the selection.
        active
          ? "bg-accent font-medium text-foreground"
          : "text-muted-foreground hover:bg-accent/60",
      )}
    >
      {label}
    </button>
  );
}

function NoteList({
  items,
  isLoading,
  isError,
  isFiltered,
  selectedId,
  onSelect,
}: {
  items: WorkspaceNote[];
  isLoading: boolean;
  isError: boolean;
  isFiltered: boolean;
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const { t } = useT("brain");
  const timeAgo = useTimeAgo();

  if (isLoading) {
    return (
      <div className="flex w-full flex-col gap-2 p-4" aria-busy="true">
        <span className="sr-only">{t(($) => $.list.loading)}</span>
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} className="h-14 w-full rounded-lg" />
        ))}
      </div>
    );
  }

  if (isError) {
    return (
      <div className="flex w-full flex-1 items-center justify-center p-4">
        <CollectionPageState
          icon={BrainCircuit}
          title={t(($) => $.list.load_error)}
          tone="destructive"
          role="alert"
        />
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="flex w-full flex-1 items-center justify-center p-4">
        <CollectionPageState
          icon={BrainCircuit}
          title={t(($) => (isFiltered ? $.list.empty_search_title : $.list.empty_title))}
          description={t(($) =>
            isFiltered ? $.list.empty_search_description : $.list.empty_description,
          )}
        />
      </div>
    );
  }

  return (
    <ul className="flex w-full min-w-0 flex-1 flex-col gap-1 overflow-y-auto p-2 lg:max-w-sm">
      {items.map((note) => {
        const isActive = note.id === selectedId;
        return (
          <li key={note.id}>
            <div
              role="button"
              tabIndex={0}
              onClick={() => onSelect(note.id)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onSelect(note.id);
                }
              }}
              className={cn(
                "flex w-full cursor-pointer flex-col gap-0.5 rounded-lg border px-2 py-2 text-left transition-colors",
                isActive
                  ? "border-primary/40 bg-accent"
                  : "border-transparent hover:bg-accent/60",
              )}
            >
              <span className="flex min-w-0 items-center gap-1.5">
                {note.pinned === true ? (
                  <Pin
                    aria-label={t(($) => $.list.pinned)}
                    className="size-3 shrink-0 text-muted-foreground"
                  />
                ) : null}
                <span
                  className={cn(
                    "line-clamp-2 text-body",
                    isActive ? "font-medium text-foreground" : undefined,
                  )}
                >
                  {note.title}
                </span>
              </span>
              <span className="flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
                <SourceBadge note={note} />
                {(note.tags ?? []).map((noteTag) => (
                  <Badge key={noteTag} variant="outline" className="text-micro">
                    {noteTag}
                  </Badge>
                ))}
                {note.archived_at ? (
                  <Badge variant="secondary" className="text-micro">
                    {t(($) => $.list.archived)}
                  </Badge>
                ) : null}
                <span className="shrink-0">{timeAgo(note.updated_at)}</span>
              </span>
            </div>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * Where a note came from. `agent` also links to the agent that saved it, so a
 * surprising note is one click from the thing that wrote it.
 */
function SourceBadge({ note }: { note: WorkspaceNote }) {
  const { t } = useT("brain");
  const slug = useWorkspaceSlug();

  // Server-driven enum: an unknown source must not blank the badge.
  const label =
    note.source === "agent"
      ? t(($) => $.source.agent)
      : note.source === "curation"
        ? t(($) => $.source.curation)
        : t(($) => $.source.manual);
  const Icon =
    note.source === "agent" ? Bot : note.source === "curation" ? Sparkles : User;

  const badge = (
    <Badge variant="secondary" className="gap-1 text-micro">
      <Icon aria-hidden="true" className="size-3" />
      {label}
    </Badge>
  );

  if (note.source !== "agent" || !note.source_agent_id || !slug) return badge;
  return (
    <AppLink
      href={paths.workspace(slug).agentDetail(note.source_agent_id)}
      onClick={(e) => e.stopPropagation()}
      aria-label={t(($) => $.source.open_run)}
    >
      {badge}
    </AppLink>
  );
}

function NoteDetail({ note, wsId }: { note: WorkspaceNote | null; wsId: string }) {
  const { t } = useT("brain");
  if (!note) {
    return (
      <aside className="hidden min-w-0 flex-1 border-l lg:flex lg:items-center lg:justify-center">
        <p className="text-caption text-muted-foreground">
          {t(($) => $.detail.select_prompt)}
        </p>
      </aside>
    );
  }
  return <NoteDetailBody key={note.id} note={note} wsId={wsId} />;
}

function NoteDetailBody({ note, wsId }: { note: WorkspaceNote; wsId: string }) {
  const { t } = useT("brain");
  const update = useUpdateWorkspaceNote(wsId);
  const setArchived = useSetWorkspaceNoteArchived(wsId);

  const [editing, setEditing] = useState(false);
  const [title, setTitle] = useState(note.title);
  const [tagsRaw, setTagsRaw] = useState((note.tags ?? []).join(", "));
  const [content, setContent] = useState(note.content);

  // A realtime update (or a curation pass) can rewrite the note under an open
  // reader. Re-seed the draft while it is NOT being edited, so the pane stays
  // current without ever discarding typing.
  useEffect(() => {
    if (editing) return;
    setTitle(note.title);
    setTagsRaw((note.tags ?? []).join(", "));
    setContent(note.content);
  }, [editing, note.title, note.tags, note.content]);

  const handleSave = useCallback(async () => {
    try {
      await update.mutateAsync({
        id: note.id,
        input: {
          title,
          content,
          tags: parseTags(tagsRaw),
          revision: note.revision,
        },
      });
      toast.success(t(($) => $.detail.saved_toast));
      setEditing(false);
    } catch (err) {
      handleWriteError(err, t);
    }
  }, [content, note.id, note.revision, t, tagsRaw, title, update]);

  const handleTogglePin = useCallback(async () => {
    try {
      await update.mutateAsync({
        id: note.id,
        input: { pinned: note.pinned !== true, revision: note.revision },
      });
    } catch (err) {
      handleWriteError(err, t);
    }
  }, [note.id, note.pinned, note.revision, t, update]);

  const isArchived = Boolean(note.archived_at);
  const handleToggleArchive = useCallback(async () => {
    try {
      await setArchived.mutateAsync({ id: note.id, archived: !isArchived });
      toast.success(
        t(($) => (isArchived ? $.detail.unarchived_toast : $.detail.archived_toast)),
      );
    } catch (err) {
      handleWriteError(err, t);
    }
  }, [isArchived, note.id, setArchived, t]);

  const busy = update.isPending || setArchived.isPending;

  return (
    <aside className="flex min-w-0 flex-1 flex-col border-l">
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-b px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <SourceBadge note={note} />
          {note.merged_into ? (
            <span className="truncate text-caption text-muted-foreground">
              {t(($) => $.detail.merged_into)}
            </span>
          ) : null}
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {editing ? (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEditing(false)}
                disabled={busy}
              >
                <X aria-hidden="true" className="size-3.5" />
                {t(($) => $.detail.cancel)}
              </Button>
              <Button size="sm" onClick={handleSave} disabled={busy}>
                {update.isPending ? (
                  <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
                ) : null}
                {update.isPending ? t(($) => $.detail.saving) : t(($) => $.detail.save)}
              </Button>
            </>
          ) : (
            <>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleTogglePin}
                disabled={busy}
                aria-label={t(($) => (note.pinned === true ? $.detail.unpin : $.detail.pin))}
              >
                {note.pinned === true ? (
                  <PinOff aria-hidden="true" className="size-3.5" />
                ) : (
                  <Pin aria-hidden="true" className="size-3.5" />
                )}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleToggleArchive}
                disabled={busy}
                aria-label={t(($) =>
                  isArchived ? $.detail.unarchive : $.detail.archive,
                )}
              >
                {isArchived ? (
                  <ArchiveRestore aria-hidden="true" className="size-3.5" />
                ) : (
                  <Archive aria-hidden="true" className="size-3.5" />
                )}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
                {t(($) => $.detail.edit)}
              </Button>
            </>
          )}
        </div>
      </div>

      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 py-4">
        {editing ? (
          <>
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              aria-label={t(($) => $.detail.title_label)}
            />
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
              placeholder={t(($) => $.detail.content_placeholder)}
              className="min-h-80 flex-1 resize-none font-mono text-body leading-relaxed"
            />
          </>
        ) : (
          <>
            <h2 className="text-title font-medium">{note.title}</h2>
            {note.content ? (
              <RichContent content={note.content} density="document" phase="settled" />
            ) : (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.detail.empty_content)}
              </p>
            )}
          </>
        )}
      </div>
    </aside>
  );
}

function NoteCreate({ wsId, onDone }: { wsId: string; onDone: () => void }) {
  const { t } = useT("brain");
  const create = useCreateWorkspaceNote(wsId);
  const [title, setTitle] = useState("");
  const [tagsRaw, setTagsRaw] = useState("");
  const [content, setContent] = useState("");

  const handleCreate = useCallback(async () => {
    try {
      await create.mutateAsync({ title, content, tags: parseTags(tagsRaw) });
      toast.success(t(($) => $.create.created_toast));
      onDone();
    } catch (err) {
      handleWriteError(err, t, t(($) => $.create.error_toast));
    }
  }, [content, create, onDone, t, tagsRaw, title]);

  return (
    <aside className="flex min-w-0 flex-1 flex-col border-l">
      <div className="flex shrink-0 items-center justify-between gap-2 border-b px-4 py-3">
        <h2 className="text-body font-medium">{t(($) => $.create.heading)}</h2>
        <div className="flex shrink-0 items-center gap-2">
          <Button variant="outline" size="sm" onClick={onDone} disabled={create.isPending}>
            {t(($) => $.detail.cancel)}
          </Button>
          <Button
            size="sm"
            onClick={handleCreate}
            disabled={create.isPending || title.trim() === ""}
          >
            {create.isPending ? (
              <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
            ) : null}
            {create.isPending
              ? t(($) => $.create.submitting)
              : t(($) => $.create.submit)}
          </Button>
        </div>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 py-4">
        <Input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          aria-label={t(($) => $.detail.title_label)}
          placeholder={t(($) => $.create.title_placeholder)}
        />
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
          placeholder={t(($) => $.detail.content_placeholder)}
          className="min-h-80 flex-1 resize-none font-mono text-body leading-relaxed"
        />
      </div>
    </aside>
  );
}

/**
 * A 409 is not an error the user caused: someone else (or the curation pass)
 * wrote first. Say so, and say what to do, instead of a generic failure.
 */
function handleWriteError(err: unknown, t: BrainT, fallback?: string) {
  if (err instanceof ApiError && err.status === 409) {
    toast.info(t(($) => $.detail.conflict_toast));
    return;
  }
  toast.error(fallback ?? t(($) => $.detail.error_toast));
}
