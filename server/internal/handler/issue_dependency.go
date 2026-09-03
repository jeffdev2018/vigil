package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// issueDependencyStackDepth bounds the recursive walk used by the anti-cycle
// check and the PR stack. Ten levels is far past any real chain of blockers.
const issueDependencyStackDepth = 10

const (
	dependencyBlocks    = "blocks"
	dependencyBlockedBy = "blocked_by"
	dependencyRelated   = "related"
)

// IssueDependencyResponse is one relation seen from the requested issue:
// Type is relative to it ("blocks" means the requested issue blocks Issue).
type IssueDependencyResponse struct {
	ID    string        `json:"id"`
	Type  string        `json:"type"`
	Issue IssueResponse `json:"issue"`
}

type IssueDependenciesResponse struct {
	Blocks    []IssueDependencyResponse `json:"blocks"`
	BlockedBy []IssueDependencyResponse `json:"blocked_by"`
	Related   []IssueDependencyResponse `json:"related"`
}

func (h *Handler) ListIssueDependencies(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}

	rows, err := h.Queries.ListIssueDependenciesForIssue(r.Context(), issue.ID)
	if err != nil {
		slog.Warn("list issue dependencies failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to list dependencies")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	fill := h.newStatusCategoryFiller(r.Context(), issue.WorkspaceID)
	resp := IssueDependenciesResponse{
		Blocks:    []IssueDependencyResponse{},
		BlockedBy: []IssueDependencyResponse{},
		Related:   []IssueDependencyResponse{},
	}
	for _, row := range rows {
		other := issueToResponse(row.Issue, prefix)
		fill(&other)
		item := IssueDependencyResponse{ID: uuidToString(row.ID), Type: row.Direction, Issue: other}
		switch row.Direction {
		case dependencyBlocks:
			resp.Blocks = append(resp.Blocks, item)
		case dependencyBlockedBy:
			resp.BlockedBy = append(resp.BlockedBy, item)
		default:
			resp.Related = append(resp.Related, item)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) CreateIssueDependency(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		TargetIssueID string `json:"target_issue_id"`
		Type          string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TargetIssueID == "" {
		writeError(w, http.StatusBadRequest, "target_issue_id is required")
		return
	}
	switch req.Type {
	case dependencyBlocks, dependencyBlockedBy, dependencyRelated:
	default:
		writeError(w, http.StatusBadRequest, "type must be blocks, blocked_by or related")
		return
	}

	// Same loader as the path issue: a target outside the workspace is a 404,
	// never a hint that it exists.
	target, ok := h.loadIssueForUser(w, r, req.TargetIssueID)
	if !ok {
		return
	}
	if target.ID == issue.ID {
		writeError(w, http.StatusBadRequest, "an issue cannot depend on itself")
		return
	}

	// Normalize to the single stored direction: blocked_by is blocks read from
	// the other side.
	from, to, storedType := issue, target, req.Type
	if req.Type == dependencyBlockedBy {
		from, to, storedType = target, issue, dependencyBlocks
	}

	ctx := r.Context()
	if h.issueDependencyExists(ctx, from.ID, to.ID, storedType) ||
		(storedType == dependencyRelated && h.issueDependencyExists(ctx, to.ID, from.ID, storedType)) {
		writeError(w, http.StatusConflict, "dependency already exists")
		return
	}
	if storedType == dependencyBlocks {
		// "from blocks to" closes a loop when `to` already blocks `from`,
		// directly or through the chain below it.
		stack, err := h.Queries.ListIssueDependencyStack(ctx, db.ListIssueDependencyStackParams{
			IssueID:  to.ID,
			MaxDepth: issueDependencyStackDepth,
		})
		if err != nil {
			slog.Warn("issue dependency cycle check failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to check dependency cycle")
			return
		}
		for _, s := range stack {
			if s.IssueID == from.ID {
				writeError(w, http.StatusConflict, "dependency would create a cycle")
				return
			}
		}
	}

	dep, err := h.Queries.CreateIssueDependency(ctx, db.CreateIssueDependencyParams{
		IssueID:          from.ID,
		DependsOnIssueID: to.ID,
		Type:             storedType,
	})
	if err != nil {
		slog.Warn("create issue dependency failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to create dependency")
		return
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publishIssueDependencyChange(r, actorType, actorID, issue.WorkspaceID, issue.ID, target.ID)

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	other := issueToResponse(target, prefix)
	h.fillStatusCategory(ctx, issue.WorkspaceID, &other)
	writeJSON(w, http.StatusCreated, IssueDependencyResponse{ID: uuidToString(dep.ID), Type: req.Type, Issue: other})
}

func (h *Handler) DeleteIssueDependency(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	depID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "depId"), "dependency id")
	if !ok {
		return
	}

	dep, err := h.Queries.DeleteIssueDependency(r.Context(), db.DeleteIssueDependencyParams{ID: depID, IssueID: issue.ID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "dependency not found")
			return
		}
		slog.Warn("delete issue dependency failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to delete dependency")
		return
	}

	other := dep.DependsOnIssueID
	if other == issue.ID {
		other = dep.IssueID
	}
	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	h.publishIssueDependencyChange(r, actorType, actorID, issue.WorkspaceID, issue.ID, other)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) issueDependencyExists(ctx context.Context, from, to pgtype.UUID, depType string) bool {
	_, err := h.Queries.GetIssueDependency(ctx, db.GetIssueDependencyParams{
		IssueID:          from,
		DependsOnIssueID: to,
		Type:             depType,
	})
	return err == nil
}

// publishIssueDependencyChange bumps both issues' revision and emits
// issue:updated for each, so clients that gate on a strictly increasing
// revision admit the event and refetch the dependency lists.
func (h *Handler) publishIssueDependencyChange(r *http.Request, actorType, actorID string, wsID pgtype.UUID, ids ...pgtype.UUID) {
	ctx := r.Context()
	if err := h.Queries.BumpIssueRevisions(ctx, ids); err != nil {
		slog.Warn("bump issue revisions failed", append(logger.RequestAttrs(r), "error", err)...)
		return
	}
	prefix := h.getIssuePrefix(ctx, wsID)
	fill := h.newStatusCategoryFiller(ctx, wsID)
	for _, id := range ids {
		issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: id, WorkspaceID: wsID})
		if err != nil {
			slog.Warn("reload issue after dependency change failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(id))...)
			continue
		}
		resp := issueToResponse(issue, prefix)
		fill(&resp)
		h.publish(protocol.EventIssueUpdated, uuidToString(wsID), actorType, actorID, map[string]any{"issue": resp})
	}
}
