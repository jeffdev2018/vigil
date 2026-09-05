"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Link2 } from "lucide-react";
import { toast } from "sonner";
import { goalListOptions, goalProgress, useSetProjectGoals } from "@multica/core/goals";
import { useWorkspaceId } from "@multica/core/hooks";
import { Button } from "@multica/ui/components/ui/button";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { flattenGoalTree } from "../../goals/components/goal-tree";
import { useT } from "../../i18n";

/**
 * Goals a project serves (K74). The link control always sends the full
 * desired set, matching the PUT semantics of `/projects/:id/goals`.
 */
export function ProjectGoalsSection({ projectId }: { projectId: string }) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const { data: goals = [] } = useQuery(goalListOptions(wsId));
  const setProjectGoals = useSetProjectGoals(wsId);
  const linked = useMemo(() => goals.filter((g) => g.project_ids.includes(projectId)), [goals, projectId]);
  const rows = useMemo(() => flattenGoalTree(goals), [goals]);

  const toggle = (goalId: string) => {
    const current = linked.map((g) => g.id);
    const goalIds = current.includes(goalId) ? current.filter((id) => id !== goalId) : [...current, goalId];
    setProjectGoals.mutate(
      { projectId, goalIds },
      { onError: (err) => toast.error(err instanceof Error && err.message ? err.message : t(($) => $.project_section.error)) },
    );
  };

  return (
    <div data-testid="project-goals-section">
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-caption font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.project_section.header)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="pl-2 space-y-1.5">
          {linked.length === 0 && <p className="text-caption text-muted-foreground">{t(($) => $.project_section.empty)}</p>}
          {linked.map((g) => {
            const pct = Math.round(goalProgress(g) * 100);
            return (
              <div key={g.id} data-testid="project-goal" className="flex items-center gap-2 px-2 text-body">
                <span className="min-w-0 flex-1 truncate">{g.title}</span>
                <span className="text-caption text-muted-foreground">{t(($) => $.status[g.status])}</span>
                <div className="h-1.5 w-20 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}>
                  <div className="h-full rounded-full bg-primary" style={{ width: `${pct}%` }} />
                </div>
              </div>
            );
          })}
          <Popover>
            <PopoverTrigger render={<Button type="button" variant="ghost" size="sm" className="h-7 gap-1 px-2 text-caption text-muted-foreground" />}>
              <Link2 className="size-3.5" />
              {t(($) => $.project_section.link)}
            </PopoverTrigger>
            <PopoverContent align="start" className="w-64 p-1">
              {rows.length === 0 && <p className="px-2 py-1.5 text-caption text-muted-foreground">{t(($) => $.project_section.no_goals)}</p>}
              <div className="max-h-64 overflow-y-auto">
                {rows.map(({ goal, depth }) => (
                  <label key={goal.id} className="flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-body hover:bg-accent" style={{ paddingLeft: `${0.5 + depth * 0.75}rem` }}>
                    <input
                      type="checkbox"
                      className="size-3.5"
                      checked={goal.project_ids.includes(projectId)}
                      disabled={setProjectGoals.isPending}
                      onChange={() => toggle(goal.id)}
                    />
                    <span className="truncate">{goal.title}</span>
                  </label>
                ))}
              </div>
            </PopoverContent>
          </Popover>
        </div>
      )}
    </div>
  );
}
