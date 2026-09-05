package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Workspace Brain: notes the whole workspace shares. Members read and write
// them here; agent runs write them through `multica brain save` (which lands
// on the same endpoints with a task token, so the actor resolves to the
// agent); the daily curation pass rewrites them as source='curation'.

const (
	// workspaceNoteMaxTitleRunes / workspaceNoteMaxContentRunes mirror the
	// CHECK constraints on the table, validated here so a violation is a clean
	// 400 instead of a 500 from the database.
	workspaceNoteMaxTitleRunes   = 200
	workspaceNoteMaxContentRunes = 20000
	// A note's tags are a filter dimension, not a second body.
	workspaceNoteMaxTags    = 10
	workspaceNoteMaxTagRune = 50

	workspaceNoteDefaultPageSize = 100
	workspaceNoteMaxPageSize     = 500
)

type WorkspaceNoteResponse struct {
	ID            string   `json:"id"`
	WorkspaceID   string   `json:"workspace_id"`
	Title         string   `json:"title"`
	Content       string   `json:"content"`
	Tags          []string `json:"tags"`
	Source        string   `json:"source"`
	SourceTaskID  *string  `json:"source_task_id"`
	SourceAgentID *string  `json:"source_agent_id"`
	Pinned        bool     `json:"pinned"`
	ArchivedAt    *string  `json:"archived_at"`
	MergedInto    *string  `json:"merged_into"`
	CreatedByType string   `json:"created_by_type"`
	CreatedByID   *string  `json:"created_by_id"`
	Revision      int64    `json:"revision"`
	CreatedAt     string   `json:"created_at"`
	UpdatedAt     string   `json:"updated_at"`
}

type CreateWorkspaceNoteRequest struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
	Pinned  bool     `json:"pinned"`
}

type UpdateWorkspaceNoteRequest struct {
	Title   *string   `json:"title"`
	Content *string   `json:"content"`
	Tags    *[]string `json:"tags"`
	Pinned  *bool     `json:"pinned"`
	// Revision is the value the client read. Omitted (0) means "I did not
	// check"; the update is then refused rather than silently clobbering.
	Revision int64 `json:"revision"`
}

func workspaceNoteToResponse(n db.WorkspaceNote) WorkspaceNoteResponse {
	tags := n.Tags
	if tags == nil {
		tags = []string{}
	}
	resp := WorkspaceNoteResponse{
		ID:            uuidToString(n.ID),
		WorkspaceID:   uuidToString(n.WorkspaceID),
		Title:         n.Title,
		Content:       n.Content,
		Tags:          tags,
		Source:        n.Source,
		SourceTaskID:  uuidToPtr(n.SourceTaskID),
		SourceAgentID: uuidToPtr(n.SourceAgentID),
		Pinned:        n.Pinned,
		MergedInto:    uuidToPtr(n.MergedInto),
		CreatedByType: n.CreatedByType,
		CreatedByID:   uuidToPtr(n.CreatedByID),
		Revision:      n.Revision,
		CreatedAt:     timestampToString(n.CreatedAt),
		UpdatedAt:     timestampToString(n.UpdatedAt),
	}
	if n.ArchivedAt.Valid {
		archivedAt := timestampToString(n.ArchivedAt)
		resp.ArchivedAt = &archivedAt
	}
	return resp
}

// validateWorkspaceNoteTitle normalizes and validates a title.
func validateWorkspaceNoteTitle(title string) (string, bool) {
	title = strings.TrimSpace(util.SanitizeTextForPostgres(title))
	if title == "" || utf8.RuneCountInString(title) > workspaceNoteMaxTitleRunes {
		return "", false
	}
	return title, true
}

func validateWorkspaceNoteContent(content string) (string, bool) {
	content = util.SanitizeTextForPostgres(content)
	if utf8.RuneCountInString(content) > workspaceNoteMaxContentRunes {
		return "", false
	}
	return content, true
}

