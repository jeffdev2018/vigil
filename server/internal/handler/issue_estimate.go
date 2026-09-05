package handler

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// What-if estimate (K44). Before assigning, what this agent has cost and
// how long it has taken on comparable work. Comparable means the same
// domain key as K43 routing, and the competency audit trail is the only
// per-issue record of (agent, domain) — no new table, no migration.
// The numbers are the median and the interquartile range of that agent's
// completed runs; below the workspace's min_sample they are withheld
// rather than guessed.

const (
	estimateMaxCandidates  = 20
	estimateComparableRuns = 50
)

// runSample is one completed run: how long it took and what it settled at.
type runSample struct {
	DurationSeconds int64
	CostTicks       int64
}

// EstimateStats is the withheld-or-nothing shape: every number is nil
// while the sample is too small to say anything.
type EstimateStats struct {
	SampleSize          int    `json:"sample_size"`
	InsufficientHistory bool   `json:"insufficient_history"`
	MedianCostTicks     *int64 `json:"median_cost_usd_ticks"`
	CostRangeLowTicks   *int64 `json:"cost_range_low_usd_ticks"`
	CostRangeHighTicks  *int64 `json:"cost_range_high_usd_ticks"`
	MedianDuration      *int64 `json:"median_duration_seconds"`
	DurationRangeLow    *int64 `json:"duration_range_low_seconds"`
	DurationRangeHigh   *int64 `json:"duration_range_high_seconds"`
}

type EstimateCandidate struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	EstimateStats
	ExceedsBudget bool `json:"exceeds_budget"`

	// id is the same agent, kept parsed for the queries below; unexported
	// so it stays out of the response.
	id pgtype.UUID
}

// percentile reads p (0..1) off a sorted slice, interpolating between the
// two neighbours the way numpy's default does.
func percentile(sorted []int64, p float64) int64 {
	switch len(sorted) {
	case 0:
		return 0
	case 1:
		return sorted[0]
	}
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	return int64(math.Round(float64(sorted[lo]) + (pos-float64(lo))*float64(sorted[hi]-sorted[lo])))
}

// estimateFromSamples returns the median and interquartile range of the
// durations and of the costs. Duration and cost are ranked independently:
// the p25 cost need not come from the p25 run, and a range is what the
// reader is asked to believe, not a specific past run.
func estimateFromSamples(samples []runSample, minSample int) EstimateStats {
	if minSample < 1 {
		minSample = 1
	}
	out := EstimateStats{SampleSize: len(samples), InsufficientHistory: len(samples) < minSample}
	if out.InsufficientHistory {
		return out
	}
	durations := make([]int64, len(samples))
	costs := make([]int64, len(samples))
	for i, s := range samples {
		durations[i], costs[i] = s.DurationSeconds, s.CostTicks
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	sort.Slice(costs, func(i, j int) bool { return costs[i] < costs[j] })
	ptr := func(v int64) *int64 { return &v }
	out.MedianDuration = ptr(percentile(durations, 0.5))
	out.DurationRangeLow = ptr(percentile(durations, 0.25))
	out.DurationRangeHigh = ptr(percentile(durations, 0.75))
	out.MedianCostTicks = ptr(percentile(costs, 0.5))
	out.CostRangeLowTicks = ptr(percentile(costs, 0.25))
	out.CostRangeHighTicks = ptr(percentile(costs, 0.75))
	return out
}

// estimateExceedsBudget: would the median run push any applicable policy
// past its limit. A missing or failing budget service is not a refusal —
// the estimate is advisory, admission is enforced elsewhere (K03).
func (h *Handler) estimateExceedsBudget(ctx context.Context, issue db.Issue, agentID pgtype.UUID, medianCostTicks *int64) bool {
	if h.BudgetService == nil {
		return false
	}
	statuses, err := h.BudgetService.Status(ctx, service.BudgetScope{WorkspaceID: issue.WorkspaceID, ProjectID: issue.ProjectID, AgentID: agentID})
	if err != nil {
		slog.Debug("issue estimate: budget status failed", "error", err, "agent_id", uuidToString(agentID))
		return false
	}
	var cost int64
	if medianCostTicks != nil {
		cost = *medianCostTicks
	}
	for _, s := range statuses {
		if s.Policy.LimitUsdTicks <= 0 {
			continue
		}
		if s.Reached || s.SpentTicks+s.ReservedTicks+cost > s.Policy.LimitUsdTicks {
			return true
		}
	}
	return false
}

// estimateCandidates resolves the ?candidates= list, or falls back to
// every agent with a competency row in this domain.
func (h *Handler) estimateCandidates(w http.ResponseWriter, r *http.Request, issue db.Issue, domain string) ([]EstimateCandidate, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("candidates"))
	if raw == "" {
		rows, err := h.Queries.ListDomainCompetency(r.Context(), db.ListDomainCompetencyParams{WorkspaceID: issue.WorkspaceID, DomainKey: domain})
		if err != nil {
			slog.Warn("issue estimate: domain competency failed", "error", err, "issue_id", uuidToString(issue.ID))
		}
		out := make([]EstimateCandidate, 0, len(rows))
		for _, c := range rows {
			if len(out) >= estimateMaxCandidates {
				break
			}
			out = append(out, EstimateCandidate{AgentID: uuidToString(c.AgentID), AgentName: c.AgentName, id: c.AgentID})
		}
		return out, true
	}
	ids := strings.Split(raw, ",")
	if len(ids) > estimateMaxCandidates {
		writeError(w, http.StatusBadRequest, "too many candidates")
		return nil, false
	}
	out := make([]EstimateCandidate, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if id = strings.TrimSpace(id); id == "" || seen[id] {
			continue
		}
		seen[id] = true
		agentID, ok := parseUUIDOrBadRequest(w, id, "candidate id")
		if !ok {
			return nil, false
		}
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: issue.WorkspaceID})
		if err != nil {
			writeError(w, http.StatusBadRequest, "candidate is not an agent of this workspace")
			return nil, false
		}
		out = append(out, EstimateCandidate{AgentID: uuidToString(agent.ID), AgentName: agent.Name, id: agent.ID})
	}
	return out, true
}

// GetIssueEstimate: GET /api/issues/{id}/estimate?candidates=a,b — what
// each candidate has historically cost and taken on this issue's domain.
func (h *Handler) GetIssueEstimate(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	minSample := competencySettings(ws.Settings).MinSample
	domain := h.issueDomainKey(r.Context(), issue)
	candidates, ok := h.estimateCandidates(w, r, issue, domain)
	if !ok {
		return
	}
	for i := range candidates {
		rows, err := h.Queries.ListComparableRunStats(r.Context(), db.ListComparableRunStatsParams{
			WorkspaceID: issue.WorkspaceID, AgentID: candidates[i].id, DomainKey: domain, RowLimit: estimateComparableRuns,
		})
		if err != nil {
			slog.Warn("issue estimate: comparable runs failed", "error", err, "agent_id", candidates[i].AgentID)
		}
		samples := make([]runSample, 0, len(rows))
		for _, row := range rows {
			samples = append(samples, runSample{DurationSeconds: row.DurationSeconds, CostTicks: row.CostTicks})
		}
		candidates[i].EstimateStats = estimateFromSamples(samples, minSample)
		candidates[i].ExceedsBudget = h.estimateExceedsBudget(r.Context(), issue, candidates[i].id, candidates[i].MedianCostTicks)
	}
	writeJSON(w, http.StatusOK, map[string]any{"domain_key": domain, "min_sample": minSample, "candidates": candidates})
}
