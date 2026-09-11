package handler

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/pricing"
)

// A run's cost, priced the way budget settlement prices it
// (service.BudgetService.SettleTaskInTx): the provider's reported cost when a
// slice carries one, the shared catalog estimate otherwise. Summing only the
// reported ticks made every run of a CLI that reports tokens but no cost read
// "$0.00" beside an issue page estimating the same usage.

// agentCostEstimateRuns matches AvgAgentRecentTaskCostTicks: the five latest
// completed runs.
const agentCostEstimateRuns = 5

// usageCostTicks prices one usage slice; priced is false when the slice has
// neither a reported cost nor a catalog rate, so it cannot contribute a figure.
func usageCostTicks(u pricing.Usage) (ticks int64, priced bool) {
	if u.CostUSDTicks != nil && *u.CostUSDTicks >= 0 {
		return pricing.EstimateTicks(u), true
	}
	if _, ok := pricing.Resolve(u.Model, u.Provider); !ok {
		return 0, false
	}
	return pricing.EstimateTicks(u), true
}

// runCostOf prices a run from its usage slices. known is false when no slice
// could be priced — no usage at all, or only unpriced models — so callers say
// "unknown" instead of presenting a zero as a free run.
func runCostOf(usage []TaskUsageData) (ticks int64, known bool) {
	for _, u := range usage {
		t, ok := usageCostTicks(pricing.Usage{
			Provider: u.Provider, Model: u.Model,
			InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens,
			CostUSDTicks: u.CostUsdTicks,
		})
		if ok {
			ticks += t
			known = true
		}
	}
	return ticks, known
}

func int8Ptr(v pgtype.Int8) *int64 {
	if !v.Valid {
		return nil
	}
	value := v.Int64
	return &value
}

// AgentCostEstimateResponse is GET /api/agents/{id}/cost-estimate: what one run
// of this agent has recently cost, for the notice shown before a user starts
// one. AvgCostUsdTicks is null when no recent run could be priced — clients
// show "cost unknown", never a zero.
type AgentCostEstimateResponse struct {
	AgentID         string `json:"agent_id"`
	SampleRuns      int    `json:"sample_runs"`
	AvgCostUsdTicks *int64 `json:"avg_cost_usd_ticks"`
}

// GetAgentCostEstimate: GET /api/agents/{id}/cost-estimate.
func (h *Handler) GetAgentCostEstimate(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListRecentAgentTaskUsageForBudget(r.Context(), db.ListRecentAgentTaskUsageForBudgetParams{
		AgentID: agent.ID, RunLimit: agentCostEstimateRuns,
	})
	if err != nil {
		slog.Warn("agent cost estimate failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the agent's recent usage")
		return
	}
	perTask := map[pgtype.UUID]int64{}
	for _, row := range rows {
		ticks, priced := usageCostTicks(pricing.Usage{
			Provider: row.Provider, Model: row.Model,
			InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens,
			CostUSDTicks: int8Ptr(row.CostUsdTicks),
		})
		if priced {
			perTask[row.TaskID] += ticks
		}
	}
	out := AgentCostEstimateResponse{AgentID: uuidToString(agent.ID), SampleRuns: len(perTask)}
	if len(perTask) > 0 {
		var total int64
		for _, ticks := range perTask {
			total += ticks
		}
		avg := total / int64(len(perTask))
		out.AvgCostUsdTicks = &avg
	}
	writeJSON(w, http.StatusOK, out)
}
