package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Cross-repo mirror issues (K54). See service/issue_mirror.go for the rules;
// this file is transport only.

// ErrCodeOpenMirrors is the stable 409 code a client can key on when the
// mirror gate refuses a move to done.
const ErrCodeOpenMirrors = "open_mirrors"

// issueMirrorService is built per call: it holds two pointers the Handler
// already owns, so there is nothing to wire or keep alive.
func (h *Handler) issueMirrorService() *service.IssueMirrorService {
	return service.NewIssueMirrorService(h.Queries, h.IssueService)
}

type MirrorLinkResponse struct {
	ID                 string `json:"id"`
	SourceProjectID    string `json:"source_project_id"`
	TargetProjectID    string `json:"target_project_id"`
	TargetProjectTitle string `json:"target_project_title"`
	TriggerLabel       string `json:"trigger_label"`
	CreatedAt          string `json:"created_at"`
}

type MirrorLinksResponse struct {
	Links []MirrorLinkResponse `json:"links"`
}

// IssueMirrorResponse is one mirror of a source issue.
type IssueMirrorResponse struct {
	ID            string `json:"id"`
	MirrorIssueID string `json:"mirror_issue_id"`
	Identifier    string `json:"identifier"`
	Number        int32  `json:"number"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	ProjectID     string `json:"project_id"`
	ProjectTitle  string `json:"project_title"`
	TypeSynced    bool   `json:"type_synced"`
}

// MirrorOfResponse is the source an issue was generated from, shown as the
// banner on a mirror.
type MirrorOfResponse struct {
	ID            string `json:"id"`
	SourceIssueID string `json:"source_issue_id"`
	Identifier    string `json:"identifier"`
	Number        int32  `json:"number"`
	Title         string `json:"title"`
	Status        string `json:"status"`
	ProjectID     string `json:"project_id"`
	ProjectTitle  string `json:"project_title"`
	TypeSynced    bool   `json:"type_synced"`
}

type IssueMirrorsResponse struct {
	Mirrors  []IssueMirrorResponse `json:"mirrors"`
	MirrorOf *MirrorOfResponse     `json:"mirror_of"`
}

// GET /api/projects/{id}/mirror-links — any workspace member may read the
// configuration; only a contributor may change it.
func (h *Handler) ListProjectMirrorLinks(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForRoles(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListProjectMirrorLinksBySource(r.Context(), db.ListProjectMirrorLinksBySourceParams{
		WorkspaceID: project.WorkspaceID, SourceProjectID: project.ID,
	})
	if err != nil {
		slog.Warn("list mirror links failed", append(logger.RequestAttrs(r), "error", err, "project_id", uuidToString(project.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to list mirror links")
		return
	}
	out := MirrorLinksResponse{Links: []MirrorLinkResponse{}}
	for _, row := range rows {
		out.Links = append(out.Links, MirrorLinkResponse{
			ID:                 uuidToString(row.ID),
			SourceProjectID:    uuidToString(row.SourceProjectID),
			TargetProjectID:    uuidToString(row.TargetProjectID),
			TargetProjectTitle: row.TargetProjectTitle,
			TriggerLabel:       row.TriggerLabel,
			CreatedAt:          timestampToString(row.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/projects/{id}/mirror-links
func (h *Handler) CreateProjectMirrorLink(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForRoles(w, r)
	if !ok {
		return
	}
	if !h.requireProjectWrite(w, r, project.ID) {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		TargetProjectID string `json:"target_project_id"`
		TriggerLabel    string `json:"trigger_label"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	targetID, ok := parseUUIDOrBadRequest(w, req.TargetProjectID, "target_project_id")
	if !ok {
		return
	}
	// A target outside this workspace is a 404, never a hint that it exists.
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: targetID, WorkspaceID: project.WorkspaceID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "target project not found")
		return
	}

	link, err := h.issueMirrorService().CreateMirrorLink(r.Context(), service.MirrorLinkParams{
		WorkspaceID:     project.WorkspaceID,
		SourceProjectID: project.ID,
		TargetProjectID: targetID,
		TriggerLabel:    req.TriggerLabel,
		CreatedBy:       parseUUID(userID),
	})
	switch {
	case errors.Is(err, service.ErrMirrorLinkSameProject):
		writeError(w, http.StatusBadRequest, "a project cannot mirror into itself")
		return
	case errors.Is(err, service.ErrMirrorLinkCycle):
		writeError(w, http.StatusConflict, "this link would create a mirror cycle")
		return
	case errors.Is(err, service.ErrMirrorLinkDuplicate):
		writeError(w, http.StatusConflict, "this mirror link already exists")
		return
	case errors.Is(err, service.ErrMirrorLinkEmptyLabel):
		writeError(w, http.StatusBadRequest, "trigger_label is required")
		return
	case err != nil:
		slog.Warn("create mirror link failed", append(logger.RequestAttrs(r), "error", err, "project_id", uuidToString(project.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to create mirror link")
		return
	}

	target, _ := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: targetID, WorkspaceID: project.WorkspaceID})
	writeJSON(w, http.StatusCreated, MirrorLinkResponse{
		ID:                 uuidToString(link.ID),
		SourceProjectID:    uuidToString(link.SourceProjectID),
		TargetProjectID:    uuidToString(link.TargetProjectID),
		TargetProjectTitle: target.Title,
		TriggerLabel:       link.TriggerLabel,
		CreatedAt:          timestampToString(link.CreatedAt),
	})
}

