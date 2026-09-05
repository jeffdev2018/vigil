"use client";

import { useMemo, useState, type FormEvent } from "react";
import { ChevronRight, Pencil, Plus, Target, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  goalDetailOptions,
  goalListOptions,
  goalProgress,
  useCreateGoal,
  useDeleteGoal,
  useUpdateGoal,
} from "@multica/core/goals";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { Goal, GoalStatus, GoalWriteRequest } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { CollectionPageHeader, CollectionPageHeaderAction, CollectionPageState } from "../../layout/collection-page";
import { useT } from "../../i18n";
import { flattenGoalTree } from "./goal-tree";

const GOAL_STATUSES: GoalStatus[] = ["draft", "active", "done", "dropped"];

const STATUS_BADGE: Record<GoalStatus, string> = {
  draft: "bg-muted text-muted-foreground",
  active: "bg-primary/10 text-primary",
  done: "bg-success/10 text-success",
  dropped: "bg-destructive/10 text-destructive",
};

const SELECT_CLASS = "h-8 w-full rounded-md border bg-background px-2 text-body";

type FormState = {
  title: string;
  description: string;
  success_measure: string;
  due_date: string;
  owner_id: string;
  status: GoalStatus;
  parent_goal_id: string;
};

type FormTarget = { mode: "create"; parentId: string | null } | { mode: "edit"; goal: Goal };

function initialForm(target: FormTarget): FormState {
  if (target.mode === "edit") {
    const g = target.goal;
    return {
      title: g.title,
      description: g.description,
      success_measure: g.success_measure,
      due_date: g.due_date ?? "",
      owner_id: g.owner_id ?? "",
      status: g.status,
      parent_goal_id: g.parent_goal_id ?? "",
    };
  }
  return { title: "", description: "", success_measure: "", due_date: "", owner_id: "", status: "draft", parent_goal_id: target.parentId ?? "" };
}

function toRequest(f: FormState): GoalWriteRequest {
  return {
    title: f.title.trim(),
    description: f.description,
    success_measure: f.success_measure,
    due_date: f.due_date || null,
    owner_id: f.owner_id || null,
    status: f.status,
    parent_goal_id: f.parent_goal_id || null,
  };
}

const errorMessage = (e: unknown, fallback: string) => (e instanceof Error && e.message ? e.message : fallback);

// ---------------------------------------------------------------------------
// Form dialog — create (with a preselected parent) or edit.
// ---------------------------------------------------------------------------

