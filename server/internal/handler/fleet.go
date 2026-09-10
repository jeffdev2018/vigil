package handler

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---------------------------------------------------------------------------
// Fleet reads (JEF-12)
//
// Three small, stable aggregations over EXISTING dashboard/agent queries, so
// the Mika agent can answer "quels agents tournent / combien a coûté X /
// historique" without scraping six dashboard endpoints:
//
//   GET /api/fleet/status   who's running now + per-agent task counts
//   GET /api/fleet/cost     USD (in ticks) per agent over the window
//   GET /api/fleet/history  daily per-agent activity buckets
//
// All three are member reads accepting ?since=<RFC3339|YYYY-MM-DD> and
// ?agent_id=<uuid>. Agents the caller may not see are FOLDED onto the
// __restricted_agents__ sentinel via foldRestrictedAgents (never dropped), so
// the totals keep reconciling with the workspace-level dashboard series.
// Exception: a caller that names a restricted agent via ?agent_id gets an
// empty list, not that agent's numbers under the sentinel — folding answers
// "how much did hidden agents do in aggregate", it must not become a way to
// interrogate one hidden agent by id.
// ---------------------------------------------------------------------------

// parseFleetSinceParam reads ?since= as RFC3339 or a bare calendar date.
// Absent means zero time ("no trim"); a malformed value writes a 400 and
// returns ok=false.
func parseFleetSinceParam(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("since"))
	if raw == "" {
		return time.Time{}, true
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, true
	}
	writeError(w, http.StatusBadRequest, "invalid since: expected RFC3339 or YYYY-MM-DD")
	return time.Time{}, false
}

