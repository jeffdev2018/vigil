// Goals with ancestry (K74): a root goal is the workspace mission, sub-goals
// hang under it, projects link to goals and an issue names one or inherits
// its project's. Progress rolls up done issues through the tree.

export type GoalStatus = "draft" | "active" | "done" | "dropped";

export interface Goal {
  id: string;
  workspace_id: string;
  parent_goal_id: string | null;
  title: string;
  description: string;
  success_measure: string;
  // Calendar day "YYYY-MM-DD", same contract as issue.due_date.
  due_date: string | null;
  owner_id: string | null;
  status: GoalStatus;
  created_at: string;
  updated_at: string;
  // Rolled up over the goal and every goal under it.
  issue_count: number;
  done_count: number;
  project_ids: string[];
}

export interface GoalWriteRequest {
  title?: string;
  description?: string;
  success_measure?: string;
  parent_goal_id?: string | null;
  due_date?: string | null;
  owner_id?: string | null;
  status?: GoalStatus;
}

export interface ListGoalsResponse {
  goals: Goal[];
  total: number;
}
