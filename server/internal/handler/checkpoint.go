package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/service"
)

// Checkpoints (K20): the latest run's resume point and attempts, for the
// cockpit. The resume itself is automatic (service/checkpoint.go).

// GetRunCheckpointStatus: GET /api/issues/{id}/run/checkpoint-status.
func (h *Handler) GetRunCheckpointStatus(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	row, err := h.Queries.GetLatestIssueTaskCheckpoint(r.Context(), issue.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"run": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": map[string]any{
		"task_id": uuidToString(row.ID), "status": row.Status, "failure_reason": row.FailureReason.String,
		"last_checkpoint_seq": int8ToPtr(row.LastCheckpointSeq), "checkpointed_at": timestampToPtr(row.CheckpointedAt),
		"attempts": row.CheckpointAttempts, "max_attempts": service.CheckpointResumeMaxAttempts,
		"resumed_from_task_id": uuidToPtr(row.RetryOfTaskID), "exhausted": row.FailureReason.String == service.ReasonCheckpointResumeExhausted,
	}})
}
