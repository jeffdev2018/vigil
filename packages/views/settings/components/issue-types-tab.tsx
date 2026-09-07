"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Archive, GripVertical, MoreHorizontal, Pencil, Plus, SlidersHorizontal } from "lucide-react";
import { toast } from "sonner";
import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import { issueTypeListOptions } from "@multica/core/issue-types/queries";
import {
  useArchiveIssueType,
  useCreateIssueType,
  useReorderIssueTypes,
  useUpdateIssueType,
} from "@multica/core/issue-types/mutations";
import type { IssueTypeEntry } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label as FieldLabel } from "@multica/ui/components/ui/label";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ColorPicker, COLOR_PICKER_PRESETS } from "../../common/color-picker";
import { PropertyIconGlyph, PropertyIconPicker } from "../../common/property-icon";
import { TypeGlyph } from "../../issues/components/pickers/type-picker";
import { useT } from "../../i18n";
import { SettingsTab } from "./settings-layout";

/**
 * Workspace work item type catalogue management (F30 / JEF-34).
 *
 * ONE flat list, unlike the status tab's seven category sections. A type
 * belongs to no equivalence class — there is nothing above it to group by —
 * and the four seeded types differ from a custom one in exactly one way: they
 * cannot be archived. So the built-in / custom distinction is a badge on a row,
 * not a section header, and the order is the workspace's own.
 */

interface TypeDraft {
  name: string;
  description: string;
  color: string;
  icon: string;
}

const EMPTY_DRAFT: TypeDraft = {
  name: "",
  description: "",
  color: COLOR_PICKER_PRESETS[6]!,
  icon: "",
};

export function IssueTypesTab() {
  const { t } = useT("settings");
  const wsId = useWorkspaceId();

  const [showArchived, setShowArchived] = useState(false);
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<IssueTypeEntry | null>(null);
  const [pendingArchive, setPendingArchive] = useState<IssueTypeEntry | null>(null);

  const { data: types = [], isLoading } = useQuery(issueTypeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentUser = useAuthStore((s) => s.user);
  const myRole = useMemo(() => {
    if (!currentUser) return null;
    return members.find((m) => m.user_id === currentUser.id)?.role ?? null;
  }, [members, currentUser]);
  const canManage = myRole === "owner" || myRole === "admin";

  const reorder = useReorderIssueTypes();
  // Archived rows are hidden behind a toggle rather than dropped: an admin
  // needs to see what a lingering type on an old issue is.
  const visible = useMemo(
    () => types.filter((entry) => showArchived || !entry.archived_at),
    [types, showArchived],
  );
  const archivedCount = types.filter((entry) => entry.archived_at).length;

  // Local order so the drag reads as instant even before the optimistic cache
  // write settles; resynced whenever the server list changes.
  const [order, setOrder] = useState(visible);
  useEffect(() => setOrder(visible), [visible]);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
  );
  const sortableIds = order.filter((entry) => !entry.archived_at).map((entry) => entry.id);
  const canReorder = canManage && sortableIds.length > 1;

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const from = order.findIndex((entry) => entry.id === active.id);
    const to = order.findIndex((entry) => entry.id === over.id);
    if (from < 0 || to < 0) return;
    const next = arrayMove(order, from, to);
    setOrder(next);
    // ACTIVE rows only. With "show archived" on, `order` also holds archived
    // rows, and the server refuses a payload naming one — archived types are
    // frozen, so their absence from the payload is also what the user sees.
    reorder.mutate(
      next.filter((entry) => !entry.archived_at),
      {
        onError: (error) => {
          setOrder(visible);
          toast.error(
            error instanceof Error ? error.message : t(($) => $.issue_types.reorder_failed),
          );
        },
      },
    );
  };

  return (
    <SettingsTab
      title={t(($) => $.issue_types.title)}
      description={t(($) => $.issue_types.description)}
    >
      <div className="space-y-4">
        <div className="flex items-center justify-end gap-4">
          {/* Offered only once the workspace has something archived: a
              permanently disabled "Show archived (0)" is a control that can
              never do anything. */}
          {archivedCount > 0 && (
            <label className="flex items-center gap-2 text-caption text-muted-foreground">
              {t(($) => $.issue_types.show_archived, { count: archivedCount })}
              <Switch checked={showArchived} onCheckedChange={setShowArchived} />
            </label>
          )}
          {canManage && (
            <Button className="gap-2" onClick={() => setCreateOpen(true)}>
              <Plus className="size-4" />
              {t(($) => $.issue_types.add)}
            </Button>
          )}
        </div>

        {isLoading ? (
          <div className="rounded-lg border border-surface-border bg-card px-4 py-12 text-center text-body text-muted-foreground">
            {t(($) => $.issue_types.loading)}
          </div>
        ) : order.length === 0 ? (
          <div className="rounded-lg border border-surface-border bg-card px-4 py-12 text-center text-body text-muted-foreground">
            {t(($) => $.issue_types.empty)}
          </div>
        ) : (
          <div className="overflow-hidden rounded-lg border border-surface-border bg-card">
            <DndContext sensors={sensors} collisionDetection={closestCenter} onDragEnd={handleDragEnd}>
              <SortableContext items={sortableIds} strategy={verticalListSortingStrategy}>
                <div className="divide-y divide-surface-border">
                  {order.map((entry) => (
                    <TypeRow
                      key={entry.id}
                      entry={entry}
                      canManage={canManage}
                      canReorder={canReorder && !entry.archived_at}
                      onEdit={() => setEditing(entry)}
                      onArchive={() => setPendingArchive(entry)}
                    />
                  ))}
                </div>
              </SortableContext>
            </DndContext>
          </div>
        )}
      </div>

      <TypeEditorDialog open={createOpen} onOpenChange={setCreateOpen} />
      <TypeEditorDialog
        open={Boolean(editing)}
        onOpenChange={(open) => !open && setEditing(null)}
        entry={editing}
      />
      <ArchiveTypeDialog entry={pendingArchive} onClose={() => setPendingArchive(null)} />
    </SettingsTab>
  );
}

