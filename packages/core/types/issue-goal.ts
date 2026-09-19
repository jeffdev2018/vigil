// Goal loop: an agent works one issue toward a stated goal across bounded
// continuations, stopping when it is satisfied, stuck, or needs the human.
// Named `IssueGoal*` (not `Goal*`) to stay clear of the unrelated K74
// workspace-mission goal hierarchy in ./goal.ts.

export type IssueGoalStatus = "active" | "paused" | "waiting_user" | "satisfied" | "stopped";

export type IssueGoalQuestionKind = "text" | "choice";

export interface IssueGoalQuestion {
  kind: IssueGoalQuestionKind;
  prompt: string;
  options?: string[];
  run_id: string;
  asked_at: string;
  answer?: string;
  answered_by?: string;
  answered_by_name?: string;
  answered_at?: string;
}

export interface IssueGoal {
  id: string;
  issue_id: string;
  goal: string;
  status: IssueGoalStatus;
  continuation: number;
  max_continuations: number;
  no_progress: number;
  /** "" | "satisfied" | "continued" | "stopped:<why>" (why: exhausted, stagnation, needs_user_input, external_wait, run_failed, judge_unavailable, paused, issue_changed). */
  last_outcome: string;
  last_blocker?: string;
  last_reason?: string;
  next_step?: string;
  evidence: string[];
  question?: IssueGoalQuestion;
  last_run_id?: string;
  chain_root_task_id?: string;
  done_request_id?: string;
  set_by_type: "member" | "agent" | "system";
  updated_at: string;
}

export interface SetIssueGoalInput {
  goal: string;
  max_continuations?: number;
}
