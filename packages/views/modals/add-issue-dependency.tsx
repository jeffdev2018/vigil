"use client";

import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAddIssueDependency } from "@multica/core/issues/dependencies";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

/**
 * Picks the other side of a blocks / blocked_by link. `data.type` is the
 * relation from the current issue's point of view.
 */
export function AddIssueDependencyModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const issueId = (data?.issueId as string) || "";
  const type = data?.type === "blocked_by" ? "blocked_by" : "blocks";
  const wsId = useWorkspaceId();
  const addDependency = useAddIssueDependency(wsId);

  return (
    <IssuePickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title={
        type === "blocks"
          ? t(($) => $.add_dependency.title_blocks)
          : t(($) => $.add_dependency.title_blocked_by)
      }
      description={t(($) => $.add_dependency.description)}
      excludeIds={[issueId]}
      onSelect={(selected) => {
        addDependency.mutate(
          { issueId, targetIssueId: selected.id, type },
          {
            onSuccess: () =>
              toast.success(
                t(($) => $.add_dependency.toast_success, { identifier: selected.identifier }),
              ),
            onError: (err) =>
              toast.error(
                err instanceof Error && err.message
                  ? err.message
                  : t(($) => $.add_dependency.toast_failed),
              ),
          },
        );
      }}
    />
  );
}
