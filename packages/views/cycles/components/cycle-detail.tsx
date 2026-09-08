"use client";

import { useMemo, useState } from "react";
import { CalendarRange, Pencil } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  cycleBurndownOptions,
  cycleDetailOptions,
  useCloseCycle,
} from "@multica/core/cycles";
import { useWorkspaceId } from "@multica/core/hooks";
import type { IssueScope } from "@multica/core/issues/surface/scope";
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
import { PageHeader } from "../../layout/page-header";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { useT } from "../../i18n";
import { BurndownChart } from "./burndown-chart";
import { CapacityBar } from "./capacity-bar";
import { CycleFormDialog, type CycleFormTarget } from "./cycle-form-dialog";

const errorMessage = (e: unknown, fallback: string) =>
  e instanceof Error && e.message ? e.message : fallback;

export function CycleDetail({ cycleId }: { cycleId: string }) {
  const { t } = useT("cycles");
  const wsId = useWorkspaceId();
  const { data: cycle, isPending } = useQuery(cycleDetailOptions(wsId, cycleId));
  const { data: burndown } = useQuery(cycleBurndownOptions(wsId, cycleId));
  const closeCycle = useCloseCycle(wsId);
  const [formTarget, setFormTarget] = useState<CycleFormTarget | null>(null);
  const [confirmClose, setConfirmClose] = useState(false);

  // The surface is scoped to the cycle, but carries its project so creating an
  // issue here files it in the right project AND plans it into this cycle.
  const scope = useMemo<IssueScope | null>(
    () =>
      cycle ? { type: "cycle", cycleId: cycle.id, projectId: cycle.project_id } : null,
    [cycle],
  );

  if (isPending) {
    return (
      <div className="flex flex-1 items-center justify-center text-body text-muted-foreground">
        {t(($) => $.detail.loading)}
      </div>
    );
  }
  if (!cycle) {
    return (
      <div className="flex flex-1 items-center justify-center text-body text-muted-foreground">
        {t(($) => $.detail.not_found)}
      </div>
    );
  }

  return (
    <div className="relative flex flex-1 min-h-0 flex-col">
      <PageHeader>
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <CalendarRange aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-body font-medium">{cycle.name}</h1>
          <Badge className="shrink-0">{t(($) => $.status[cycle.status])}</Badge>
          {cycle.late && (
            <Badge className="shrink-0 bg-destructive/10 text-destructive">
              {t(($) => $.page.late)}
            </Badge>
          )}
          <span className="shrink-0 font-mono text-caption tabular-nums text-muted-foreground">
            {t(($) => $.page.dates, { start: cycle.start_date, end: cycle.end_date })}
          </span>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="size-7"
            aria-label={t(($) => $.page.edit)}
            onClick={() => setFormTarget({ mode: "edit", cycle })}
          >
            <Pencil className="size-3.5" />
          </Button>
          {cycle.status !== "closed" && (
            <Button type="button" variant="outline" size="sm" onClick={() => setConfirmClose(true)}>
              {t(($) => $.detail.close)}
            </Button>
          )}
        </div>
      </PageHeader>

      <div className="grid gap-4 border-b px-4 py-3 md:grid-cols-2">
        <section className="flex flex-col gap-2" aria-label={t(($) => $.detail.capacity)}>
          <h2 className="text-caption font-medium text-muted-foreground">
            {t(($) => $.detail.capacity)}
          </h2>
          <CapacityBar
            label={t(($) => $.detail.human)}
            side={cycle.capacity.human}
            undeclaredLabel={t(($) => $.detail.capacity_undeclared)}
            valueLabel={(load, capacity) => t(($) => $.detail.capacity_value, { load, capacity })}
            overLabel={(over) => t(($) => $.detail.over_capacity, { over })}
          />
          <CapacityBar
            label={t(($) => $.detail.agent)}
            side={cycle.capacity.agent}
            tone="accent"
            undeclaredLabel={t(($) => $.detail.capacity_undeclared)}
            valueLabel={(load, capacity) => t(($) => $.detail.capacity_value, { load, capacity })}
            overLabel={(over) => t(($) => $.detail.over_capacity, { over })}
          />
          {cycle.capacity.unassigned_load > 0 && (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.detail.unassigned)}: {cycle.capacity.unassigned_load}
            </p>
          )}
          <p className="text-caption text-muted-foreground">
            {cycle.load_unit === "property"
              ? t(($) => $.detail.load_unit_property)
              : t(($) => $.detail.load_unit_issues)}
          </p>
        </section>

        <section className="flex flex-col gap-2" aria-label={t(($) => $.detail.burndown)}>
          <h2 className="text-caption font-medium text-muted-foreground">
            {t(($) => $.detail.burndown)}
          </h2>
          {burndown ? (
            <BurndownChart burndown={burndown} />
          ) : (
            <p className="text-caption text-muted-foreground">{t(($) => $.detail.loading)}</p>
          )}
        </section>
      </div>

      <div className="flex min-h-0 flex-1 flex-col">
        {scope && (
          <IssueSurface
            scope={scope}
            modes={["board", "list", "table", "swimlane", "gantt", "calendar"]}
          />
        )}
      </div>

      {formTarget && (
        <CycleFormDialog target={formTarget} onClose={() => setFormTarget(null)} />
      )}

      <Dialog open={confirmClose} onOpenChange={setConfirmClose}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.detail.close_title)}</DialogTitle>
            <DialogDescription>{t(($) => $.detail.close_description)}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={() => setConfirmClose(false)}>
              {t(($) => $.detail.cancel)}
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={closeCycle.isPending}
              onClick={() =>
                closeCycle.mutate(cycle.id, {
                  onError: (err) =>
                    toast.error(errorMessage(err, t(($) => $.detail.close_error))),
                  onSettled: () => setConfirmClose(false),
                })
              }
            >
              {t(($) => $.detail.close_confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
