package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Scorecards (K25): per agent and runtime, from the runs that ended each
// day — acceptance, failure, reopening, resolution without a human
// stepping in, and cost. The rollup recomputes the last days on every
// tick, so a late usage row or a reopened issue is picked up and a rerun
// never double counts. Every metric stays visible on its own; there is no
// composite score.

const (
	scorecardRollupLookback = 3 * 24 * time.Hour
	scorecardLowSample      = 10
	zeroRuntimeID           = "00000000-0000-0000-0000-000000000000"
)

var knownTerminalStatuses = map[string]bool{"completed": true, "failed": true, "cancelled": true}
var knownActiveStatuses = map[string]bool{"queued": true, "dispatched": true, "running": true, "waiting_local_directory": true}

type ScorecardTotals struct {
	RunsTotal          int   `json:"runs_total"`
	RunsFailed         int   `json:"runs_failed"`
	RunsCancelled      int   `json:"runs_cancelled"`
	RunsAccepted       int   `json:"runs_accepted"`
	RunsReopened       int   `json:"runs_reopened"`
	RunsNoIntervention int   `json:"runs_no_intervention"`
	CostUsdTicksTotal  int64 `json:"cost_usd_ticks_total"`
	// LowSample flags totals too small to read as rates.
	LowSample bool `json:"low_sample"`
}

func (t *ScorecardTotals) add(runs, failed, cancelled, accepted, reopened, noInt int, cost int64) {
	t.RunsTotal += runs
	t.RunsFailed += failed
	t.RunsCancelled += cancelled
	t.RunsAccepted += accepted
	t.RunsReopened += reopened
	t.RunsNoIntervention += noInt
	t.CostUsdTicksTotal += cost
	t.LowSample = t.RunsTotal < scorecardLowSample
}

type ScorecardDay struct {
	Day string `json:"day"`
	ScorecardTotals
}

type AgentScorecardResponse struct {
	AgentID  string          `json:"agent_id"`
	Days     int             `json:"days"`
	Totals   ScorecardTotals `json:"totals"`
	Previous ScorecardTotals `json:"previous"`
	Series   []ScorecardDay  `json:"series"`
}

type WorkspaceScorecardRow struct {
	AgentID   string `json:"agent_id"`
	RuntimeID string `json:"runtime_id,omitempty"`
	ScorecardTotals
}

// RollupAgentScorecards recomputes the last days; an unknown run status is
// an error in the log, never a silent skip.
func (h *Handler) RollupAgentScorecards(ctx context.Context, now time.Time) (int, error) {
	from := now.Add(-scorecardRollupLookback).Truncate(24 * time.Hour)
	statuses, err := h.Queries.ListTerminalTaskStatusesSince(ctx, pgtype.Timestamptz{Time: from, Valid: true})
	if err != nil {
		return 0, fmt.Errorf("list statuses: %w", err)
	}
	for _, s := range statuses {
		if !knownTerminalStatuses[s] && !knownActiveStatuses[s] {
			slog.Error("scorecard rollup: unknown run status, not counted", "status", s)
		}
	}
	n, err := h.Queries.RollupAgentScorecards(ctx, db.RollupAgentScorecardsParams{
		CompletedAt: pgtype.Timestamptz{Time: from, Valid: true}, CompletedAt_2: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("rollup: %w", err)
	}
	return int(n), nil
}

func scorecardWindow(r *http.Request) (days int, from, to, prevFrom time.Time) {
	days = 30
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d > 0 && d <= 365 {
		days = d
	}
	to = time.Now().UTC().Truncate(24 * time.Hour).Add(24 * time.Hour)
	from = to.Add(-time.Duration(days) * 24 * time.Hour)
	prevFrom = from.Add(-time.Duration(days) * 24 * time.Hour)
	return
}

func dateOf(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }

// GetAgentScorecard — GET /api/agents/{id}/scorecard?days=.
func (h *Handler) GetAgentScorecard(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	days, from, to, prevFrom := scorecardWindow(r)
	rows, err := h.Queries.ListAgentScorecardDays(r.Context(), db.ListAgentScorecardDaysParams{WorkspaceID: agent.WorkspaceID, AgentID: agent.ID, Day: dateOf(prevFrom), Day_2: dateOf(to)})
	if err != nil {
		slog.Warn("agent scorecard failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the scorecard")
		return
	}
	out := AgentScorecardResponse{AgentID: uuidToString(agent.ID), Days: days, Series: []ScorecardDay{}}
	out.Totals.LowSample, out.Previous.LowSample = true, true
	byDay := map[string]*ScorecardDay{}
	for _, row := range rows {
		day := row.Day.Time.Format("2006-01-02")
		if row.Day.Time.Before(from) {
			out.Previous.add(int(row.RunsTotal), int(row.RunsFailed), int(row.RunsCancelled), int(row.RunsAccepted), int(row.RunsReopened), int(row.RunsNoIntervention), row.CostUsdTicksTotal)
			continue
		}
		out.Totals.add(int(row.RunsTotal), int(row.RunsFailed), int(row.RunsCancelled), int(row.RunsAccepted), int(row.RunsReopened), int(row.RunsNoIntervention), row.CostUsdTicksTotal)
		d, ok := byDay[day]
		if !ok {
			out.Series = append(out.Series, ScorecardDay{Day: day})
			d = &out.Series[len(out.Series)-1]
			byDay[day] = d
		}
		d.add(int(row.RunsTotal), int(row.RunsFailed), int(row.RunsCancelled), int(row.RunsAccepted), int(row.RunsReopened), int(row.RunsNoIntervention), row.CostUsdTicksTotal)
	}
	writeJSON(w, http.StatusOK, out)
}

// ListWorkspaceScorecards — GET /api/scorecards?days=.
func (h *Handler) ListWorkspaceScorecards(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	days, from, to, _ := scorecardWindow(r)
	rows, err := h.Queries.ListWorkspaceScorecards(r.Context(), db.ListWorkspaceScorecardsParams{WorkspaceID: wsUUID, Day: dateOf(from), Day_2: dateOf(to)})
	if err != nil {
		slog.Warn("workspace scorecards failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load scorecards")
		return
	}
	out := make([]WorkspaceScorecardRow, 0, len(rows))
	for _, row := range rows {
		item := WorkspaceScorecardRow{AgentID: uuidToString(row.AgentID)}
		if rt := uuidToString(row.RuntimeID); rt != zeroRuntimeID {
			item.RuntimeID = rt
		}
		item.LowSample = true
		item.add(int(row.RunsTotal), int(row.RunsFailed), int(row.RunsCancelled), int(row.RunsAccepted), int(row.RunsReopened), int(row.RunsNoIntervention), row.CostUsdTicksTotal)
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "rows": out})
}