function GoalFormDialog({ target, goals, onClose }: { target: FormTarget; goals: Goal[]; onClose: () => void }) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const createGoal = useCreateGoal(wsId);
  const updateGoal = useUpdateGoal(wsId);
  const [form, setForm] = useState<FormState>(() => initialForm(target));
  const set = <K extends keyof FormState>(key: K, value: FormState[K]) => setForm((f) => ({ ...f, [key]: value }));
  const pending = createGoal.isPending || updateGoal.isPending;

  // A goal cannot descend from itself: exclude it and its subtree.
  const parentOptions = useMemo(() => {
    const excluded = new Set<string>();
    if (target.mode === "edit") {
      excluded.add(target.goal.id);
      let grew = true;
      while (grew) {
        grew = false;
        for (const g of goals) {
          if (g.parent_goal_id && excluded.has(g.parent_goal_id) && !excluded.has(g.id)) {
            excluded.add(g.id);
            grew = true;
          }
        }
      }
    }
    return flattenGoalTree(goals).filter(({ goal }) => !excluded.has(goal.id));
  }, [goals, target]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const data = toRequest(form);
    if (!data.title) return;
    const opts = { onSuccess: onClose, onError: (err: unknown) => toast.error(errorMessage(err, t(($) => $.form.error))) };
    if (target.mode === "edit") updateGoal.mutate({ id: target.goal.id, data }, opts);
    else createGoal.mutate(data, opts);
  };

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit} className="flex flex-col gap-4">
          <DialogHeader>
            <DialogTitle>{target.mode === "edit" ? t(($) => $.form.edit_title) : t(($) => $.form.create_title)}</DialogTitle>
            <DialogDescription className="sr-only">{t(($) => $.page.empty_description)}</DialogDescription>
          </DialogHeader>

          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.title)}
            <Input value={form.title} onChange={(e) => set("title", e.target.value)} placeholder={t(($) => $.form.title_placeholder)} autoFocus required />
          </label>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.description)}
            <Textarea value={form.description} onChange={(e) => set("description", e.target.value)} rows={3} />
          </label>
          <label className="flex flex-col gap-1 text-caption text-muted-foreground">
            {t(($) => $.form.success_measure)}
            <Input value={form.success_measure} onChange={(e) => set("success_measure", e.target.value)} placeholder={t(($) => $.form.success_measure_placeholder)} />
          </label>
          <div className="grid grid-cols-2 gap-3">
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.due_date)}
              <input type="date" className={SELECT_CLASS} value={form.due_date} onChange={(e) => set("due_date", e.target.value)} />
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.status)}
              <select className={SELECT_CLASS} value={form.status} onChange={(e) => set("status", e.target.value as GoalStatus)}>
                {GOAL_STATUSES.map((s) => (
                  <option key={s} value={s}>{t(($) => $.status[s])}</option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.owner)}
              <select className={SELECT_CLASS} value={form.owner_id} onChange={(e) => set("owner_id", e.target.value)}>
                <option value="">{t(($) => $.form.owner_none)}</option>
                {members.map((m) => (
                  <option key={m.user_id} value={m.user_id}>{m.name}</option>
                ))}
              </select>
            </label>
            <label className="flex flex-col gap-1 text-caption text-muted-foreground">
              {t(($) => $.form.parent)}
              <select className={SELECT_CLASS} value={form.parent_goal_id} onChange={(e) => set("parent_goal_id", e.target.value)}>
                <option value="">{t(($) => $.form.parent_none)}</option>
                {parentOptions.map(({ goal, depth }) => (
                  <option key={goal.id} value={goal.id}>{`${"  ".repeat(depth)}${goal.title}`}</option>
                ))}
              </select>
            </label>
          </div>
          {form.status === "active" && !form.owner_id && (
            <p className="text-caption text-warning">{t(($) => $.form.owner_hint)}</p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={onClose}>{t(($) => $.form.cancel)}</Button>
            <Button type="submit" size="sm" disabled={pending || !form.title.trim()}>
              {target.mode === "edit" ? t(($) => $.form.save) : t(($) => $.form.create)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Expanded issues of one goal.
// ---------------------------------------------------------------------------

function GoalIssues({ goalId }: { goalId: string }) {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const { data, isPending } = useQuery(goalDetailOptions(wsId, goalId));
  const issues = data?.issues ?? [];
  if (isPending) return <p className="text-caption text-muted-foreground">{t(($) => $.page.loading_issues)}</p>;
  if (issues.length === 0) return <p className="text-caption text-muted-foreground">{t(($) => $.page.no_issues)}</p>;
  return (
    <ul className="flex flex-col gap-0.5">
      {issues.map((issue) => (
        <li key={issue.id}>
          <AppLink href={paths.issueDetail(issue.id)} className="flex items-center gap-2 rounded-md px-2 py-1 text-body hover:bg-accent/70">
            <span className="font-mono text-caption text-muted-foreground">{issue.identifier}</span>
            <span className="min-w-0 flex-1 truncate">{issue.title}</span>
            <span className="text-caption text-muted-foreground">{issue.status}</span>
          </AppLink>
        </li>
      ))}
    </ul>
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export function GoalsPage() {
  const { t } = useT("goals");
  const wsId = useWorkspaceId();
  const currentUser = useAuthStore((s) => s.user);
  const { data: goals = [], isLoading } = useQuery(goalListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const deleteGoal = useDeleteGoal(wsId);
  const [formTarget, setFormTarget] = useState<FormTarget | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<Goal | null>(null);
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set());

  const rows = useMemo(() => flattenGoalTree(goals), [goals]);
  const memberName = useMemo(() => new Map(members.map((m) => [m.user_id, m.name])), [members]);
  const canDelete = useMemo(() => {
    const me = members.find((m) => m.user_id === currentUser?.id);
    return me?.role === "owner" || me?.role === "admin";
  }, [members, currentUser?.id]);

  const toggleExpanded = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  const runDelete = (goal: Goal) => {
    deleteGoal.mutate(goal.id, {
      onError: (err) => toast.error(errorMessage(err, t(($) => $.delete_dialog.error))),
      onSettled: () => setConfirmDelete(null),
    });
  };

  return (
    <div className="relative flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Target}
        title={t(($) => $.page.title)}
        count={goals.length}
        actions={<CollectionPageHeaderAction icon={Plus} label={t(($) => $.page.new_goal)} onClick={() => setFormTarget({ mode: "create", parentId: null })} />}
      />

      {!isLoading && goals.length === 0 ? (
        <CollectionPageState
          icon={Target}
          title={t(($) => $.page.empty)}
          description={t(($) => $.page.empty_description)}
          actions={
            <Button size="sm" variant="outline" onClick={() => setFormTarget({ mode: "create", parentId: null })}>
              {t(($) => $.page.new_goal)}
            </Button>
          }
        />
      ) : (
        <div className="flex-1 overflow-y-auto px-4 py-2">
          {rows.map(({ goal, depth }) => {
            const isOpen = expanded.has(goal.id);
            const progress = Math.round(goalProgress(goal) * 100);
            return (
              <div key={goal.id} data-testid="goal-row" data-depth={depth} style={{ marginLeft: `${depth * 1.5}rem` }} className="group/goal border-b border-border/60 py-2">
                <div className="flex items-start gap-2">
                  <button
                    type="button"
                    aria-label={isOpen ? t(($) => $.page.collapse) : t(($) => $.page.expand)}
                    aria-expanded={isOpen}
                    onClick={() => toggleExpanded(goal.id)}
                    className="mt-0.5 rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
                  >
                    <ChevronRight className={cn("size-3.5 transition-transform", isOpen && "rotate-90")} />
                  </button>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="truncate text-body font-medium">{goal.title}</span>
                      <Badge className={STATUS_BADGE[goal.status]}>{t(($) => $.status[goal.status])}</Badge>
                      <span className="text-caption text-muted-foreground">
                        {goal.owner_id ? memberName.get(goal.owner_id) ?? goal.owner_id : t(($) => $.page.no_owner)}
                      </span>
                      <span className="text-caption text-muted-foreground">{goal.due_date ?? t(($) => $.page.no_due_date)}</span>
                    </div>
                    <div className="mt-1 flex items-center gap-2">
                      <div className="h-1.5 w-32 overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={progress} aria-valuemin={0} aria-valuemax={100}>
                        <div className="h-full rounded-full bg-primary" style={{ width: `${progress}%` }} />
                      </div>
                      <span className="text-caption tabular-nums text-muted-foreground">
                        {t(($) => $.page.progress, { done: goal.done_count, total: goal.issue_count })}
                      </span>
                    </div>
                    {goal.success_measure && (
                      <p className="mt-1 truncate text-caption text-muted-foreground">{goal.success_measure}</p>
                    )}
                    {isOpen && (
                      <div className="mt-2">
                        <GoalIssues goalId={goal.id} />
                      </div>
                    )}
                  </div>
                  <div className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity group-hover/goal:opacity-100 focus-within:opacity-100">
                    <Button type="button" variant="ghost" size="icon" className="size-7" aria-label={t(($) => $.page.add_sub_goal)} onClick={() => setFormTarget({ mode: "create", parentId: goal.id })}>
                      <Plus className="size-3.5" />
                    </Button>
                    <Button type="button" variant="ghost" size="icon" className="size-7" aria-label={t(($) => $.page.edit)} onClick={() => setFormTarget({ mode: "edit", goal })}>
                      <Pencil className="size-3.5" />
                    </Button>
                    {canDelete && (
                      <Button type="button" variant="ghost" size="icon" className="size-7 text-destructive hover:text-destructive" aria-label={t(($) => $.page.delete)} onClick={() => setConfirmDelete(goal)}>
                        <Trash2 className="size-3.5" />
                      </Button>
                    )}
                  </div>
                </div>
              </div>
            );
          })}
        </div>
      )}

      {formTarget && <GoalFormDialog key={formTarget.mode === "edit" ? formTarget.goal.id : `new:${formTarget.parentId ?? ""}`} target={formTarget} goals={goals} onClose={() => setFormTarget(null)} />}

      <Dialog open={confirmDelete !== null} onOpenChange={(open) => { if (!open) setConfirmDelete(null); }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.delete_dialog.title)}</DialogTitle>
            <DialogDescription>{t(($) => $.delete_dialog.description)}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={() => setConfirmDelete(null)}>{t(($) => $.delete_dialog.cancel)}</Button>
            <Button type="button" variant="destructive" size="sm" disabled={deleteGoal.isPending} onClick={() => { if (confirmDelete) runDelete(confirmDelete); }}>
              {t(($) => $.delete_dialog.confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