// fleetAgentFilter reads the optional ?agent_id= filter. Absent returns
// Valid=false; malformed writes a 400 and returns ok=false.
func fleetAgentFilter(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	if raw == "" {
		return pgtype.UUID{}, true
	}
	id, ok := parseUUIDOrBadRequest(w, raw, "agent_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	return id, true
}

// fleetRestrictedFilter applies the ?agent_id= visibility rule: a named agent
// the caller may not see yields an immediate empty 200, never a fold.
// Returns ok=false with the response already written in that case.
func fleetRestrictedFilter(w http.ResponseWriter, agentFilter pgtype.UUID, restricted map[string]struct{}) bool {
	if agentFilter.Valid {
		if _, hidden := restricted[uuidToString(agentFilter)]; hidden {
			writeJSON(w, http.StatusOK, []any{})
			return false
		}
	}
	return true
}

func fleetAgentMatch(agentFilter pgtype.UUID, agentID string) bool {
	return !agentFilter.Valid || uuidToString(agentFilter) == agentID
}

// FleetStatusRow is one agent's live + windowed workload. Name is empty on
// the restricted bucket: folding merges agents whose names the caller may not
// know. TaskCount counts terminal tasks (completed + failed) anchored on
// completed_at inside the window; FailedCount is a subset of it.
type FleetStatusRow struct {
	AgentID          string `json:"agent_id"`
	Name             string `json:"name,omitempty"`
	RunningTaskCount int32  `json:"running_task_count"`
	TaskCount        int32  `json:"task_count"`
	FailedCount      int32  `json:"failed_count"`
}

// fleetSinceOrDefault returns the parsed ?since=, defaulting to 30 days ago —
// the same window the activity feeder's dashboard sibling hard-wires.
func fleetSinceOrDefault(since time.Time) time.Time {
	if since.IsZero() {
		return time.Now().Add(-30 * 24 * time.Hour)
	}
	return since
}

// GetFleetStatus handles GET /api/fleet/status: per-agent running task count
// (ListWorkspaceWorkingAgents) joined with terminal-task counts over the
// window (ListFleetAgentActivityDaily; ?since= defaults to 30 days).
func (h *Handler) GetFleetStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}
	since, ok := parseFleetSinceParam(w, r)
	if !ok {
		return
	}
	agentFilter, ok := fleetAgentFilter(w, r)
	if !ok {
		return
	}
	if !fleetRestrictedFilter(w, agentFilter, restricted) {
		return
	}

	ctx := r.Context()
	wsUUID := parseUUID(workspaceID)

	working, err := h.Queries.ListWorkspaceWorkingAgents(ctx, db.ListWorkspaceWorkingAgentsParams{
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list working agents")
		return
	}
	activity, err := h.Queries.ListFleetAgentActivityDaily(ctx, db.ListFleetAgentActivityDailyParams{
		WorkspaceID: wsUUID,
		Since:       pgtype.Timestamptz{Time: fleetSinceOrDefault(since), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent activity")
		return
	}
	agents, err := h.Queries.ListAllAgents(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	names := make(map[string]string, len(agents))
	for _, a := range agents {
		names[uuidToString(a.ID)] = a.Name
	}

	rowsByAgent := make(map[string]*FleetStatusRow)
	rowFor := func(agentID string) *FleetStatusRow {
		row, ok := rowsByAgent[agentID]
		if !ok {
			row = &FleetStatusRow{AgentID: agentID, Name: names[agentID]}
			rowsByAgent[agentID] = row
		}
		return row
	}
	for _, wrow := range working {
		agentID := uuidToString(wrow.ID)
		if !fleetAgentMatch(agentFilter, agentID) {
			continue
		}
		rowFor(agentID).RunningTaskCount = wrow.RunningTaskCount
	}
	for _, arow := range activity {
		agentID := uuidToString(arow.AgentID)
		if !fleetAgentMatch(agentFilter, agentID) {
			continue
		}
		row := rowFor(agentID)
		row.TaskCount += arow.TaskCount
		row.FailedCount += arow.FailedCount
	}

	rows := make([]FleetStatusRow, 0, len(rowsByAgent))
	for _, row := range rowsByAgent {
		rows = append(rows, *row)
	}
	// Deterministic order: map iteration is random, and this response feeds an
	// LLM skill whose answers should be reproducible.
	sort.Slice(rows, func(i, j int) bool { return rows[i].AgentID < rows[j].AgentID })

	writeJSON(w, http.StatusOK, foldRestrictedFleetStatus(rows, restricted))
}

func foldRestrictedFleetStatus(rows []FleetStatusRow, restricted map[string]struct{}) []FleetStatusRow {
	return foldRestrictedAgents(
		rows,
		restricted,
		func(row FleetStatusRow) string { return row.AgentID },
		func(row FleetStatusRow) (FleetStatusRow, struct{}) {
			row.AgentID = restrictedAgentsRowID
			row.Name = ""
			return row, struct{}{}
		},
		func(dst, src FleetStatusRow) FleetStatusRow {
			dst.RunningTaskCount += src.RunningTaskCount
			dst.TaskCount += src.TaskCount
			dst.FailedCount += src.FailedCount
			return dst
		},
	)
}

// FleetCostRow is one agent's provider-reported spend over the window,
// aggregated across models: the per-model split the dashboard serves exists
// so its client can price rows, but this endpoint's costs are already
// authoritative (cost_usd_ticks, 1e-10 USD), so a single per-agent sum is
// the stable shape the fleet skill consumes.
type FleetCostRow struct {
	AgentID      string `json:"agent_id"`
	CostUSDTicks int64  `json:"cost_usd_ticks"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TaskCount    int32  `json:"task_count"`
}

// GetFleetCost handles GET /api/fleet/cost: per-agent cost over the window
// (ListDashboardUsageByAgent, summed across provider/model). ?since= defaults
// to 30 days ago, matching the feeder's dashboard window.
func (h *Handler) GetFleetCost(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}
	since, ok := parseFleetSinceParam(w, r)
	if !ok {
		return
	}
	if since.IsZero() {
		since = time.Now().Add(-30 * 24 * time.Hour)
	}
	agentFilter, ok := fleetAgentFilter(w, r)
	if !ok {
		return
	}
	if !fleetRestrictedFilter(w, agentFilter, restricted) {
		return
	}

	rows, err := h.Queries.ListDashboardUsageByAgent(r.Context(), db.ListDashboardUsageByAgentParams{
		WorkspaceID: parseUUID(workspaceID),
		Since:       pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list usage by agent")
		return
	}

	rowsByAgent := make(map[string]*FleetCostRow)
	for _, urow := range rows {
		agentID := uuidToString(urow.AgentID)
		if !fleetAgentMatch(agentFilter, agentID) {
			continue
		}
		row, ok := rowsByAgent[agentID]
		if !ok {
			row = &FleetCostRow{AgentID: agentID}
			rowsByAgent[agentID] = row
		}
		row.CostUSDTicks += urow.CostUsdTicks
		row.InputTokens += urow.InputTokens
		row.OutputTokens += urow.OutputTokens
		row.TaskCount += urow.TaskCount
	}

	out := make([]FleetCostRow, 0, len(rowsByAgent))
	for _, row := range rowsByAgent {
		out = append(out, *row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })

	writeJSON(w, http.StatusOK, foldRestrictedFleetCost(out, restricted))
}

func foldRestrictedFleetCost(rows []FleetCostRow, restricted map[string]struct{}) []FleetCostRow {
	return foldRestrictedAgents(
		rows,
		restricted,
		func(row FleetCostRow) string { return row.AgentID },
		func(row FleetCostRow) (FleetCostRow, struct{}) {
			row.AgentID = restrictedAgentsRowID
			return row, struct{}{}
		},
		func(dst, src FleetCostRow) FleetCostRow {
			dst.CostUSDTicks += src.CostUSDTicks
			dst.InputTokens += src.InputTokens
			dst.OutputTokens += src.OutputTokens
			dst.TaskCount += src.TaskCount
			return dst
		},
	)
}

// FleetHistoryRow is one (date, agent) activity bucket, anchored on
// completed_at in UTC days — the same buckets the Agents-list sparkline
// renders. Days with no completions produce no row.
type FleetHistoryRow struct {
	Date        string `json:"date"`
	AgentID     string `json:"agent_id"`
	TaskCount   int32  `json:"task_count"`
	FailedCount int32  `json:"failed_count"`
}

// GetFleetHistory handles GET /api/fleet/history: daily per-agent activity
// buckets (ListFleetAgentActivityDaily, UTC calendar days; ?since= defaults to
// 30 days).
func (h *Handler) GetFleetHistory(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	restricted, ok := h.dashboardRestrictedAgents(w, r, workspaceID, member.Role)
	if !ok {
		return
	}
	since, ok := parseFleetSinceParam(w, r)
	if !ok {
		return
	}
	agentFilter, ok := fleetAgentFilter(w, r)
	if !ok {
		return
	}
	if !fleetRestrictedFilter(w, agentFilter, restricted) {
		return
	}

	rows, err := h.Queries.ListFleetAgentActivityDaily(r.Context(), db.ListFleetAgentActivityDailyParams{
		WorkspaceID: parseUUID(workspaceID),
		Since:       pgtype.Timestamptz{Time: fleetSinceOrDefault(since), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get agent activity")
		return
	}

	resp := make([]FleetHistoryRow, 0, len(rows))
	for _, row := range rows {
		agentID := uuidToString(row.AgentID)
		if !fleetAgentMatch(agentFilter, agentID) {
			continue
		}
		resp = append(resp, FleetHistoryRow{
			Date:        row.Date,
			AgentID:     agentID,
			TaskCount:   row.TaskCount,
			FailedCount: row.FailedCount,
		})
	}

	writeJSON(w, http.StatusOK, foldRestrictedFleetHistory(resp, restricted))
}

// The restricted bucket keeps its date split: history without dates is
// useless, and the workspace-level daily totals are already unfiltered.
func foldRestrictedFleetHistory(rows []FleetHistoryRow, restricted map[string]struct{}) []FleetHistoryRow {
	return foldRestrictedAgents(
		rows,
		restricted,
		func(row FleetHistoryRow) string { return row.AgentID },
		func(row FleetHistoryRow) (FleetHistoryRow, string) {
			date := row.Date
			row.AgentID = restrictedAgentsRowID
			return row, date
		},
		func(dst, src FleetHistoryRow) FleetHistoryRow {
			dst.TaskCount += src.TaskCount
			dst.FailedCount += src.FailedCount
			return dst
		},
	)
}