// normalizeWorkspaceNoteTags trims, lowercases, de-duplicates and sorts tags.
// Lowercasing is what makes the tag filter a single equality test instead of a
// case-folding scan, and stops "Backend" and "backend" from splitting a facet.
func normalizeWorkspaceNoteTags(tags []string) ([]string, bool) {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, raw := range tags {
		tag := strings.ToLower(strings.TrimSpace(util.SanitizeTextForPostgres(raw)))
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > workspaceNoteMaxTagRune {
			return nil, false
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) > workspaceNoteMaxTags {
		return nil, false
	}
	sort.Strings(out)
	return out, true
}

// noteActor resolves who is writing. A run authenticated with a task token
// resolves to its agent, so notes an agent saves are attributed to the agent
// and linked back to the run.
func (h *Handler) noteActor(r *http.Request, userID, workspaceID string) (actorType string, actorID, taskID pgtype.UUID) {
	resolvedType, resolvedID := h.resolveActor(r, userID, workspaceID)
	if parsed, err := util.ParseUUID(resolvedID); err == nil {
		actorID = parsed
	}
	if resolvedType == "agent" {
		if parsed, err := util.ParseUUID(r.Header.Get("X-Task-ID")); err == nil {
			taskID = parsed
		}
	}
	return resolvedType, actorID, taskID
}

func (h *Handler) ListWorkspaceNotes(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}

	limit := workspaceNoteDefaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if parsed > workspaceNoteMaxPageSize {
			parsed = workspaceNoteMaxPageSize
		}
		limit = parsed
	}

	params := db.ListWorkspaceNotesParams{
		WorkspaceID:     workspaceID,
		IncludeArchived: r.URL.Query().Get("archived") == "true",
		PageLimit:       int32(limit),
	}
	if tag := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("tag"))); tag != "" {
		params.Tag = pgtype.Text{String: tag, Valid: true}
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		params.Search = pgtype.Text{String: search, Valid: true}
	}

	rows, err := h.Queries.ListWorkspaceNotes(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace notes")
		return
	}
	items := make([]WorkspaceNoteResponse, 0, len(rows))
	for _, n := range rows {
		items = append(items, workspaceNoteToResponse(n))
	}

	// The filter chips need every live tag, not just the tags of the page the
	// current filter happens to return — so they ship alongside the items
	// instead of being derived client-side from a filtered list.
	tags, err := h.Queries.ListWorkspaceNoteTags(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspace note tags")
		return
	}
	if tags == nil {
		tags = []string{}
	}

	writeJSON(w, http.StatusOK, struct {
		Items []WorkspaceNoteResponse `json:"items"`
		Tags  []string                `json:"tags"`
	}{Items: items, Tags: tags})
}

func (h *Handler) GetWorkspaceNote(w http.ResponseWriter, r *http.Request) {
	note, ok := h.loadWorkspaceNote(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, workspaceNoteToResponse(note))
}

// loadWorkspaceNote resolves {id} to a row in the request workspace. A note
// from another workspace is a 404, so existence leaks nothing across tenants.
func (h *Handler) loadWorkspaceNote(w http.ResponseWriter, r *http.Request) (db.WorkspaceNote, bool) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return db.WorkspaceNote{}, false
	}
	noteID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return db.WorkspaceNote{}, false
	}
	note, err := h.Queries.GetWorkspaceNote(r.Context(), db.GetWorkspaceNoteParams{
		ID:          noteID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "workspace note not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load workspace note")
		}
		return db.WorkspaceNote{}, false
	}
	return note, true
}

