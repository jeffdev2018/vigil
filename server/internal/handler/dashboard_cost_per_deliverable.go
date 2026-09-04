package handler

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Cost per deliverable (K04): what a closed issue and a merged pull request
// cost, over the dashboard's period, against the previous period of the same
// length. Computed on read like the other dashboard endpoints; an issue
// closed without a run is not a zero-cost deliverable, it is absent.

type DeliverableCostStats struct {
	Count          int   `json:"count"`
	MeanUsdTicks   int64 `json:"mean_usd_ticks"`
	MedianUsdTicks int64 `json:"median_usd_ticks"`
	TotalUsdTicks  int64 `json:"total_usd_ticks"`
	// UncostedCount is how many deliverables carry usage rows without a
	// price: their cost is a floor.
	UncostedCount int `json:"uncosted_count"`
	// TrendPct compares the mean to the previous period; nil without a
	// previous period to compare to.
	TrendPct *float64 `json:"trend_pct"`
}

type CostPerDeliverableResponse struct {
	Days         int                  `json:"days"`
	Issues       DeliverableCostStats `json:"issues"`
	PullRequests DeliverableCostStats `json:"pull_requests"`
}

type deliverableCost struct {
	cost     int64
	uncosted bool
}

func deliverableStats(items []deliverableCost) DeliverableCostStats {
	s := DeliverableCostStats{Count: len(items)}
	if len(items) == 0 {
		return s
	}
	costs := make([]int64, 0, len(items))
	for _, it := range items {
		costs = append(costs, it.cost)
		s.TotalUsdTicks += it.cost
		if it.uncosted {
			s.UncostedCount++
		}
	}
	sort.Slice(costs, func(i, j int) bool { return costs[i] < costs[j] })
	s.MeanUsdTicks = s.TotalUsdTicks / int64(len(costs))
	if n := len(costs); n%2 == 1 {
		s.MedianUsdTicks = costs[n/2]
	} else {
		s.MedianUsdTicks = (costs[n/2-1] + costs[n/2]) / 2
	}
	return s
}

func withTrend(cur, prev DeliverableCostStats) DeliverableCostStats {
	if prev.Count == 0 || prev.MeanUsdTicks == 0 {
		return cur
	}
	pct := (float64(cur.MeanUsdTicks) - float64(prev.MeanUsdTicks)) / float64(prev.MeanUsdTicks) * 100
	cur.TrendPct = &pct
	return cur
}

func parseDashboardDays(r *http.Request) int {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days <= 0 {
		return 30
	}
	if days > 365 {
		return 365
	}
	return days
}

func (h *Handler) deliverableCosts(ctx context.Context, wsID pgtype.UUID, from, to time.Time, projectID pgtype.UUID) ([]deliverableCost, []deliverableCost, error) {
	fromTS, toTS := pgtype.Timestamptz{Time: from, Valid: true}, pgtype.Timestamptz{Time: to, Valid: true}
	issueRows, err := h.Queries.ListCompletedIssueCosts(ctx, db.ListCompletedIssueCostsParams{WorkspaceID: wsID, CompletedAt: fromTS, CompletedAt_2: toTS, ProjectID: projectID})
	if err != nil {
		return nil, nil, err
	}
	prRows, err := h.Queries.ListMergedPullRequestCosts(ctx, db.ListMergedPullRequestCostsParams{WorkspaceID: wsID, MergedAt: fromTS, MergedAt_2: toTS, ProjectID: projectID})
	if err != nil {
		return nil, nil, err
	}
	issues := make([]deliverableCost, 0, len(issueRows))
	for _, row := range issueRows {
		issues = append(issues, deliverableCost{cost: row.CostUsdTicks, uncosted: row.Uncosted})
	}
	prs := make([]deliverableCost, 0, len(prRows))
	for _, row := range prRows {
		prs = append(prs, deliverableCost{cost: row.CostUsdTicks, uncosted: row.Uncosted})
	}
	return issues, prs, nil
}

// GetDashboardCostPerDeliverable — GET /api/dashboard/cost-per-deliverable?days=&project_id=&tz=.
func (h *Handler) GetDashboardCostPerDeliverable(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	// The exact N-day cutoff, like the by-agent leaderboard: nothing here has
	// a date axis the client could trim.
	tz := h.resolveViewingTZ(r)
	since := parseExactSinceParamInTZ(r, 30, tz)
	now := time.Now()
	period := now.Sub(since.Time)
	wsUUID := parseUUID(workspaceID)

	curIssues, curPRs, err := h.deliverableCosts(r.Context(), wsUUID, since.Time, now, projectID)
	if err != nil {
		slog.Warn("cost per deliverable failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compute cost per deliverable")
		return
	}
	prevIssues, prevPRs, err := h.deliverableCosts(r.Context(), wsUUID, since.Time.Add(-period), since.Time, projectID)
	if err != nil {
		slog.Warn("cost per deliverable (previous period) failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compute cost per deliverable")
		return
	}
	writeJSON(w, http.StatusOK, CostPerDeliverableResponse{
		Days:         parseDashboardDays(r),
		Issues:       withTrend(deliverableStats(curIssues), deliverableStats(prevIssues)),
		PullRequests: withTrend(deliverableStats(curPRs), deliverableStats(prevPRs)),
	})
}
