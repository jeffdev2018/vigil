"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Ban, Copy, Link2, Lock } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useAddIssueDependency } from "@multica/core/issues/dependencies";
import type { IssueDependencyType } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

/**
 * Picks the other side of a relation. `data.type` preselects the relation
 * from the current issue's point of view; when absent, a type selector rides
 * above the search so ONE entry point covers all four relations (R01).
 */
const RELATION_TYPES: IssueDependencyType[] = ["blocks", "blocked_by", "related", "duplicate"];

const TYPE_ICONS: Record<IssueDependencyType, typeof Ban> = {
  blocks: Ban,
  blocked_by: Lock,
  related: Link2,
  duplicate: Copy,
};

export function AddIssueDependencyModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const issueId = (data?.issueId as string) || "";
  const preset = data?.type as IssueDependencyType | undefined;
  const initial = preset && RELATION_TYPES.includes(preset) ? preset : "blocks";
  const [type, setType] = useState<IssueDependencyType>(initial);
  const wsId = useWorkspaceId();
  const addDependency = useAddIssueDependency(wsId);

  const selector = (
    <div className="flex flex-wrap gap-1 px-3 pb-2 pt-1" data-testid="relation-type-selector">
      {RELATION_TYPES.map((candidate) => {
        const Icon = TYPE_ICONS[candidate];
        const active = candidate === type;
        return (
          <button
            key={candidate}
            type="button"
            aria-pressed={active}
            onClick={() => setType(candidate)}
            className={cn(
              "flex items-center gap-1.5 rounded-md border px-2 py-1 text-caption transition-colors",
              active
                ? "border-transparent bg-primary/10 font-medium text-primary data-[active]:hover:bg-primary/15"
                : "border-border text-muted-foreground hover:bg-accent hover:text-foreground",
            )}
            data-active={active ? "true" : undefined}
          >
            <Icon className="h-3.5 w-3.5 shrink-0" />
            {t(($) => $.add_dependency.type_labels[candidate])}
          </button>
        );
      })}
    </div>
  );

  return (
    <IssuePickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title={t(($) => $.add_dependency.title)}
      description={t(($) => $.add_dependency.description)}
      excludeIds={[issueId]}
      above={selector}
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
