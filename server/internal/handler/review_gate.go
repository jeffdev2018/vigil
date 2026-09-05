package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Review gate (JEF-238): when the project's review config enables it (and the
// workspace cross-review policy covers the project), moving an issue into a
// done-category status requires the latest cross-provider review of its
// latest completed worker run to be an approve. Without a project config the
// review stays the non-blocking signal it was in K15.

// reviewGateBlocksDone returns the refusal reason, or "" when the move is
// allowed; an error means the gate itself could not be evaluated.
func (h *Handler) reviewGateBlocksDone(ctx context.Context, issue db.Issue, statusKey string) (string, error) {
	if issuestatus.Effective(ctx, h.Queries, issue.WorkspaceID, statusKey) != issuestatus.Done {
		return "", nil
	}
	if !issue.ProjectID.Valid {
		return "", nil
	}
	cfg, err := h.Queries.GetProjectReviewConfig(ctx, issue.ProjectID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("load project review config: %w", err)
	}
	if !cfg.GateEnabled {
		return "", nil
	}
	ws, err := h.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return "", fmt.Errorf("load workspace: %w", err)
	}
	if !service.CrossReviewSettings(ws.Settings).Allows(uuidToString(issue.ProjectID)) {
		return "", nil // no review ever runs for this project: nothing to gate on
	}
	worker, err := h.Queries.GetLatestCompletedWorkerTaskForIssue(ctx, issue.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "review gate: awaiting review", nil
	}
	if err != nil {
		return "", fmt.Errorf("load latest worker run: %w", err)
	}
	review, err := h.Queries.GetLatestCrossReviewForTask(ctx, worker.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "review gate: awaiting review", nil
	}
	if err != nil {
		return "", fmt.Errorf("load latest review: %w", err)
	}
	message, err := h.Queries.GetLatestReviewReportMessage(ctx, review.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "review gate: awaiting review", nil
	}
	if err != nil {
		return "", fmt.Errorf("load review report: %w", err)
	}
	var report CrossReviewReport
	if err := json.Unmarshal([]byte(message.Content.String), &report); err != nil {
		return "", fmt.Errorf("parse review report: %w", err)
	}
	if report.Verdict != "approve" {
		return "review gate: latest review verdict is " + report.Verdict, nil
	}
	return "", nil
}

// reviewGateAllowsStatus writes the 409 and returns false when the gate
// refuses; like the plan verification gate, a read failure is a 500 rather
// than a silent pass, because the gate exists to be trusted.
func (h *Handler) reviewGateAllowsStatus(w http.ResponseWriter, r *http.Request, issue db.Issue, statusKey string) bool {
	reason, err := h.reviewGateBlocksDone(r.Context(), issue, statusKey)
	if err != nil {
		slog.Warn("review gate failed", "error", err, "issue_id", uuidToString(issue.ID))
		writeError(w, http.StatusInternalServerError, "failed to evaluate review gate")
		return false
	}
	if reason != "" {
		writeError(w, http.StatusConflict, reason)
		return false
	}
	return true
}
