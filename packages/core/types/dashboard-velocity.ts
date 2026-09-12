// Mixed member/agent velocity dashboard (JEF-251).
// GET /api/dashboard/velocity/weekly?days=<1|7|30|90>&project_id=<uuid?>

export interface DashboardThroughputWeek {
  /** ISO date (YYYY-MM-DD) of the week's Monday, UTC. */
  week_start: string;
  member_count: number;
  agent_count: number;
}

export interface DashboardVelocityCycleTime {
  /** null when the period closed nothing — not zero. */
  member_median_days: number | null;
  agent_median_days: number | null;
  prev_member_median_days: number | null;
  prev_agent_median_days: number | null;
  member_count: number;
  agent_count: number;
}

export interface DashboardCostPerClosedIssueWeek {
  week_start: string;
  issue_count: number;
  /** Ticks: 1e10 per USD, same convention as the other cost rollups. */
  total_cost_usd_ticks: number;
  mean_cost_usd_ticks: number;
}

export interface DashboardVelocityWeekly {
  throughput: DashboardThroughputWeek[];
  cycle_time: DashboardVelocityCycleTime;
  cost_per_closed_issue: DashboardCostPerClosedIssueWeek[];
}
