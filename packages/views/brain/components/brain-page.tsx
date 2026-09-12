"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  Archive,
  ArchiveRestore,
  Bot,
  BrainCircuit,
  Inbox,
  Loader2,
  Pin,
  PinOff,
  Plus,
  Search,
  Sparkles,
  Trash2,
  User,
  X,
} from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { ApiError } from "@multica/core/api";
import type { WorkspaceNote, WorkspaceNoteSearchHit } from "@multica/core/types";
import {
  brainNotesOptions,
  noteSearchOptions,
  useBrainRawCount,
} from "@multica/core/brain/queries";
import { renderSnippet } from "@multica/core/brain/snippet";
import {
  useCreateWorkspaceNote,
  useDeleteWorkspaceNote,
  useSetWorkspaceNoteArchived,
  useUpdateWorkspaceNote,
} from "@multica/core/brain/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Badge } from "@multica/ui/components/ui/badge";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { cn } from "@multica/ui/lib/utils";
import { AppLink, useNavigation } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import { useDebouncedValue } from "../../common/use-debounced-value";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";
import { RichContent } from "../../rich-content";
import { CaptureInbox } from "./capture-inbox";

type BrainT = ReturnType<typeof useT<"brain">>["t"];

/** Comma-separated tag input → the array the API takes. */
function parseTags(raw: string): string[] {
  return raw
    .split(",")
    .map((tag) => tag.trim())
    .filter((tag) => tag !== "");
}

/**
 * The Brain has two halves of the same loop: capture first (the inbox), sort
 * later (the notes). They are tabs rather than panes because the phone-sized
 * end of the responsive range cannot carry three columns, and because sorting
 * is a different sitting from capturing.
 */
export function BrainPage() {
  const wsId = useWorkspaceId();
  const { t } = useT("brain");
  const { searchParams } = useNavigation();

  const [tab, setTab] = useState<"inbox" | "notes">("inbox");
  const [search, setSearch] = useState("");
  const [tag, setTag] = useState("");
  const [archived, setArchived] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  // A capture card and the command palette both link here with ?note=<id>.
  const linkedNoteId = searchParams.get("note");
  useEffect(() => {
    if (!linkedNoteId) return;
    setTab("notes");
    setSelectedId(linkedNoteId);
  }, [linkedNoteId]);

  const rawCount = useBrainRawCount(wsId);
  const notesQuery = useQuery(brainNotesOptions(wsId, { search: "", tag, archived }));

  // A non-empty query goes to the ranked endpoint (lexical + vector, fused),
  // which returns hits with a snippet. An empty one is the plain list.
  const debouncedSearch = useDebouncedValue(search.trim(), 250);
  const searchQuery = useQuery(
    noteSearchOptions(wsId, debouncedSearch, { tag, archived }),
  );
  const searching = debouncedSearch !== "";

  const items = useMemo<WorkspaceNote[]>(
    () =>
      searching ? (searchQuery.data?.notes ?? []) : (notesQuery.data?.items ?? []),
    [notesQuery.data, searchQuery.data, searching],
  );
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
              setTab("notes");
              setCreating(true);
              setSelectedId(null);
            }}
          />
        }
      />

      <div className="flex shrink-0 items-center gap-1 border-b px-4 py-1.5">
        <TabButton
          label={t(($) => $.tabs.inbox)}
          icon={Inbox}
          active={tab === "inbox"}
          badge={rawCount.data ?? 0}
          badgeLabel={t(($) => $.tabs.inbox_badge, { count: rawCount.data ?? 0 })}
          onClick={() => setTab("inbox")}
        />
        <TabButton
          label={t(($) => $.tabs.notes)}
          icon={BrainCircuit}
          active={tab === "notes"}
          onClick={() => setTab("notes")}
        />
      </div>

      {tab === "inbox" ? (
        <CaptureInbox wsId={wsId} />
      ) : (
        <>
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
            {searching && searchQuery.data?.vector === true ? (
              <Badge
                variant="secondary"
                className="text-micro"
                title={t(($) => $.search.semantic_hint)}
              >
                {t(($) => $.search.semantic)}
              </Badge>
            ) : null}
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
              <Checkbox
                checked={archived}
                onCheckedChange={(checked) => setArchived(checked === true)}
              />
              {t(($) => $.filter.show_archived)}
            </label>
          </div>

          <div className="flex min-h-0 flex-1">
            <NoteList
              items={items}
              hits={searching ? (searchQuery.data?.notes ?? []) : undefined}
              isLoading={searching ? searchQuery.isLoading : notesQuery.isLoading}
              isError={searching ? searchQuery.isError : notesQuery.isError}
              isFiltered={searching || tag !== ""}
              selectedId={selectedId}
              onSelect={(id) => {
                setSelectedId(id);
                setCreating(false);
              }}
              onCreate={() => {
                setCreating(true);
                setSelectedId(null);
              }}
            />
            {creating ? (
              <NoteCreate wsId={wsId} onDone={() => setCreating(false)} />
            ) : (
              <NoteDetail note={selected} wsId={wsId} onDeleted={() => setSelectedId(null)} />
            )}
          </div>
        </>
      )}
    </div>
  );
}

