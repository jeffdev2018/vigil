package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Bounded workflows (JEF-275): the workspace-level ceiling on how far one
// workflow may grow. See service/workflow_limits.go for what it bounds.

// AuditWorkflowLimits records a change to the ceiling.
const AuditWorkflowLimits = "workflow_limits"

// workflowLimitsResponse is the setting plus the range a client may submit, so
// the form does not carry its own copy of the bounds.
type workflowLimitsResponse struct {
	service.WorkflowLimits
	MinLegs        int `json:"min_legs"`
	MaxLegsAllowed int `json:"max_legs_allowed"`
}

func workflowLimitsBody(limits service.WorkflowLimits) workflowLimitsResponse {
	minLegs, maxLegs := service.WorkflowLimitsRange()
	return workflowLimitsResponse{WorkflowLimits: limits, MinLegs: minLegs, MaxLegsAllowed: maxLegs}
}

// GetWorkflowLimits: GET /api/workflow-limits.
func (h *Handler) GetWorkflowLimits(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, workflowLimitsBody(service.WorkflowLimitsFromSettings(ws.Settings)))
}

// PutWorkflowLimits: PUT /api/workflow-limits {max_legs, max_cost_usd_ticks}.
func (h *Handler) PutWorkflowLimits(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.WorkflowLimits
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || !service.ValidWorkflowLimits(req) {
		minLegs, maxLegs := service.WorkflowLimitsRange()
		writeError(w, http.StatusBadRequest, workflowLimitsRangeMessage(minLegs, maxLegs))
		return
	}
	if _, err := h.Queries.GetWorkspace(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	// Merged server-side: a read-modify-write of the whole blob lost the
	// writes of any concurrent settings PUT.
	patch := map[string]any{}
	patch["workflow_limits"] = req
	raw, _ := json.Marshal(patch)
	if _, err := h.Queries.MergeWorkspaceSettings(r.Context(), db.MergeWorkspaceSettingsParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the workflow limits")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditWorkflowLimits, "workspace", wsUUID,
		map[string]any{"max_legs": req.MaxLegs, "max_cost_usd_ticks": req.MaxCostUsdTicks}, nil)
	writeJSON(w, http.StatusOK, workflowLimitsBody(req))
}

// workflowLimitsRangeMessage states both bounds in one sentence.
func workflowLimitsRangeMessage(minLegs, maxLegs int) string {
	return fmt.Sprintf("max_legs must be between %d and %d, and max_cost_usd_ticks must not be negative", minLegs, maxLegs)
}