// DELETE /api/projects/{id}/mirror-links/{linkId} — removes the configuration
// only. Mirrors already created stay: they are real issues.
func (h *Handler) DeleteProjectMirrorLink(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForRoles(w, r)
	if !ok {
		return
	}
	if !h.requireProjectWrite(w, r, project.ID) {
		return
	}
	linkID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "linkId"), "link id")
	if !ok {
		return
	}
	if _, err := h.Queries.DeleteProjectMirrorLink(r.Context(), db.DeleteProjectMirrorLinkParams{
		ID: linkID, WorkspaceID: project.WorkspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "mirror link not found")
			return
		}
		slog.Warn("delete mirror link failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete mirror link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/issues/{id}/mirrors — the mirrors generated from this issue, and
// the source it was generated from when it is itself a mirror.
func (h *Handler) GetIssueMirrors(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	ctx := r.Context()
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)

	rows, err := h.Queries.ListIssueMirrorsBySource(ctx, db.ListIssueMirrorsBySourceParams{
		WorkspaceID: issue.WorkspaceID, SourceIssueID: issue.ID,
	})
	if err != nil {
		slog.Warn("list issue mirrors failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to list mirrors")
		return
	}
	out := IssueMirrorsResponse{Mirrors: []IssueMirrorResponse{}}
	for _, row := range rows {
		out.Mirrors = append(out.Mirrors, IssueMirrorResponse{
			ID:            uuidToString(row.ID),
			MirrorIssueID: uuidToString(row.MirrorIssueID),
			Identifier:    prefix + "-" + strconv.Itoa(int(row.MirrorNumber)),
			Number:        row.MirrorNumber,
			Title:         row.MirrorTitle,
			Status:        row.MirrorStatus,
			ProjectID:     uuidToString(row.MirrorProjectID),
			ProjectTitle:  row.MirrorProjectTitle,
			TypeSynced:    row.TypeSynced,
		})
	}

	// pgx.ErrNoRows here is the ordinary case: most issues are not mirrors.
	if src, err := h.Queries.GetIssueMirrorByMirrorIssue(ctx, db.GetIssueMirrorByMirrorIssueParams{
		WorkspaceID: issue.WorkspaceID, MirrorIssueID: issue.ID,
	}); err == nil {
		out.MirrorOf = &MirrorOfResponse{
			ID:            uuidToString(src.ID),
			SourceIssueID: uuidToString(src.SourceIssueID),
			Identifier:    prefix + "-" + strconv.Itoa(int(src.SourceNumber)),
			Number:        src.SourceNumber,
			Title:         src.SourceTitle,
			Status:        src.SourceStatus,
			ProjectID:     uuidToString(src.SourceProjectID),
			ProjectTitle:  src.SourceProjectTitle,
			TypeSynced:    src.TypeSynced,
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("load mirror source failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
	}

	writeJSON(w, http.StatusOK, out)
}

// PUT /api/issues/{id}/mirrors/{mirrorId}/type-synced — the per-mirror marker
// saying a human reconciled the two issues' types.
func (h *Handler) SetIssueMirrorTypeSynced(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.requireProjectWrite(w, r, issue.ProjectID) {
		return
	}
	mirrorID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "mirrorId"), "mirror id")
	if !ok {
		return
	}
	var req struct {
		Value bool `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// The row must belong to this issue: without the check, a member of the
	// workspace could flip a marker on any pair by guessing its id.
	existing, err := h.Queries.GetIssueMirror(r.Context(), db.GetIssueMirrorParams{ID: mirrorID, WorkspaceID: issue.WorkspaceID})
	if err != nil || (existing.SourceIssueID != issue.ID && existing.MirrorIssueID != issue.ID) {
		writeError(w, http.StatusNotFound, "mirror not found")
		return
	}

	updated, err := h.Queries.SetIssueMirrorTypeSynced(r.Context(), db.SetIssueMirrorTypeSyncedParams{
		ID: mirrorID, WorkspaceID: issue.WorkspaceID, TypeSynced: req.Value,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "mirror not found")
			return
		}
		slog.Warn("set mirror type synced failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update mirror")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":          uuidToString(updated.ID),
		"type_synced": updated.TypeSynced,
	})
}

// mirrorsAllowStatus keeps a source issue out of the done category while one
// of its mirrors is still open (K54). The mirror also blocks the source through
// issue_dependency, which merge readiness reports; this is the gate that
// actually refuses the write.
//
// Sixth gate next to plan verification, the review gate, acceptance criteria,
// business rules and the trust dial. Every status-change entry point that runs
// those must run this one too, or the rule is bypassable.
func (h *Handler) mirrorsAllowStatus(w http.ResponseWriter, r *http.Request, issue db.Issue, statusKey string) bool {
	ctx := r.Context()
	if issuestatus.Effective(ctx, h.Queries, issue.WorkspaceID, statusKey) != issuestatus.Done {
		return true
	}
	rows, err := h.Queries.ListIssueMirrorsBySource(ctx, db.ListIssueMirrorsBySourceParams{
		WorkspaceID: issue.WorkspaceID, SourceIssueID: issue.ID,
	})
	if err != nil {
		slog.Warn("mirror gate failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to evaluate open mirrors")
		return false
	}
	if len(rows) == 0 {
		return true
	}

	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	open := []IssueMirrorResponse{}
	identifiers := []string{}
	for _, row := range rows {
		// A mirror in a CUSTOM done/cancelled status is finished too, so the
		// gate resolves the catalog rather than comparing the raw key.
		switch issuestatus.Effective(ctx, h.Queries, issue.WorkspaceID, row.MirrorStatus) {
		case issuestatus.Done, issuestatus.Cancelled:
			continue
		}
		identifier := prefix + "-" + strconv.Itoa(int(row.MirrorNumber))
		identifiers = append(identifiers, identifier)
		open = append(open, IssueMirrorResponse{
			ID:            uuidToString(row.ID),
			MirrorIssueID: uuidToString(row.MirrorIssueID),
			Identifier:    identifier,
			Number:        row.MirrorNumber,
			Title:         row.MirrorTitle,
			Status:        row.MirrorStatus,
			ProjectID:     uuidToString(row.MirrorProjectID),
			ProjectTitle:  row.MirrorProjectTitle,
			TypeSynced:    row.TypeSynced,
		})
	}
	if len(open) == 0 {
		return true
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"code":    ErrCodeOpenMirrors,
		"error":   fmt.Sprintf("%d mirror issues are still open: %s", len(open), strings.Join(identifiers, ", ")),
		"mirrors": open,
	})
	return false
}

// mirrorIssueForLabels is the trigger hook. It runs after a label attach has
// already committed, so a failure is logged and swallowed: refusing the
// request would undo nothing and would report a failure for work that
// succeeded.
func (h *Handler) mirrorIssueForLabels(ctx context.Context, issue db.Issue, labels []db.IssueLabel) {
	if !issue.ProjectID.Valid || len(labels) == 0 {
		return
	}
	svc := h.issueMirrorService()
	for _, label := range labels {
		if _, err := svc.MirrorIssueForLabel(ctx, issue, label.Name); err != nil {
			slog.Warn("mirror issue for label failed",
				"issue_id", uuidToString(issue.ID), "label", label.Name, "error", err)
		}
	}
}
