package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// MergeTriageItem resolves one pending item as a duplicate of an issue the
// human picked, instead of creating a second issue for the same work. The
// target issue gets a system comment naming what was folded into it, so the
// merge is visible where the work actually lives.
func (h *Handler) MergeTriageItem(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	itemID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var req struct {
		IssueID string `json:"issue_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.IssueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	// The target may arrive as a UUID or a human identifier (MUL-42); the
	// loader resolves both and enforces workspace membership.
	issue, ok := h.loadIssueForUser(w, r, req.IssueID)
	if !ok {
		return
	}

	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to merge triage item")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	item, err := qtx.LockTriageItemForResolution(ctx, db.LockTriageItemForResolutionParams{
		ID: itemID, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "triage item not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to merge triage item")
		return
	}
	if item.State != triage.StatePending {
		writeError(w, http.StatusConflict, "triage item was already resolved")
		return
	}
	if _, err := qtx.MergePendingTriageItem(ctx, db.MergePendingTriageItemParams{
		ID:                 item.ID,
		WorkspaceID:        workspaceID,
		DuplicateOfIssueID: issue.ID,
		ResolvedBy:         parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to merge triage item")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to merge triage item")
		return
	}

	h.postTriageMergeComment(r, issue, item.Title)
	h.publishTriageResolved(workspaceID, item.ID, triage.StateMerged)

	prefix := h.getIssuePrefix(ctx, workspaceID)
	writeJSON(w, http.StatusOK, map[string]any{
		"item_id":                    util.UUIDToString(itemID),
		"state":                      triage.StateMerged,
		"duplicate_of_issue_id":      util.UUIDToString(issue.ID),
		"duplicate_issue_identifier": fmt.Sprintf("%s-%d", prefix, issue.Number),
	})
}

// postTriageMergeComment records the merge on the target issue. Same shape as
// every other platform notice: author_type='system' with the zero UUID as
// author_id — clients branch on author_type, not on the UUID value.
func (h *Handler) postTriageMergeComment(r *http.Request, issue db.Issue, itemTitle string) {
	ctx := r.Context()
	created, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     "Merged from triage: " + itemTitle,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("triage merge: create system comment failed",
			"error", err, "issue_id", uuidToString(issue.ID))
		return
	}
	comment := created.Comment()
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
		"issue_revision":      created.IssueRevision,
	})
}
