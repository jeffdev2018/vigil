import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { Goal, GoalWriteRequest } from "../types";
import { projectKeys } from "../projects/queries";

// Goals with ancestry (K74): the tree, its progress and the links to projects.

export const goalKeys = {
  all: (wsId: string) => ["goals", wsId] as const,
  list: (wsId: string) => [...goalKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...goalKeys.all(wsId), "detail", id] as const,
};

export function goalListOptions(wsId: string) {
  return queryOptions({
    queryKey: goalKeys.list(wsId),
    queryFn: () => api.listGoals(),
    select: (data) => data.goals,
  });
}

export function goalDetailOptions(wsId: string, id: string) {
  return queryOptions({ queryKey: goalKeys.detail(wsId, id), queryFn: () => api.getGoal(id) });
}

export function useCreateGoal(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: GoalWriteRequest) => api.createGoal(data),
    onSettled: () => qc.invalidateQueries({ queryKey: goalKeys.all(wsId) }),
  });
}

export function useUpdateGoal(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; data: GoalWriteRequest }) => api.updateGoal(v.id, v.data),
    onSettled: () => qc.invalidateQueries({ queryKey: goalKeys.all(wsId) }),
  });
}

export function useDeleteGoal(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteGoal(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: goalKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}

export function useSetProjectGoals(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { projectId: string; goalIds: string[] }) => api.setProjectGoals(v.projectId, v.goalIds),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: goalKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: projectKeys.all(wsId) });
    },
  });
}

/** Children of a goal, in creation order. Root goals have a null parent. */
export function goalChildren(goals: Goal[], parentId: string | null): Goal[] {
  return goals.filter((g) => g.parent_goal_id === parentId);
}

/** Chain from the root goal down to goalId, cut at the first repeated id. */
export function goalAncestry(goals: Goal[], goalId: string | null): Goal[] {
  const byId = new Map(goals.map((g) => [g.id, g] as const));
  const chain: Goal[] = [];
  const seen = new Set<string>();
  for (let id = goalId; id && !seen.has(id); ) {
    const g = byId.get(id);
    if (!g) break;
    seen.add(id);
    chain.unshift(g);
    id = g.parent_goal_id;
  }
  return chain;
}

/** Done ratio in [0, 1]; 0 without issues. */
export function goalProgress(goal: Pick<Goal, "issue_count" | "done_count">): number {
  return goal.issue_count > 0 ? Math.min(1, goal.done_count / goal.issue_count) : 0;
}
