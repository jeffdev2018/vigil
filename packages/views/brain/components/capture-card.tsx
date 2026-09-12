"use client";

import { useCallback, useState } from "react";
import { toast } from "sonner";
import {
  CheckSquare,
  FileText,
  Image as ImageIcon,
  Link as LinkIcon,
  Loader2,
  Mic,
  Sparkles,
  Trash2,
  Type,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { ApiError } from "@multica/core/api";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import type { BrainCapture } from "@multica/core/types";
import {
  useDeleteCapture,
  useOrganizeCapture,
  useReopenCapture,
  useSuggestCapture,
} from "@multica/core/brain/mutations";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
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
import { AppLink } from "../../navigation";
import { useT, useTimeAgo } from "../../i18n";
import type { OrganizeAction } from "./organize-dialog";

const KIND_ICONS: Record<string, LucideIcon> = {
  text: Type,
  link: LinkIcon,
  image: ImageIcon,
  audio: Mic,
  file: FileText,
  todo: CheckSquare,
};

type BrainT = ReturnType<typeof useT<"brain">>["t"];

/** Server-driven enum: an unknown kind keeps its own key as the label. */
function kindLabel(t: BrainT, kind: string): string {
  const labels = t(($) => $.card.kind, { returnObjects: true }) as Record<
    string,
    string
  >;
  return labels[kind] ?? kind;
}

function originLabel(t: BrainT, origin: string): string {
  const labels = t(($) => $.card.origin, { returnObjects: true }) as Record<
    string,
    string
  >;
  return labels[origin] ?? origin;
}

export function CaptureCard({
  wsId,
  capture,
  noModel,
  onNoModel,
  onOrganize,
}: {
  wsId: string;
  capture: BrainCapture;
  /** The server already answered 503 once: suggestions are off, not broken. */
  noModel: boolean;
  onNoModel: () => void;
  onOrganize: (capture: BrainCapture, action: OrganizeAction) => void;
}) {
  const { t } = useT("brain");
  const timeAgo = useTimeAgo();
  const slug = useWorkspaceSlug();

  const organize = useOrganizeCapture(wsId);
  const reopen = useReopenCapture(wsId);
  const suggest = useSuggestCapture(wsId);
  const remove = useDeleteCapture(wsId);
  const [error, setError] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);

  const Icon = KIND_ICONS[capture.kind] ?? FileText;
  const attachment = capture.attachment ?? null;
  const suggestion = capture.suggestion ?? null;

  const run = useCallback(
    async (work: Promise<unknown>, successKey: "discarded" | "reopened" | "deleted") => {
      setError("");
      try {
        await work;
        toast.success(
          t(($) =>
            successKey === "discarded"
              ? $.card.discarded_toast
              : successKey === "reopened"
                ? $.card.reopened_toast
                : $.card.deleted_toast,
          ),
        );
      } catch (err) {
        setError(
          err instanceof ApiError && err.status === 409
            ? t(($) => $.organize.already_organized)
            : t(($) => $.card.error),
        );
      }
    },
    [t],
  );

  const handleSuggest = useCallback(async () => {
    setError("");
    try {
      await suggest.mutateAsync(capture.id);
    } catch (err) {
      if (err instanceof ApiError && err.status === 503) {
        // Not an error: this deployment has no model. Say it once, page-wide.
        onNoModel();
        return;
      }
      setError(t(($) => $.card.suggest_failed));
    }
  }, [capture.id, onNoModel, suggest, t]);

  const busy =
    organize.isPending || reopen.isPending || suggest.isPending || remove.isPending;

  return (
    <li className="flex flex-col gap-2 rounded-lg border px-3 py-2.5">
      <div className="flex min-w-0 flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
        <Icon aria-hidden="true" className="size-3.5 shrink-0" />
        <span>{kindLabel(t, capture.kind)}</span>
        <Badge variant="secondary" className="text-micro">
          {originLabel(t, capture.origin)}
        </Badge>
        {capture.transcription_status === "pending" ? (
          <Badge variant="outline" className="text-micro">
            {t(($) => $.card.transcription_pending)}
          </Badge>
        ) : null}
        {capture.transcription_status === "failed" ? (
          <Badge variant="outline" className="text-micro text-destructive">
            {t(($) => $.card.transcription_failed)}
          </Badge>
        ) : null}
        <span className="ml-auto shrink-0">{timeAgo(capture.created_at)}</span>
      </div>

      {capture.title_hint ? (
        <p className="text-body font-medium">{capture.title_hint}</p>
      ) : null}

      {capture.url ? (
        <a
          href={capture.url}
          target="_blank"
          rel="noreferrer noopener"
          className="break-all text-caption text-primary underline"
        >
          {capture.url}
        </a>
      ) : null}

      {capture.content ? (
        <p className="whitespace-pre-wrap break-words text-body">{capture.content}</p>
      ) : null}

      {attachment && capture.kind === "image" ? (
        <img
          src={attachment.url}
          alt={attachment.filename}
          className="max-h-48 w-auto rounded-md border object-contain"
        />
      ) : null}

      {attachment && capture.kind === "audio" ? (
        // A voice memo has no caption track: its transcript is the capture's
        // own content, rendered above as text.
        <audio controls src={attachment.download_url || attachment.url} className="w-full">
          {t(($) => $.card.audio_unsupported)}
        </audio>
      ) : null}

      {attachment && capture.kind === "file" ? (
        <a
          href={attachment.download_url || attachment.url}
          target="_blank"
          rel="noreferrer noopener"
          className="break-all text-caption text-primary underline"
        >
          {attachment.filename}
        </a>
      ) : null}

      {suggestion && !error ? (
        // A section inside the card, not a card inside the card: the heading
        // and the spacing above carry the separation. Hidden while `error` is
        // set: the Suggest button re-requests a suggestion for a capture that
        // may already carry one (regenerate), and a failed regeneration left
        // the earlier suggestion on screen next to the failure — showing a
        // proposal and "the model could not be reached" at once (UX audit).
        // Only the real, current state (the error) should be visible.
        <div className="mt-1 flex flex-col gap-1">
          <span className="flex items-center gap-1.5 text-caption font-medium">
            <Sparkles aria-hidden="true" className="size-3.5" />
            {t(($) => $.card.suggestion_heading)}
            {suggestion.title ? <span>· {suggestion.title}</span> : null}
          </span>
          {suggestion.summary ? (
            <p className="text-caption text-muted-foreground">{suggestion.summary}</p>
          ) : null}
          <span className="flex flex-wrap items-center gap-1">
            {suggestion.tags.map((tag) => (
              <Badge key={tag} variant="outline" className="text-micro">
                {tag}
              </Badge>
            ))}
          </span>
          <p className="text-caption text-muted-foreground">
            {suggestion.action === "merge"
              ? t(($) => $.card.suggestion_action_merge, {
                  title: suggestion.merge_note?.title ?? "",
                })
              : suggestion.action === "discard"
                ? t(($) => $.card.suggestion_action_discard)
                : t(($) => $.card.suggestion_action_note)}
          </p>
        </div>
      ) : null}

      {capture.status === "organized" && capture.note_id && slug ? (
        <AppLink
          href={`${paths.workspace(slug).brain()}?note=${encodeURIComponent(capture.note_id)}`}
          className="text-caption text-primary underline"
        >
          {t(($) => $.card.open_note)}
        </AppLink>
      ) : null}

      <div className="flex flex-wrap items-center gap-1.5">
        {capture.status === "raw" ? (
          <>
            <Button
              size="sm"
              disabled={busy}
              onClick={() => onOrganize(capture, "note")}
            >
              {t(($) => $.card.save_as_note)}
            </Button>
            <Button
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => onOrganize(capture, "merge")}
            >
              {t(($) => $.card.merge_into)}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={busy}
              onClick={() =>
                void run(
                  organize.mutateAsync({ id: capture.id, input: { action: "discard" } }),
                  "discarded",
                )
              }
            >
              {t(($) => $.card.discard)}
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={busy || noModel}
              title={noModel ? t(($) => $.card.no_model) : undefined}
              onClick={() => void handleSuggest()}
            >
              {suggest.isPending ? (
                <Loader2 aria-hidden="true" className="size-3.5 animate-spin" />
              ) : null}
              {suggest.isPending ? t(($) => $.card.suggesting) : t(($) => $.card.suggest)}
            </Button>
          </>
        ) : null}

        {capture.status === "discarded" ? (
          <Button
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => void run(reopen.mutateAsync(capture.id), "reopened")}
          >
            {t(($) => $.card.reopen)}
          </Button>
        ) : null}

        <Button
          variant="ghost"
          size="sm"
          className="ml-auto text-destructive"
          disabled={busy}
          onClick={() => setConfirmDelete(true)}
        >
          <Trash2 aria-hidden="true" className="size-3.5" />
          {t(($) => $.card.delete)}
        </Button>
      </div>

      {error ? (
        <p role="alert" className="text-caption text-destructive">
          {error}
        </p>
      ) : null}

      <AlertDialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.card.delete_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.card.delete_description)}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.card.delete_cancel)}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setConfirmDelete(false);
                void run(remove.mutateAsync(capture.id), "deleted");
              }}
            >
              {t(($) => $.card.delete_confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </li>
  );
}