func (h *Handler) CreateWorkspaceNote(w http.ResponseWriter, r *http.Request) {
	workspaceIDStr := h.resolveWorkspaceID(r)
	workspaceID, ok := parseUUIDOrBadRequest(w, workspaceIDStr, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateWorkspaceNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title, ok := validateWorkspaceNoteTitle(req.Title)
	if !ok {
		writeError(w, http.StatusBadRequest, "title is required and must be at most 200 characters")
		return
	}
	content, ok := validateWorkspaceNoteContent(req.Content)
	if !ok {
		writeError(w, http.StatusBadRequest, "content must be at most 20000 characters")
		return
	}
	tags, ok := normalizeWorkspaceNoteTags(req.Tags)
	if !ok {
		writeError(w, http.StatusBadRequest, "at most 10 tags of 50 characters each")
		return
	}

	actorType, actorID, taskID := h.noteActor(r, userID, workspaceIDStr)
	source := "manual"
	params := db.CreateWorkspaceNoteParams{
		ID:            dbid.NewV7(),
		WorkspaceID:   workspaceID,
		Title:         title,
		Content:       content,
		Tags:          tags,
		Pinned:        req.Pinned,
		CreatedByType: actorType,
		CreatedByID:   actorID,
	}
	if actorType == "agent" {
		source = "agent"
		params.SourceAgentID = actorID
		params.SourceTaskID = taskID
	}
	params.Source = source

	// "Show me first" (K69): a preview-mode run's note is held for approval.
	if agentID, taskID, preview := h.previewRun(r); preview {
		if eff, ok := h.recordPending(r, agentID, taskID, workspaceID, pgtype.UUID{}, service.EffectNoteCreate, "workspace_note", params.ID,
			map[string]any{"title": title}, map[string]any{"title": title, "content": content, "tags": tags, "pinned": req.Pinned}, true); ok {
			writePending(w, eff, map[string]any{"id": uuidToString(eff.ID), "title": title, "pending_approval": true})
			return
		}
	}
	note, err := h.Queries.CreateWorkspaceNote(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create workspace note: "+err.Error())
		return
	}

	// Undo (K69): a note a run wrote can be removed again.
	h.recordEffect(r, workspaceID, pgtype.UUID{}, service.EffectNoteCreate, "workspace_note", note.ID, map[string]any{}, map[string]any{"title": note.Title}, true)
	resp := workspaceNoteToResponse(note)
	h.publish(protocol.EventWorkspaceNoteCreated, workspaceIDStr, actorType, uuidToString(actorID), map[string]any{"note": resp})
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) UpdateWorkspaceNote(w http.ResponseWriter, r *http.Request) {
	note, ok := h.loadWorkspaceNote(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req UpdateWorkspaceNoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Revision <= 0 {
		writeError(w, http.StatusBadRequest, "revision is required; send the revision you read")
		return
	}

	params := db.UpdateWorkspaceNoteParams{
		ID:               note.ID,
		WorkspaceID:      note.WorkspaceID,
		ExpectedRevision: req.Revision,
	}
	if req.Title != nil {
		title, valid := validateWorkspaceNoteTitle(*req.Title)
		if !valid {
			writeError(w, http.StatusBadRequest, "title is required and must be at most 200 characters")
			return
		}
		params.Title = pgtype.Text{String: title, Valid: true}
	}
	if req.Content != nil {
		content, valid := validateWorkspaceNoteContent(*req.Content)
		if !valid {
			writeError(w, http.StatusBadRequest, "content must be at most 20000 characters")
			return
		}
		params.Content = pgtype.Text{String: content, Valid: true}
	}
	if req.Tags != nil {
		tags, valid := normalizeWorkspaceNoteTags(*req.Tags)
		if !valid {
			writeError(w, http.StatusBadRequest, "at most 10 tags of 50 characters each")
			return
		}
		// An explicit empty list clears the tags; sqlc's COALESCE would keep
		// the old value for a nil slice, so send a non-nil empty slice.
		if tags == nil {
			tags = []string{}
		}
		params.Tags = tags
	}
	if req.Pinned != nil {
		params.Pinned = pgtype.Bool{Bool: *req.Pinned, Valid: true}
	}

	// "Show me first" (K69): a preview-mode run's edit is held for approval.
	if agentID, taskID, preview := h.previewRun(r); preview {
		payload := map[string]any{}
		if req.Title != nil {
			payload["title"] = params.Title.String
		}
		if req.Content != nil {
			payload["content"] = params.Content.String
		}
		if req.Tags != nil {
			payload["tags"] = params.Tags
		}
		if req.Pinned != nil {
			payload["pinned"] = *req.Pinned
		}
		if eff, ok := h.recordPending(r, agentID, taskID, note.WorkspaceID, pgtype.UUID{}, service.EffectNoteUpdate, "workspace_note", note.ID, map[string]any{"title": note.Title}, payload, true); ok {
			writePending(w, eff, workspaceNoteToResponse(note))
			return
		}
	}
	updated, err := h.Queries.UpdateWorkspaceNote(r.Context(), params)
	if errors.Is(err, pgx.ErrNoRows) {
		// The note exists (loadWorkspaceNote just read it), so the only way
		// the UPDATE matched nothing is a revision that moved under us.
		writeError(w, http.StatusConflict, "workspace note was modified by someone else; reload and retry")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workspace note: "+err.Error())
		return
	}
	// Undo (K69): title, content, tags and pin state as they were before the run's edit.
	h.recordEffect(r, note.WorkspaceID, pgtype.UUID{}, service.EffectNoteUpdate, "workspace_note", note.ID,
		map[string]any{"title": note.Title, "content": note.Content, "tags": note.Tags, "pinned": note.Pinned},
		map[string]any{"title": updated.Title, "content": updated.Content, "tags": updated.Tags, "pinned": updated.Pinned}, true)

	workspaceIDStr := uuidToString(note.WorkspaceID)
	actorType, actorID, _ := h.noteActor(r, userID, workspaceIDStr)
	resp := workspaceNoteToResponse(updated)
	h.publish(protocol.EventWorkspaceNoteUpdated, workspaceIDStr, actorType, uuidToString(actorID), map[string]any{"note": resp})
	writeJSON(w, http.StatusOK, resp)
}

// ArchiveWorkspaceNote takes a note out of the Brain without losing it: it
// stops being injected into runs and drops out of the default listing.
func (h *Handler) ArchiveWorkspaceNote(w http.ResponseWriter, r *http.Request) {
	h.setWorkspaceNoteArchived(w, r, true)
}

func (h *Handler) UnarchiveWorkspaceNote(w http.ResponseWriter, r *http.Request) {
	h.setWorkspaceNoteArchived(w, r, false)
}

func (h *Handler) setWorkspaceNoteArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	note, ok := h.loadWorkspaceNote(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	params := db.SetWorkspaceNoteArchivedParams{ID: note.ID, WorkspaceID: note.WorkspaceID}
	if archived {
		params.ArchivedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
		// Keep an existing merge pointer; unarchiving clears it, because a
		// note that is live again is no longer folded into another one.
		params.MergedInto = note.MergedInto
	}

	if agentID, taskID, preview := h.previewRun(r); preview && archived {
		// "Show me first" (K69): held for approval.
		if eff, ok := h.recordPending(r, agentID, taskID, note.WorkspaceID, pgtype.UUID{}, service.EffectNoteArchive, "workspace_note", note.ID, map[string]any{"archived": true}, map[string]any{"archived": true}, true); ok {
			writePending(w, eff, workspaceNoteToResponse(note))
			return
		}
	}
	updated, err := h.Queries.SetWorkspaceNoteArchived(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update workspace note")
		return
	}
	if archived {
		// Undo (K69): unarchive on reversal.
		h.recordEffect(r, note.WorkspaceID, pgtype.UUID{}, service.EffectNoteArchive, "workspace_note", note.ID, map[string]any{"archived": false}, map[string]any{"archived": true}, true)
	}

	workspaceIDStr := uuidToString(note.WorkspaceID)
	actorType, actorID, _ := h.noteActor(r, userID, workspaceIDStr)
	resp := workspaceNoteToResponse(updated)
	h.publish(protocol.EventWorkspaceNoteUpdated, workspaceIDStr, actorType, uuidToString(actorID), map[string]any{"note": resp})
	writeJSON(w, http.StatusOK, resp)
}

// DeleteWorkspaceNote removes a note permanently. Unlike read/write, which
// every member holds, deletion is reserved for workspace owners/admins and the
// note's own author — a shared knowledge base whose entries anyone can erase
// is not one a team can rely on.
func (h *Handler) DeleteWorkspaceNote(w http.ResponseWriter, r *http.Request) {
	note, ok := h.loadWorkspaceNote(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceIDStr := uuidToString(note.WorkspaceID)
	member, ok := h.workspaceMember(w, r, workspaceIDStr)
	if !ok {
		return
	}
	actorType, actorID, _ := h.noteActor(r, userID, workspaceIDStr)
	if !canDeleteWorkspaceNote(note, member, actorType, actorID) {
		writeError(w, http.StatusForbidden, "only a workspace admin or the note author can delete this note")
		return
	}

	// "Show me first" (K69): a preview-mode run's deletion is held for approval.
	if agentID, taskID, preview := h.previewRun(r); preview && actorType == "agent" {
		if eff, ok := h.recordPending(r, agentID, taskID, note.WorkspaceID, pgtype.UUID{}, service.EffectNoteDelete, "workspace_note", note.ID,
			map[string]any{"title": note.Title}, map[string]any{}, true); ok {
			writePending(w, eff, map[string]any{"note_id": uuidToString(note.ID), "pending_approval": true})
			return
		}
	}

	rows, err := h.Queries.DeleteWorkspaceNote(r.Context(), db.DeleteWorkspaceNoteParams{
		ID:          note.ID,
		WorkspaceID: note.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete workspace note")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "workspace note not found")
		return
	}
	// Undo (K69): a run's deletion is reversible.
	h.recordEffect(r, note.WorkspaceID, pgtype.UUID{}, service.EffectNoteDelete, "workspace_note", note.ID, noteEffectSnapshot(note), map[string]any{}, true)

	h.publish(protocol.EventWorkspaceNoteDeleted, workspaceIDStr, actorType, uuidToString(actorID), map[string]any{
		"note": workspaceNoteToResponse(note),
	})
	w.WriteHeader(http.StatusNoContent)
}

// canDeleteWorkspaceNote is the delete predicate, split out so the boundary
// matrix is testable without an HTTP round trip.
func canDeleteWorkspaceNote(note db.WorkspaceNote, member db.Member, actorType string, actorID pgtype.UUID) bool {
	if roleAllowed(member.Role, "owner", "admin") {
		return true
	}
	if note.CreatedByType != actorType {
		return false
	}
	return note.CreatedByID.Valid && actorID.Valid && note.CreatedByID == actorID
}

// WorkspaceNoteContext is one Brain note as the claim response ships it to the
// daemon. Its json tags mirror execenv.WorkspaceNoteForEnv, which the daemon
// decodes straight into.
type WorkspaceNoteContext struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Pinned    bool     `json:"pinned,omitempty"`
	Source    string   `json:"source,omitempty"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

func workspaceNotesToContext(notes []db.WorkspaceNote) []WorkspaceNoteContext {
	out := make([]WorkspaceNoteContext, 0, len(notes))
	for _, n := range notes {
		out = append(out, WorkspaceNoteContext{
			ID:        uuidToString(n.ID),
			Title:     n.Title,
			Content:   n.Content,
			Tags:      n.Tags,
			Pinned:    n.Pinned,
			Source:    n.Source,
			UpdatedAt: timestampToString(n.UpdatedAt),
		})
	}
	return out
}
