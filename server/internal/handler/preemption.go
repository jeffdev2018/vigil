package handler

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Preemption (K41): the history a human reads on an issue — which runs
// were suspended, for whom, and whether they resumed.

type PreemptionResponse struct {
	TaskID                string  `json:"task_id"`
	Status                string  `json:"status"`
	PreemptedAt           string  `json:"preempted_at"`
	PreemptedByTaskID     string  `json:"preempted_by_task_id"`
	PreemptedByIssueID    *string `json:"preempted_by_issue_id"`
	PreemptedByIdentifier *string `json:"preempted_by_identifier"`
	ResumedByTaskID       *string `json:"resumed_by_task_id"`
}

// ListIssuePreemptions: GET /api/issues/{id}/preemptions.
func (h *Handler) ListIssuePreemptions(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListIssuePreemptions(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list preemptions")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	out := make([]PreemptionResponse, 0, len(rows))
	for _, t := range rows {
		p := PreemptionResponse{TaskID: uuidToString(t.ID), Status: t.Status, PreemptedAt: timestampToString(t.PreemptedAt), PreemptedByTaskID: uuidToString(t.PreemptedByTaskID), ResumedByTaskID: uuidToPtr(t.ResumedByTaskID)}
		if by, err := h.Queries.GetAgentTask(r.Context(), t.PreemptedByTaskID); err == nil && by.IssueID.Valid {
			if other, err := h.Queries.GetIssue(r.Context(), by.IssueID); err == nil {
				id := uuidToString(other.ID)
				ident := fmt.Sprintf("%s-%d", prefix, other.Number)
				p.PreemptedByIssueID, p.PreemptedByIdentifier = &id, &ident
			}
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"preemptions": out})
}

var _ = db.AgentTaskQueue{}
