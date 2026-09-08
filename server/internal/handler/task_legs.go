package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
)

// Per-leg accounting (JEF-274). A multi-leg workflow — draft, review,
// revision, retry, fallback, escalation — is a set of separate runs, each
// with its own usage. This endpoint puts them back together: the workflow,
// leg by leg, with the totals every leg contributed to.

// legRoleDraft is what the primary leg reports on the wire. The column stores
// the empty string for it (a new producer must opt into a role, never out of
// one), but a client rendering a list of legs needs a name for the first one.
const legRoleDraft = "draft"

// WorkflowLeg is one run of a workflow with the spend it accounts for.
type WorkflowLeg struct {
	TaskID          string  `json:"task_id"`
	LegRole         string  `json:"leg_role"`
	Status          string  `json:"status"`
	AgentID         string  `json:"agent_id"`
	AgentName       string  `json:"agent_name"`
	RuntimeID       string  `json:"runtime_id"`
	RuntimeName     string  `json:"runtime_name"`
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CostUsdTicks    int64   `json:"cost_usd_ticks"`
	DurationSeconds float64 `json:"duration_seconds"`
	CreatedAt       *string `json:"created_at"`
	CompletedAt     *string `json:"completed_at"`
}

// WorkflowLegTotals is what the whole workflow cost — every leg counted, which
// is the figure a single run's usage cannot express.
type WorkflowLegTotals struct {
	Legs            int     `json:"legs"`
	CostUsdTicks    int64   `json:"cost_usd_ticks"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// WorkflowLegsResponse is GET /api/tasks/{taskId}/legs.
type WorkflowLegsResponse struct {
	RootTaskID string            `json:"root_task_id"`
	Legs       []WorkflowLeg     `json:"legs"`
	Totals     WorkflowLegTotals `json:"totals"`
}

// GetTaskLegs: GET /api/tasks/{taskId}/legs — the whole workflow the given run
// belongs to. The id may be ANY leg: the root is resolved first, so a client
// holding a review or retry run gets the same answer as one holding the draft.
func (h *Handler) GetTaskLegs(w http.ResponseWriter, r *http.Request) {
	task, _, ok := h.runReplayTask(w, r)
	if !ok {
		return
	}
	root := service.WorkflowRoot(task)
	rows, err := h.Queries.ListWorkflowLegs(r.Context(), root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the workflow legs")
		return
	}
	resp := WorkflowLegsResponse{RootTaskID: uuidToString(root), Legs: make([]WorkflowLeg, 0, len(rows))}
	for _, row := range rows {
		role := row.LegRole
		if role == "" {
			role = legRoleDraft
		}
		resp.Legs = append(resp.Legs, WorkflowLeg{
			TaskID:          uuidToString(row.ID),
			LegRole:         role,
			Status:          row.Status,
			AgentID:         uuidToString(row.AgentID),
			AgentName:       row.AgentName,
			RuntimeID:       uuidToString(row.RuntimeID),
			RuntimeName:     row.RuntimeName,
			Provider:        row.Provider,
			Model:           row.Model,
			InputTokens:     row.InputTokens,
			OutputTokens:    row.OutputTokens,
			CostUsdTicks:    row.CostUsdTicks,
			DurationSeconds: row.DurationSeconds,
			CreatedAt:       timestampToPtr(row.CreatedAt),
			CompletedAt:     timestampToPtr(row.CompletedAt),
		})
		resp.Totals.CostUsdTicks += row.CostUsdTicks
		resp.Totals.InputTokens += row.InputTokens
		resp.Totals.OutputTokens += row.OutputTokens
		resp.Totals.DurationSeconds += row.DurationSeconds
	}
	resp.Totals.Legs = len(resp.Legs)
	writeJSON(w, http.StatusOK, resp)
}
