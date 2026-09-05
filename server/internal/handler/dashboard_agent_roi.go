package handler

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ROI per agent (JEF-252): what each agent cost against what it delivered over
// the dashboard's period, so "keep paying for this one" is one line rather than
// a spreadsheet. Read-only aggregation, computed like the other dashboard
// endpoints, with the previous period of equal length carried alongside for a
// trend.
//
// An agent that spent without closing anything keeps its row with a null ratio:
// dropping it would hide the exact case the buyer is looking for.

type AgentRoiRow struct {
	AgentID      string `json:"agent_id"`
	AgentName    string `json:"agent_name"`
	Provider     string `json:"provider"`
	IssuesClosed int64  `json:"issues_closed"`
	PRsMerged    int64  `json:"prs_merged"`
	CostUSDTicks int64  `json:"cost_usd_ticks"`
	// UncostedRuns is how many of this agent's runs carry usage rows without
	// a price: its cost is a floor, not a total.
	UncostedRuns int64 `json:"uncosted_runs"`
	// Ratios are nil rather than zero when there is no deliverable to divide
	// by — an agent that closed nothing has no cost per issue, and rendering
	// that as $0.00 would read as the cheapest agent in the workspace.
	CostPerIssueUSDTicks     *int64 `json:"cost_per_issue_usd_ticks"`
	CostPerPRUSDTicks        *int64 `json:"cost_per_pr_usd_ticks"`
	PrevCostPerIssueUSDTicks *int64 `json:"prev_cost_per_issue_usd_ticks"`
}

type DashboardAgentRoiResponse struct {
	Days   int           `json:"days"`
	Agents []AgentRoiRow `json:"agents"`
}

// costPer divides cost by a deliverable count, or nil when there is nothing to
// divide by.
func costPer(cost, count int64) *int64 {
	if count <= 0 {
		return nil
	}
	v := cost / count
	return &v
}

// foldRestrictedAgentRoi merges every agent the viewer may not name into one
// bucket, exactly like the by-agent leaderboard (see foldRestrictedAgents). The
// bucket keeps the counts and the cost so the card still adds up, and drops the
// name and provider, which are the two fields that would leak.
func foldRestrictedAgentRoi(rows []AgentRoiRow, restricted map[string]struct{}) []AgentRoiRow {
	return foldRestrictedAgents(
		rows,
		restricted,
		func(row AgentRoiRow) string { return row.AgentID },
		func(row AgentRoiRow) (AgentRoiRow, struct{}) {
			row.AgentID = restrictedAgentsRowID
			row.AgentName = ""
			row.Provider = ""
			return row, struct{}{}
		},
		func(dst, src AgentRoiRow) AgentRoiRow {
			dst.IssuesClosed += src.IssuesClosed
			dst.PRsMerged += src.PRsMerged
			dst.CostUSDTicks += src.CostUSDTicks
			dst.UncostedRuns += src.UncostedRuns
			return dst
		},
	)
}

// agentRoiRows returns the folded per-agent aggregate for one window. Ratios
// are left nil here: they are computed after folding, so the bucket's ratio is
// its own merged cost over its own merged count rather than a sum of ratios.
func (h *Handler) agentRoiRows(
	ctx context.Context,
	wsID pgtype.UUID,
	from, to time.Time,
	projectID pgtype.UUID,
	restricted map[string]struct{},
) ([]AgentRoiRow, error) {
	rows, err := h.Queries.ListAgentRoiRows(ctx, db.ListAgentRoiRowsParams{
		WorkspaceID: wsID,
		PeriodStart: pgtype.Timestamptz{Time: from, Valid: true},
		PeriodEnd:   pgtype.Timestamptz{Time: to, Valid: true},
		ProjectID:   projectID,
	})
	if err != nil {
		return nil, err
	}
	out := make([]AgentRoiRow, len(rows))
	for i, row := range rows {
		out[i] = AgentRoiRow{
			AgentID:      uuidToString(row.ID),
			AgentName:    row.Name,
			Provider:     row.Provider,
			IssuesClosed: row.IssuesClosed,
			PRsMerged:    row.PrsMerged,
			CostUSDTicks: row.CostUsdTicks,
			UncostedRuns: row.UncostedRuns,
		}
	}
	return foldRestrictedAgentRoi(out, restricted), nil
}

// GetDashboardAgentRoi — GET /api/dashboard/roi-by-agent?days=&project_id=&tz=.
func (h *Handler) GetDashboardAgentRoi(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	projectID, ok := parseProjectIDParam(w, r)
	if !ok {
		return
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}
	// Exact N-day cutoff for the same reason as the by-agent leaderboard: the
	// response carries no date axis the client could trim back.
	tz := h.resolveViewingTZ(r)
	since := parseExactSinceParamInTZ(r, 30, tz)
	now := time.Now()
	period := now.Sub(since.Time)
	wsUUID := parseUUID(workspaceID)

	agents, err := h.agentRoiRows(r.Context(), wsUUID, since.Time, now, projectID, restricted)
	if err != nil {
		slog.Warn("roi by agent failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compute roi by agent")
		return
	}
	prev, err := h.agentRoiRows(r.Context(), wsUUID, since.Time.Add(-period), since.Time, projectID, restricted)
	if err != nil {
		slog.Warn("roi by agent (previous period) failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compute roi by agent")
		return
	}
	prevByAgent := make(map[string]*int64, len(prev))
	for _, row := range prev {
		prevByAgent[row.AgentID] = costPer(row.CostUSDTicks, row.IssuesClosed)
	}
	for i := range agents {
		agents[i].CostPerIssueUSDTicks = costPer(agents[i].CostUSDTicks, agents[i].IssuesClosed)
		agents[i].CostPerPRUSDTicks = costPer(agents[i].CostUSDTicks, agents[i].PRsMerged)
		agents[i].PrevCostPerIssueUSDTicks = prevByAgent[agents[i].AgentID]
	}
	// Cheapest per closed issue first — that is the ranking the decision is
	// made on. Agents that closed nothing have no ratio and sort last, ordered
	// by what they burned so the most expensive dead weight is on top of them.
	sort.SliceStable(agents, func(i, j int) bool {
		a, b := agents[i], agents[j]
		if (a.CostPerIssueUSDTicks == nil) != (b.CostPerIssueUSDTicks == nil) {
			return b.CostPerIssueUSDTicks == nil
		}
		if a.CostPerIssueUSDTicks != nil && *a.CostPerIssueUSDTicks != *b.CostPerIssueUSDTicks {
			return *a.CostPerIssueUSDTicks < *b.CostPerIssueUSDTicks
		}
		return a.CostUSDTicks > b.CostUSDTicks
	})
	writeJSON(w, http.StatusOK, DashboardAgentRoiResponse{Days: parseDashboardDays(r), Agents: agents})
}
