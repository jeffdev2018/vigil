"use client";

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
import { useT } from "../../i18n";

/**
 * Confirm step shared by the two surfaces that delete a meeting — the list row
 * menu and the detail header. Deleting drops the transcript for good, and both
 * entry points are one click away from a row the user may have mis-aimed at.
 *
 * Fully controlled: the caller owns `open` and the in-flight `pending` state so
 * a second click cannot race the request.
 */
export function DeleteMeetingDialog({
  open,
  title,
  pending,
  onOpenChange,
  onConfirm,
}: {
  open: boolean;
  title: string;
  pending?: boolean;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}) {
  const { t } = useT("meetings");
  if (!open) return null;
  return (
    <AlertDialog open onOpenChange={onOpenChange}>
      <AlertDialogContent onClick={(e) => e.stopPropagation()}>
        <AlertDialogHeader>
          <AlertDialogTitle>{t(($) => $.delete_dialog.title, { title })}</AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.delete_dialog.body)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={pending}>
            {t(($) => $.delete_dialog.cancel)}
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={pending}
            // Deleting is awaited by the caller (the detail page navigates on
            // success), so the dialog stays up until the caller closes it.
            onClick={onConfirm}
          >
            {t(($) => $.delete_dialog.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
