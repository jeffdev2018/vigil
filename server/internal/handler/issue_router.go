package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Issue router (K27): the decision behind an issue's latest run, and the
// workspace thresholds. Routing itself happens at enqueue in
// service/issue_router.go.

const AuditRoutingSettingsChanged = "routing.settings_changed"

// GetIssueRoutingDecision: GET /api/issues/{id}/routing-decision.
func (h *Handler) GetIssueRoutingDecision(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	row, err := h.Queries.GetLatestIssueTaskRouting(r.Context(), issue.ID)
	if err != nil || len(row.RoutingDecision) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"decision": nil, "task_id": nil})
		return
	}
	var decision service.RoutingDecision
	if json.Unmarshal(row.RoutingDecision, &decision) != nil {
		writeJSON(w, http.StatusOK, map[string]any{"decision": nil, "task_id": uuidToString(row.ID)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": decision, "task_id": uuidToString(row.ID), "task_status": row.Status})
}

// GetRoutingSettings / PutRoutingSettings: the workspace's routing policy,
// stored under settings.routing and validated here.
func (h *Handler) GetRoutingSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, service.RoutingSettings(ws.Settings))
}

func (h *Handler) PutRoutingSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.Routing
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.EscalationFailures < 0 || req.EscalationFailures > 20 {
		writeError(w, http.StatusBadRequest, "escalation_failures must be between 0 and 20")
		return
	}
	pools := map[string]string{}
	for level, id := range req.Pools {
		if level != service.RiskLow && level != service.RiskNormal && level != service.RiskHigh {
			writeError(w, http.StatusBadRequest, "unknown risk level "+level)
			return
		}
		if id == "" {
			continue
		}
		pid, ok := parseUUIDOrBadRequest(w, id, "pool id")
		if !ok {
			return
		}
		pool, err := h.Queries.GetRuntimePool(r.Context(), pid)
		if err != nil || pool.WorkspaceID != wsUUID {
			writeError(w, http.StatusUnprocessableEntity, "pool "+id+" is not in this workspace")
			return
		}
		pools[level] = id
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	settings := map[string]any{}
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &settings)
	}
	next := service.Routing{Enabled: req.Enabled, Pools: pools, EscalationFailures: req.EscalationFailures}
	settings["routing"] = next
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save routing settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditRoutingSettingsChanged, "workspace", wsUUID, map[string]any{"enabled": next.Enabled, "pools": pools, "escalation_failures": next.EscalationFailures}, nil)
	writeJSON(w, http.StatusOK, next)
}
