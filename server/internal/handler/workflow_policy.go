package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Workflow selector (JEF-273): the workspace policy behind the per-task
// single / cascade / critique choice, and the 90-day run statistics the
// auto mode learns from.

const AuditWorkflowPolicySettingsChanged = "workflow_policy.settings_changed"

// GetWorkflowPolicySettings: GET /api/workflow-policy-settings — the
// workspace's workflow_policy settings, with defaults filled in.
func (h *Handler) GetWorkflowPolicySettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, service.WorkflowPolicySettings(ws.Settings))
}

// PutWorkflowPolicySettings: PUT /api/workflow-policy-settings {mode}.
func (h *Handler) PutWorkflowPolicySettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.WorkflowPolicy
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !service.ValidWorkflowPolicyMode(req.Mode) {
		writeError(w, http.StatusBadRequest, "mode must be \"off\" or \"auto\"")
		return
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
	next := service.WorkflowPolicy{Mode: req.Mode}
	settings["workflow_policy"] = next
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save workflow policy settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditWorkflowPolicySettingsChanged, "workspace", wsUUID, map[string]any{"mode": next.Mode}, nil)
	writeJSON(w, http.StatusOK, next)
}

// workflowStatsWindowDays is the lookback for the workflow-stats endpoint —
// the same 90-day window the selector decides on, so the UI shows exactly
// the data the decisions are made from.
const workflowStatsWindowDays = 90

// WorkflowStatsRow is one (task_class, workflow) aggregate line of the
// workflow statistics (JEF-273). Averages are pointers: null when no run in
// the bucket carried a cost / a start time, distinct from a genuine zero.
type WorkflowStatsRow struct {
	TaskClass       string   `json:"task_class"`
	Workflow        string   `json:"workflow"`
	Samples         int32    `json:"samples"`
	SuccessRate     float64  `json:"success_rate"`
	AvgCostUSD      *float64 `json:"avg_cost_usd"`
	AvgDurationSecs *float64 `json:"avg_duration_secs"`
}

// WorkflowStatsResponse wraps the rows with the window they were computed on.
type WorkflowStatsResponse struct {
	WindowDays int32              `json:"window_days"`
	Rows       []WorkflowStatsRow `json:"rows"`
}

// GetWorkflowStats serves GET /api/runtimes/workflow-stats: the trailing
// 90-day per-(task_class, workflow) run statistics backing the workflow
// selector, for any workspace member.
func (h *Handler) GetWorkflowStats(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}

	rows, err := h.Queries.GetWorkflowStats(r.Context(), db.GetWorkflowStatsParams{
		WorkspaceID: parseUUID(workspaceID),
		Since:       pgtype.Timestamptz{Time: time.Now().Add(-workflowStatsWindowDays * 24 * time.Hour), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow stats")
		return
	}

	resp := WorkflowStatsResponse{
		WindowDays: workflowStatsWindowDays,
		Rows:       make([]WorkflowStatsRow, 0, len(rows)),
	}
	for _, row := range rows {
		out := WorkflowStatsRow{
			TaskClass: row.TaskClass,
			Workflow:  row.Workflow,
			Samples:   row.Samples,
		}
		if row.Samples > 0 {
			out.SuccessRate = float64(row.SuccessCount) / float64(row.Samples)
		}
		if row.CostSamples > 0 {
			avg := row.TotalCostUsdTicks / float64(row.CostSamples) * costTicksPerUSD
			out.AvgCostUSD = &avg
		}
		if row.DurationSamples > 0 {
			avg := row.TotalDurationSecs / float64(row.DurationSamples)
			out.AvgDurationSecs = &avg
		}
		resp.Rows = append(resp.Rows, out)
	}
	writeJSON(w, http.StatusOK, resp)
}