function TypeRow({
  entry,
  canManage,
  canReorder,
  onEdit,
  onArchive,
}: {
  entry: IssueTypeEntry;
  canManage: boolean;
  canReorder: boolean;
  onEdit: () => void;
  onArchive: () => void;
}) {
  const { t } = useT("settings");
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({
    id: entry.id,
    disabled: !canReorder,
  });
  const archived = Boolean(entry.archived_at);

  return (
    <div
      ref={setNodeRef}
      style={{ transform: CSS.Transform.toString(transform), transition }}
      className={`group/row relative flex min-h-12 items-center gap-3 bg-card px-4 py-2 ${isDragging ? "z-10 shadow-[var(--surface-shadow)]" : ""} ${archived ? "opacity-60" : ""}`}
    >
      {/* The handle rides inside the row's own left padding rather than taking
          a gutter of its own, mirroring the status list. */}
      {canReorder && (
        <button
          type="button"
          aria-label={t(($) => $.issue_types.actions.reorder, { name: entry.name })}
          className="absolute left-0 top-1/2 flex w-4 -translate-y-1/2 cursor-grab justify-center text-faint-foreground opacity-0 transition-opacity group-hover/row:opacity-100 focus-visible:opacity-100 active:cursor-grabbing"
          {...attributes}
          {...listeners}
        >
          <GripVertical className="size-4" />
        </button>
      )}
      <TypeGlyph icon={entry.icon} color={entry.color} className="size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="truncate text-body font-medium">{entry.name}</span>
          {entry.is_system && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="shrink-0 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground">
                    {t(($) => $.issue_types.system_badge)}
                  </span>
                }
              />
              <TooltipContent>{t(($) => $.issue_types.system_hint)}</TooltipContent>
            </Tooltip>
          )}
          {archived && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <span className="shrink-0 rounded-full bg-muted/60 px-1.5 py-0.5 text-micro text-muted-foreground">
                    {t(($) => $.issue_types.archived_badge)}
                  </span>
                }
              />
              <TooltipContent>{t(($) => $.issue_types.archived_hint)}</TooltipContent>
            </Tooltip>
          )}
        </div>
        {entry.description && (
          <p className="truncate text-caption text-muted-foreground">{entry.description}</p>
        )}
      </div>
      {canManage && !archived && (
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="ghost"
                size="icon-sm"
                aria-label={t(($) => $.issue_types.actions.open, { name: entry.name })}
              >
                <MoreHorizontal className="size-4" />
              </Button>
            }
          />
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onEdit}>
              <Pencil className="size-4" />
              {t(($) => $.issue_types.actions.edit)}
            </DropdownMenuItem>
            {/* Archive is absent for a built-in rather than present-and-refused:
                the server answers 409, and offering an action that can only
                fail is worse than not offering it. */}
            {!entry.is_system && (
              <DropdownMenuItem variant="destructive" onClick={onArchive}>
                <Archive className="size-4" />
                {t(($) => $.issue_types.actions.archive)}
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </div>
  );
}

