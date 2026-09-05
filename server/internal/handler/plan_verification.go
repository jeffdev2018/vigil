package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ErrCodePlanVerificationCritical is the stable 409 code a client can key on
// when the done gate refuses a status change.
const ErrCodePlanVerificationCritical = "plan_verification_critical"

// planVerificationBlocksDone is true when the workspace gate is on, the target
// status behaves as done, and the newest report on the ACTIVE plan carries a
// critical finding. A new plan version or a clean report lifts it.
func (h *Handler) planVerificationBlocksDone(ctx context.Context, issue db.Issue, statusKey string) (bool, error) {
	if issuestatus.Effective(ctx, h.Queries, issue.WorkspaceID, statusKey) != issuestatus.Done {
		return false, nil
	}
	ws, err := h.Queries.GetWorkspace(ctx, issue.WorkspaceID)
	if err != nil {
		return false, fmt.Errorf("load workspace: %w", err)
	}
	if !service.PlanVerificationGateEnabled(ws.Settings) {
		return false, nil
	}
	latest, err := h.Queries.GetLatestReportedPlanVerification(ctx, issue.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load verification: %w", err)
	}
	return latest.CriticalCount > 0, nil
}

// planVerificationAllowsStatus writes the 409 and returns false when the gate
// refuses; a read failure is a 500 rather than a silent pass, because the gate
// exists to be trusted.
func (h *Handler) planVerificationAllowsStatus(w http.ResponseWriter, r *http.Request, issue db.Issue, statusKey string) bool {
	blocked, err := h.planVerificationBlocksDone(r.Context(), issue, statusKey)
	if err != nil {
		slog.Warn("plan verification gate failed", "error", err, "issue_id", uuidToString(issue.ID))
		writeError(w, http.StatusInternalServerError, "failed to evaluate plan verification gate")
		return false
	}
	if blocked {
		writeErrorCode(w, http.StatusConflict, ErrCodePlanVerificationCritical,
			"plan verification found a critical divergence; publish a new plan version or a clean verification before marking done")
		return false
	}
	return true
}

// postPlanVerificationComment doubles the report as a system comment so the
// issue timeline keeps a copy the sidebar cannot lose.
func (h *Handler) postPlanVerificationComment(r *http.Request, issue db.Issue, v db.PlanVerification, findings []PlanFinding, actorType, actorID string) {
	ctx := r.Context()
	var b strings.Builder
	fmt.Fprintf(&b, "Plan verification (plan v%d): %d critical, %d major, %d minor, %d outdated.", v.PlanVersion, v.CriticalCount, v.MajorCount, v.MinorCount, v.OutdatedCount)
	if v.Summary.Valid && strings.TrimSpace(v.Summary.String) != "" {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(v.Summary.String))
	}
	if len(findings) == 0 {
		b.WriteString("\n\nNo divergence from the plan.")
	}
	for i, f := range findings {
		if i >= 10 {
			fmt.Fprintf(&b, "\n- …and %d more in the Plan verification section.", len(findings)-10)
			break
		}
		fmt.Fprintf(&b, "\n- **%s** — %s", strings.ToUpper(f.Severity), f.Title)
	}

	created, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     b.String(),
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("plan verification: create system comment failed", "error", err, "issue_id", uuidToString(issue.ID))
		return
	}
	comment := created.Comment()
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"comment":        commentToResponse(comment, nil, nil),
		"issue_title":    issue.Title,
		"issue_revision": created.IssueRevision,
	})
}
