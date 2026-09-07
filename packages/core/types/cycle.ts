// Dated cycles (F29): a project's time-boxed iteration, with human and agent
// capacity kept apart because the two are not interchangeable — a sprint that
// fits its humans can still be impossible for its agents.

export type CycleStatus = "upcoming" | "active" | "closed";

/** "issues" = one issue is one unit; "property" = a number property's value. */
export type CycleLoadUnit = "issues" | "property";

export interface CycleCapacitySide {
  /** null means nobody declared one — not zero, and not over capacity. */
  capacity: number | null;
  load: number;
}

export interface CycleCapacity {
  human: CycleCapacitySide;
  agent: CycleCapacitySide;
  /** Work in the cycle with no assignee. Loads neither side. */
  unassigned_load: number;
}

export interface Cycle {
  id: string;
  workspace_id: string;
  project_id: string;
  name: string;
  description: string;
  /** Calendar days "YYYY-MM-DD", same contract as issue.start_date. */
  start_date: string;
  end_date: string;
  rollover: boolean;
  closed_at: string | null;
  /** Derived from the dates and the close; never stored. */
  status: CycleStatus;
  /** Open and past its end date. */
  late: boolean;
  load_unit: CycleLoadUnit;
  load_property_id: string | null;
  issue_count: number;
  done_count: number;
  capacity: CycleCapacity;
  created_at: string;
  updated_at: string;
}

export interface CycleWriteRequest {
  project_id?: string;
  name?: string;
  description?: string;
  start_date?: string;
  end_date?: string;
  human_capacity?: number | null;
  agent_capacity?: number | null;
  load_property_id?: string | null;
  rollover?: boolean;
}

export interface CycleBurndownDay {
  date: string;
  /** null on days the series cannot speak for: the future, and the past
   *  before the first snapshot when no value can be carried forward. */
  remaining_count: number | null;
  remaining_load: number | null;
  ideal_count: number;
  ideal_load: number;
  human_load: number | null;
  agent_load: number | null;
}

export interface CycleBurndown {
  days: CycleBurndownDay[];
  capacity: { human: number | null; agent: number | null };
  load_unit: CycleLoadUnit;
  load_property_id: string | null;
  /** First day with real history. Earlier days are a flat fill. */
  approximate_before?: string | null;
}

export interface ListCyclesResponse {
  cycles: Cycle[];
  total: number;
}

/** Per-project progress of the issues that count for a goal (F29). */
export interface GoalProjectProgress {
  project_id: string;
  name: string;
  total_count: number;
  done_count: number;
}

export interface GoalProgress {
  goal_id: string;
  projects: GoalProjectProgress[];
  total_count: number;
  done_count: number;
}
