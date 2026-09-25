package handler

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Dashboard vélocité mixte (JEF-251): throughput hebdo member/agent, cycle
// time médian contre la période précédente, et coût hebdo par issue fermée.
// Computed on read like the other dashboard endpoints; an issue closed
// without a run has no cost and is absent from the cost series (K04).

type VelocityThroughputWeek struct {
	WeekStart   string `json:"week_start"`
	MemberCount int64  `json:"member_count"`
	AgentCount  int64  `json:"agent_count"`
}

type VelocityCycleTime struct {
	MemberMedianDays     *float64 `json:"member_median_days"`
	AgentMedianDays      *float64 `json:"agent_median_days"`
	PrevMemberMedianDays *float64 `json:"prev_member_median_days"`
	PrevAgentMedianDays  *float64 `json:"prev_agent_median_days"`
	MemberCount          int64    `json:"member_count"`
	AgentCount           int64    `json:"agent_count"`
}

type VelocityCostWeek struct {
	WeekStart         string `json:"week_start"`
	IssueCount        int64  `json:"issue_count"`
	TotalCostUsdTicks int64  `json:"total_cost_usd_ticks"`
	MeanCostUsdTicks  int64  `json:"mean_cost_usd_ticks"`
}

type DashboardVelocityWeeklyResponse struct {
	Throughput         []VelocityThroughputWeek `json:"throughput"`
	CycleTime          VelocityCycleTime        `json:"cycle_time"`
	CostPerClosedIssue []VelocityCostWeek       `json:"cost_per_closed_issue"`
}

// GetDashboardVelocityWeekly — GET /api/dashboard/velocity/weekly?days=&project_id=.
func (h *Handler) GetDashboardVelocityWeekly(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	// Exact N-day cutoff, like cost-per-deliverable: the previous-period
	// comparison needs two back-to-back windows of the same length.
	tz := h.resolveViewingTZ(r)
	since := parseExactSinceParamInTZ(r, 30, tz)
	now := time.Now()
	period := now.Sub(since.Time)
	wsUUID := parseUUID(workspaceID)
	ctx := r.Context()

	throughputRows, err := h.Queries.ListVelocityWeeklyThroughput(ctx, db.ListVelocityWeeklyThroughputParams{
		WorkspaceID: wsUUID, PeriodStart: since, PeriodEnd: pgtype.Timestamptz{Time: now, Valid: true}, ProjectID: projectID,
	})
	if err != nil {
		slog.Warn("velocity throughput failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compute velocity")
		return
	}
	curCycle, err := h.Queries.GetVelocityCycleTime(ctx, db.GetVelocityCycleTimeParams{
		WorkspaceID: wsUUID, PeriodStart: since, PeriodEnd: pgtype.Timestamptz{Time: now, Valid: true}, ProjectID: projectID,
	})
	if err != nil {
		slog.Warn("velocity cycle time failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compute velocity")
		return
	}
	prevCycle, err := h.Queries.GetVelocityCycleTime(ctx, db.GetVelocityCycleTimeParams{
		WorkspaceID: wsUUID, PeriodStart: pgtype.Timestamptz{Time: since.Time.Add(-period), Valid: true}, PeriodEnd: since, ProjectID: projectID,
	})
	if err != nil {
		slog.Warn("velocity cycle time (previous period) failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compute velocity")
		return
	}
	costRows, err := h.Queries.ListVelocityWeeklyIssueCosts(ctx, db.ListVelocityWeeklyIssueCostsParams{
		WorkspaceID: wsUUID, PeriodStart: since, PeriodEnd: pgtype.Timestamptz{Time: now, Valid: true}, ProjectID: projectID,
	})
	if err != nil {
		slog.Warn("velocity cost per closed issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compute velocity")
		return
	}

	throughput := make([]VelocityThroughputWeek, 0, len(throughputRows))
	for _, row := range throughputRows {
		throughput = append(throughput, VelocityThroughputWeek{
			WeekStart:   row.WeekStart.Time.Format("2006-01-02"),
			MemberCount: row.MemberCount,
			AgentCount:  row.AgentCount,
		})
	}
	costs := make([]VelocityCostWeek, 0, len(costRows))
	for _, row := range costRows {
		costs = append(costs, VelocityCostWeek{
			WeekStart:         row.WeekStart.Time.Format("2006-01-02"),
			IssueCount:        row.IssueCount,
			TotalCostUsdTicks: row.TotalCostUsdTicks,
			MeanCostUsdTicks:  row.MeanCostUsdTicks,
		})
	}

	cycle := VelocityCycleTime{
		MemberCount:          curCycle.MemberCount,
		AgentCount:           curCycle.AgentCount,
		PrevMemberMedianDays: velocityMedianOrNil(prevCycle.MemberMedianDays, prevCycle.MemberCount),
		PrevAgentMedianDays:  velocityMedianOrNil(prevCycle.AgentMedianDays, prevCycle.AgentCount),
	}
	cycle.MemberMedianDays = velocityMedianOrNil(curCycle.MemberMedianDays, curCycle.MemberCount)
	cycle.AgentMedianDays = velocityMedianOrNil(curCycle.AgentMedianDays, curCycle.AgentCount)

	writeJSON(w, http.StatusOK, DashboardVelocityWeeklyResponse{
		Throughput:         throughput,
		CycleTime:          cycle,
		CostPerClosedIssue: costs,
	})
}

// velocityMedianOrNil publishes the median only when the window actually held
// issues of the type: the SQL COALESCEs an empty window to 0 (sqlc emits no
// pointer for percentile_cont), and the contract wants null there.
func velocityMedianOrNil(median float64, count int64) *float64 {
	if count == 0 {
		return nil
	}
	return &median
}
