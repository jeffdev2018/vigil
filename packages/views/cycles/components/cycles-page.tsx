"use client";

import { useMemo, useState } from "react";
import { CalendarRange, Pencil, Plus, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  cycleListOptions,
  cycleProgress,
  cyclesByStatus,
  useDeleteCycle,
} from "@multica/core/cycles";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Cycle, CycleStatus } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { AppLink } from "../../navigation";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";
import { useT } from "../../i18n";
import { CycleFormDialog, type CycleFormTarget } from "./cycle-form-dialog";

// Sections in the order a planner reads them: what is running now, what is
// next, then the archive. Closed sits last because it is reference, not work.
const SECTIONS: CycleStatus[] = ["active", "upcoming", "closed"];

const STATUS_BADGE: Record<CycleStatus, string> = {
  upcoming: "bg-muted text-muted-foreground",
  active: "bg-primary/10 text-primary",
  closed: "bg-success/10 text-success",
};

const errorMessage = (e: unknown, fallback: string) =>
  e instanceof Error && e.message ? e.message : fallback;

function CycleRow({
  cycle,
  projectName,
  onEdit,
  onDelete,
}: {
  cycle: Cycle;
  projectName: string | undefined;
  onEdit: () => void;
  onDelete: () => void;
}) {
  const { t } = useT("cycles");
  const paths = useWorkspacePaths();
  const progress = Math.round(cycleProgress(cycle) * 100);

  return (
    <div
      data-testid="cycle-row"
      data-cycle-id={cycle.id}
      className="group/cycle flex items-start gap-2 border-b border-border/60 py-2"
    >
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <AppLink
            href={paths.cycleDetail(cycle.id)}
            className="truncate text-body font-medium hover:underline"
          >
            {cycle.name}
          </AppLink>
          <Badge className={STATUS_BADGE[cycle.status]}>
            {t(($) => $.status[cycle.status])}
          </Badge>
          {cycle.late && (
            <Badge className="bg-destructive/10 text-destructive">
              {t(($) => $.page.late)}
            </Badge>
          )}
          {projectName && (
            <span className="truncate text-caption text-muted-foreground">{projectName}</span>
          )}
          <span className="font-mono text-caption tabular-nums text-muted-foreground">
            {t(($) => $.page.dates, { start: cycle.start_date, end: cycle.end_date })}
          </span>
        </div>
        <div className="mt-1 flex items-center gap-2">
          <div
            className="h-1.5 w-32 overflow-hidden rounded-full bg-muted"
            role="progressbar"
            aria-valuenow={progress}
            aria-valuemin={0}
            aria-valuemax={100}
          >
            <div className="h-full rounded-full bg-primary" style={{ width: `${progress}%` }} />
          </div>
          <span className="text-caption tabular-nums text-muted-foreground">
            {t(($) => $.page.progress, { done: cycle.done_count, total: cycle.issue_count })}
          </span>
        </div>
      </div>
      <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity focus-within:opacity-100 group-hover/cycle:opacity-100">
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7"
          aria-label={t(($) => $.page.edit)}
          onClick={onEdit}
        >
          <Pencil className="size-3.5" />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="size-7 text-destructive hover:text-destructive"
          aria-label={t(($) => $.page.delete)}
          onClick={onDelete}
        >
          <Trash2 className="size-3.5" />
        </Button>
      </div>
    </div>
  );
}

export function CyclesPage() {
  const { t } = useT("cycles");
  const wsId = useWorkspaceId();
  const [projectFilter, setProjectFilter] = useState("");
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: cycles = [], isLoading, isError } = useQuery(
    cycleListOptions(wsId, projectFilter || undefined),
  );
  const deleteCycle = useDeleteCycle(wsId);
  const [formTarget, setFormTarget] = useState<CycleFormTarget | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Cycle | null>(null);

  const projectName = useMemo(
    () => new Map(projects.map((p) => [p.id, p.title])),
    [projects],
  );

  const runDelete = (cycle: Cycle) => {
    deleteCycle.mutate(cycle.id, {
      onError: (err) => toast.error(errorMessage(err, t(($) => $.delete_dialog.error))),
      onSettled: () => setConfirmDelete(null),
    });
  };

  return (
    <div className="relative flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={CalendarRange}
        title={t(($) => $.page.title)}
        count={cycles.length}
        actions={
          <div className="flex items-center gap-2">
            <Select
              items={[
                { value: "", label: t(($) => $.page.all_projects) },
                ...projects.map((p) => ({ value: p.id, label: p.title })),
              ]}
              value={projectFilter}
              onValueChange={(value) => value !== null && setProjectFilter(value)}
            >
              <SelectTrigger size="sm" aria-label={t(($) => $.form.project)}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="">{t(($) => $.page.all_projects)}</SelectItem>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={p.id}>{p.title}</SelectItem>
                ))}
              </SelectContent>
            </Select>
            <CollectionPageHeaderAction
              icon={Plus}
              label={t(($) => $.page.new_cycle)}
              onClick={() =>
                setFormTarget({ mode: "create", projectId: projectFilter || null })
              }
            />
          </div>
        }
      />

      {isError ? (
        <CollectionPageState icon={CalendarRange} tone="destructive" title={t(($) => $.page.load_error)} />
      ) : !isLoading && cycles.length === 0 ? (
        <CollectionPageState
          icon={CalendarRange}
          title={t(($) => $.page.empty)}
          description={t(($) => $.page.empty_description)}
          actions={
            <Button
              size="sm"
              variant="outline"
              onClick={() =>
                setFormTarget({ mode: "create", projectId: projectFilter || null })
              }
            >
              {t(($) => $.page.new_cycle)}
            </Button>
          }
        />
      ) : (
        <div className="flex-1 overflow-y-auto px-4 py-2">
          {SECTIONS.map((status) => {
            const section = cyclesByStatus(cycles, status);
            if (section.length === 0) return null;
            return (
              <section key={status} className="mb-4" data-testid={`cycle-section-${status}`}>
                <h2 className="mb-1 text-caption font-medium text-muted-foreground">
                  {status === "active"
                    ? t(($) => $.page.section_active)
                    : status === "upcoming"
                      ? t(($) => $.page.section_upcoming)
                      : t(($) => $.page.section_closed)}
                </h2>
                {section.map((cycle) => (
                  <CycleRow
                    key={cycle.id}
                    cycle={cycle}
                    projectName={projectName.get(cycle.project_id)}
                    onEdit={() => setFormTarget({ mode: "edit", cycle })}
                    onDelete={() => setConfirmDelete(cycle)}
                  />
                ))}
              </section>
            );
          })}
        </div>
      )}

      {formTarget && (
        <CycleFormDialog
          key={formTarget.mode === "edit" ? formTarget.cycle.id : "new"}
          target={formTarget}
          onClose={() => setFormTarget(null)}
        />
      )}

      <Dialog
        open={confirmDelete !== null}
        onOpenChange={(open) => { if (!open) setConfirmDelete(null); }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.delete_dialog.title)}</DialogTitle>
            <DialogDescription>{t(($) => $.delete_dialog.description)}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setConfirmDelete(null)}
            >
              {t(($) => $.delete_dialog.cancel)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              disabled={deleteCycle.isPending}
              onClick={() => { if (confirmDelete) runDelete(confirmDelete); }}
            >
              {t(($) => $.delete_dialog.confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