function TypeEditorDialog({
  open,
  onOpenChange,
  entry,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  entry?: IssueTypeEntry | null;
}) {
  const { t } = useT("settings");
  const create = useCreateIssueType();
  const update = useUpdateIssueType();
  const [draft, setDraft] = useState<TypeDraft>(EMPTY_DRAFT);
  const [iconPickerOpen, setIconPickerOpen] = useState(false);

  useEffect(() => {
    if (!open) return;
    setIconPickerOpen(false);
    setDraft(
      entry
        ? {
            name: entry.name,
            description: entry.description ?? "",
            color: entry.color,
            icon: entry.icon ?? "",
          }
        : EMPTY_DRAFT,
    );
  }, [entry, open]);

  const submit = () => {
    const name = draft.name.trim();
    if (!name) return;
    const onError = (error: unknown) =>
      toast.error(
        error instanceof Error ? error.message : t(($) => $.issue_types.editor.save_failed),
      );

    if (entry) {
      update.mutate(
        {
          id: entry.id,
          name,
          description: draft.description.trim(),
          color: draft.color,
          icon: draft.icon,
        },
        { onSuccess: () => onOpenChange(false), onError },
      );
      return;
    }
    create.mutate(
      {
        name,
        description: draft.description.trim(),
        color: draft.color,
        icon: draft.icon,
      },
      {
        onSuccess: (created) => {
          onOpenChange(false);
          // The key is derived server-side and is the only handle the API
          // accepts, so creation has to say what it minted — a name with no
          // ASCII to slug gets one that cannot be guessed back from it.
          if (created?.key) {
            toast.success(t(($) => $.issue_types.editor.created, { key: created.key }));
          }
        },
        onError,
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>
            {entry
              ? t(($) => $.issue_types.editor.edit_title)
              : t(($) => $.issue_types.editor.create_title)}
          </DialogTitle>
          <DialogDescription>{t(($) => $.issue_types.editor.hint)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-5 py-2">
          <div className="grid grid-cols-[4.25rem_minmax(0,1fr)] gap-3">
            <div className="space-y-2">
              <FieldLabel>{t(($) => $.issue_types.editor.icon)}</FieldLabel>
              <Popover open={iconPickerOpen} onOpenChange={setIconPickerOpen}>
                <PopoverTrigger
                  render={
                    <Button
                      type="button"
                      variant="outline"
                      className="w-full px-0 text-title"
                      aria-label={t(($) => $.issue_types.editor.choose_icon)}
                    >
                      {draft.icon ? (
                        <PropertyIconGlyph icon={draft.icon} />
                      ) : (
                        <SlidersHorizontal className="size-4 text-muted-foreground" />
                      )}
                    </Button>
                  }
                />
                <PopoverContent align="start" className="w-auto p-2">
                  <PropertyIconPicker
                    value={draft.icon}
                    label={t(($) => $.issue_types.editor.choose_icon)}
                    removeLabel={t(($) => $.issue_types.editor.remove_icon)}
                    onSelect={(icon) => {
                      setDraft((current) => ({ ...current, icon }));
                      setIconPickerOpen(false);
                    }}
                    onRemove={() => {
                      setDraft((current) => ({ ...current, icon: "" }));
                      setIconPickerOpen(false);
                    }}
                  />
                </PopoverContent>
              </Popover>
            </div>
            <div className="space-y-2">
              <FieldLabel htmlFor="issue-type-name">
                {t(($) => $.issue_types.editor.name)}
              </FieldLabel>
              <Input
                id="issue-type-name"
                autoFocus
                maxLength={64}
                value={draft.name}
                onChange={(event) =>
                  setDraft((current) => ({ ...current, name: event.target.value }))
                }
                placeholder={t(($) => $.issue_types.editor.name_placeholder)}
              />
              {/* The key is the string the API takes, and renaming a type does
                  not move it — so it has to be readable somewhere. Here, not as
                  a chip on every row: the list is for scanning names. */}
              {entry && (
                <p className="text-caption text-muted-foreground">
                  {t(($) => $.issue_types.editor.key_hint, { key: entry.key })}
                </p>
              )}
            </div>
          </div>
          <div className="space-y-2">
            <FieldLabel htmlFor="issue-type-description">
              {t(($) => $.issue_types.editor.description)}
            </FieldLabel>
            <Textarea
              id="issue-type-description"
              rows={3}
              maxLength={256}
              value={draft.description}
              onChange={(event) =>
                setDraft((current) => ({ ...current, description: event.target.value }))
              }
              placeholder={t(($) => $.issue_types.editor.description_placeholder)}
            />
          </div>
          <div className="space-y-2">
            <FieldLabel>{t(($) => $.issue_types.editor.color)}</FieldLabel>
            <ColorPicker
              value={draft.color}
              onChange={(color) => setDraft((current) => ({ ...current, color }))}
              trigger={
                <button
                  type="button"
                  aria-label={t(($) => $.issue_types.editor.color)}
                  className="flex h-9 items-center gap-2.5 rounded-md border border-surface-border px-2.5 transition-colors hover:bg-surface-hover"
                >
                  <span className="size-5 rounded-full" style={{ backgroundColor: draft.color }} />
                  <span className="font-mono text-caption uppercase text-muted-foreground">
                    {draft.color}
                  </span>
                </button>
              }
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t(($) => $.issue_types.editor.cancel)}
          </Button>
          <Button
            onClick={submit}
            disabled={!draft.name.trim() || create.isPending || update.isPending}
          >
            {create.isPending || update.isPending
              ? t(($) => $.issue_types.editor.saving)
              : t(($) => $.issue_types.editor.save)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function ArchiveTypeDialog({
  entry,
  onClose,
}: {
  entry: IssueTypeEntry | null;
  onClose: () => void;
}) {
  const { t } = useT("settings");
  const archive = useArchiveIssueType();
  return (
    <AlertDialog open={Boolean(entry)} onOpenChange={(open) => !open && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.issue_types.archive_dialog.title)}</AlertDialogTitle>
          {/* Archiving retires a type from FUTURE assignment. Issues already on
              it keep it AND keep every property value — say so, or this reads
              like a delete that takes data with it. */}
          <AlertDialogDescription>
            {t(($) => $.issue_types.archive_dialog.description, { name: entry?.name ?? "" })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t(($) => $.issue_types.archive_dialog.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() => {
              if (!entry) return;
              archive.mutate(entry.id, {
                onSuccess: onClose,
                onError: (error) =>
                  toast.error(
                    error instanceof Error
                      ? error.message
                      : t(($) => $.issue_types.archive_dialog.failed),
                  ),
              });
            }}
          >
            {t(($) => $.issue_types.archive_dialog.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
