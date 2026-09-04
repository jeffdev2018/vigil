package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Drift detection (K40): an observer of the run's own tool calls, run
// after each message batch is written (never before). A repeated action or
// a re-read loop stops the run with the exact reason on the task and in
// the Attention Inbox.

const (
	AuditDriftDetected = "run.drift_detected"
	DriftInboxType     = "drift_detected"
)

// checkDrift is called after a message batch that carried a tool call.
func (h *Handler) checkDrift(ctx context.Context, task db.AgentTaskQueue) {
	if task.Status != "running" || task.DriftReason.Valid {
		return
	}
	agent, err := h.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return
	}
	ws, err := h.Queries.GetWorkspace(ctx, agent.WorkspaceID)
	if err != nil {
		return
	}
	cfg := service.DriftSettings(ws.Settings)
	if !cfg.Enabled {
		return
	}
	calls, err := h.Queries.ListRecentTaskToolUses(ctx, db.ListRecentTaskToolUsesParams{TaskID: task.ID, Limit: service.DriftWindow})
	if err != nil {
		return
	}
	verdict, drifted := service.DetectDrift(cfg, calls)
	if !drifted {
		return
	}
	if err := h.Queries.SetTaskDriftReason(ctx, db.SetTaskDriftReasonParams{ID: task.ID, DriftReason: pgtype.Text{String: verdict.Reason, Valid: true}}); err != nil {
		slog.Warn("drift: mark reason failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	if _, err := h.TaskService.FailTask(ctx, task.ID, "Run stopped for drift: "+verdict.Detail, "", "", "", service.ReasonDriftDetected, false, "", ""); err != nil {
		slog.Warn("drift: stop failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	h.audit(ctx, agent.WorkspaceID, "system", "", AuditDriftDetected, "task", task.ID, map[string]any{"reason": verdict.Reason, "detail": verdict.Detail, "seqs": verdict.Seqs}, nil)
	details, _ := json.Marshal(map[string]any{"task_id": uuidToString(task.ID), "reason": verdict.Reason, "detail": verdict.Detail, "seqs": verdict.Seqs})
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, agent.WorkspaceID)
	if err != nil {
		return
	}
	title := "Run stopped for drift: " + verdict.Detail
	for _, rcpt := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: agent.WorkspaceID, RecipientType: rcpt.Type, RecipientID: rcpt.ID, Type: DriftInboxType, Severity: "attention",
			IssueID: task.IssueID, Title: title, ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(agent.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
	}
}

// GetDriftPolicy / PutDriftPolicy: the workspace thresholds (settings.drift).
func (h *Handler) GetDriftPolicy(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	writeJSON(w, http.StatusOK, service.DriftSettings(ws.Settings))
}

func (h *Handler) PutDriftPolicy(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.Drift
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.RepeatedActionThreshold < 2 || req.RepeatedActionThreshold > 100 || req.FileRereadThreshold < 2 || req.FileRereadThreshold > 100 {
		writeError(w, http.StatusBadRequest, "thresholds must be between 2 and 100")
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
	settings["drift"] = req
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save drift policy")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), "drift.policy_changed", "workspace", wsUUID, map[string]any{"enabled": req.Enabled, "repeated_action_threshold": req.RepeatedActionThreshold, "file_reread_threshold": req.FileRereadThreshold}, nil)
	writeJSON(w, http.StatusOK, req)
}