function TabButton({
  label,
  icon: Icon,
  active,
  badge,
  badgeLabel,
  onClick,
}: {
  label: string;
  icon: typeof Inbox;
  active: boolean;
  badge?: number;
  badgeLabel?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2 py-1 text-caption transition-colors",
        // Same rule as the chips: the active tab keeps its weight and text
        // colour under hover, which only moves the background.
        active
          ? "bg-accent font-medium text-foreground"
          : "text-muted-foreground hover:bg-accent",
      )}
    >
      <Icon aria-hidden="true" className="size-3.5" />
      {label}
      {typeof badge === "number" && badge > 0 ? (
        <span
          aria-label={badgeLabel}
          className="rounded-full bg-primary px-1.5 font-mono text-micro tabular-nums text-primary-foreground"
        >
          {badge}
        </span>
      ) : null}
    </button>
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
  hits,
  isLoading,
  isError,
  isFiltered,
  selectedId,
  onSelect,
  onCreate,
}: {
  items: WorkspaceNote[];
  /** Present only in search mode: the same notes, with their snippet. */
  hits?: WorkspaceNoteSearchHit[];
  isLoading: boolean;
  isError: boolean;
  isFiltered: boolean;
  selectedId: string | null;
  onSelect: (id: string) => void;
  onCreate: () => void;
}) {
  const { t } = useT("brain");
  const timeAgo = useTimeAgo();
  const snippets = useMemo(
    () => new Map((hits ?? []).map((hit) => [hit.id, hit])),
    [hits],
  );

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
          actions={
            isFiltered ? undefined : (
              <Button size="sm" onClick={onCreate}>
                <Plus aria-hidden="true" className="size-3.5" />
                {t(($) => $.create.new)}
              </Button>
            )
          }
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
              <Snippet
                snippet={snippets.get(note.id)?.snippet ?? ""}
                heading={snippets.get(note.id)?.passage_heading ?? ""}
              />
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
 * A ranked-search snippet, after the heading of the section it comes from
 * (`Section · excerpt`). The server hands us the note's own text with
 * `<mark>` inserted and nothing escaped, so `renderSnippet` escapes the whole
 * string and revives only those two markers — a note containing `<script>`
 * renders as the characters the author typed. The heading is plain text. See
 * packages/core/brain/snippet.test.ts for the matrix.
 */
function Snippet({ snippet, heading }: { snippet: string; heading: string }) {
  if (snippet === "" && heading === "") return null;
  return (
    <span className="line-clamp-2 text-caption text-muted-foreground">
      {heading !== "" ? (
        <span className="text-foreground">
          {heading}
          {snippet !== "" ? " · " : ""}
        </span>
      ) : null}
      <span
        className="[&_mark]:bg-warning/30 [&_mark]:text-foreground"
        dangerouslySetInnerHTML={{ __html: renderSnippet(snippet) }}
      />
    </span>
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

function NoteDetail({
  note,
  wsId,
  onDeleted,
}: {
  note: WorkspaceNote | null;
  wsId: string;
  onDeleted: () => void;
}) {
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
  return <NoteDetailBody key={note.id} note={note} wsId={wsId} onDeleted={onDeleted} />;
}

function NoteDetailBody({
  note,
  wsId,
  onDeleted,
}: {
  note: WorkspaceNote;
  wsId: string;
  onDeleted: () => void;
}) {
  const { t } = useT("brain");
  const update = useUpdateWorkspaceNote(wsId);
  const setArchived = useSetWorkspaceNoteArchived(wsId);
  const remove = useDeleteWorkspaceNote(wsId);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const [editing, setEditing] = useState(false);
  // The revision the draft was opened on. `note.revision` keeps moving while
  // the editor is open (a realtime update refetches the note), and sending
  // the moved value made the server accept a save built on stale fields — a
  // concurrent edit was silently overwritten instead of answering 409.
  const [editRevision, setEditRevision] = useState(note.revision);
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
          revision: editRevision,
        },
      });
      toast.success(t(($) => $.detail.saved_toast));
      setEditing(false);
    } catch (err) {
      handleWriteError(err, t);
    }
  }, [content, editRevision, note.id, t, tagsRaw, title, update]);

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

  // The server narrows who may delete (a workspace admin, or the note's
  // author) and says so in the 403 body — surface that text rather than a
  // generic failure, since the user cannot tell from the UI which rule bit.
  const handleDelete = useCallback(async () => {
    setConfirmDelete(false);
    try {
      await remove.mutateAsync(note.id);
      toast.success(t(($) => $.note_delete.deleted_toast));
      onDeleted();
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) {
        toast.error(err.message || t(($) => $.note_delete.error));
        return;
      }
      toast.error(t(($) => $.note_delete.error));
    }
  }, [note.id, onDeleted, remove, t]);

  const busy = update.isPending || setArchived.isPending || remove.isPending;

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
              <Button variant="outline" size="sm" onClick={() => {
                  setEditRevision(note.revision);
                  setEditing(true);
                }}>
                {t(($) => $.detail.edit)}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="text-destructive"
                disabled={busy}
                onClick={() => setConfirmDelete(true)}
                aria-label={t(($) => $.note_delete.action)}
              >
                <Trash2 aria-hidden="true" className="size-3.5" />
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

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.note_delete.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.note_delete.description, { title: note.title })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.note_delete.cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void handleDelete()}>
              {t(($) => $.note_delete.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
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
