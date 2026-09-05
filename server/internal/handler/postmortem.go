package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	postmortemDefaultPageSize = 50
	postmortemMaxPageSize     = 100
)

// PostmortemResponse is one drafted postmortem as the review UI renders it.
type PostmortemResponse struct {
	ID              string     `json:"id"`
	SourceTaskID    string     `json:"source_task_id"`
	IssueID         string     `json:"issue_id,omitempty"`
	AgentID         string     `json:"agent_id,omitempty"`
	Trigger         string     `json:"trigger"`
	State           string     `json:"state"`
	FailureReason   string     `json:"failure_reason"`
	Summary         string     `json:"summary"`
	RootCause       string     `json:"root_cause"`
	Impact          string     `json:"impact"`
	PreventiveRules []string   `json:"preventive_rules"`
	CostUsdTicks    int64      `json:"cost_usd_ticks,omitempty"`
	LlmGenerated    bool       `json:"llm_generated"`
	ResolvedAt      *time.Time `json:"resolved_at,omitempty"`
	Revision        int64      `json:"revision"`
	// AppliedRules is how many preventive rules were stored in the agent's
	// memory by this approve call. Absent on reads and on discard.
	AppliedRules *int      `json:"applied_rules,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func postmortemToResponse(pm db.Postmortem) PostmortemResponse {
	rules := []string{}
	if len(pm.PreventiveRules) > 0 {
		_ = json.Unmarshal(pm.PreventiveRules, &rules)
	}
	resp := PostmortemResponse{
		ID:              util.UUIDToString(pm.ID),
		SourceTaskID:    util.UUIDToString(pm.SourceTaskID),
		Trigger:         pm.Trigger,
		State:           pm.State,
		FailureReason:   pm.FailureReason,
		Summary:         pm.Summary,
		RootCause:       pm.RootCause,
		Impact:          pm.Impact,
		PreventiveRules: rules,
		LlmGenerated:    pm.LlmGenerated,
		Revision:        pm.Revision,
		CreatedAt:       pm.CreatedAt.Time,
	}
	if pm.IssueID.Valid {
		resp.IssueID = util.UUIDToString(pm.IssueID)
	}
	if pm.AgentID.Valid {
		resp.AgentID = util.UUIDToString(pm.AgentID)
	}
	if pm.CostUsdTicks.Valid {
		resp.CostUsdTicks = pm.CostUsdTicks.Int64
	}
	if pm.ResolvedAt.Valid {
		resolvedAt := pm.ResolvedAt.Time
		resp.ResolvedAt = &resolvedAt
	}
	return resp
}

// GetPostmortems lists postmortems for the workspace, newest first, filtered
// by state (default "draft"), keyset-paginated.
func (h *Handler) GetPostmortems(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = "draft"
	}
	switch state {
	case "draft", "approved", "discarded":
	default:
		writeError(w, http.StatusBadRequest, "state must be one of: draft, approved, discarded")
		return
	}

	limit := postmortemDefaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if parsed > postmortemMaxPageSize {
			parsed = postmortemMaxPageSize
		}
		limit = parsed
	}

	params := db.ListPostmortemsParams{
		WorkspaceID: workspaceID,
		State:       state,
		PageLimit:   int32(limit),
	}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		cursorTime, cursorID, err := decodePostmortemCursor(cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		params.CursorTime = pgtype.Timestamptz{Time: cursorTime, Valid: true}
		params.CursorID = cursorID
	}

	rows, err := h.Queries.ListPostmortems(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list postmortems")
		return
	}
	items := make([]PostmortemResponse, 0, len(rows))
	for _, pm := range rows {
		items = append(items, postmortemToResponse(pm))
	}

	resp := struct {
		Items      []PostmortemResponse `json:"items"`
		NextCursor string               `json:"next_cursor,omitempty"`
	}{Items: items}
	if len(rows) == limit {
		last := rows[len(rows)-1]
		resp.NextCursor = encodePostmortemCursor(last.CreatedAt.Time, last.ID)
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetPostmortemsStats returns per-state counts to drive the queue badge.
func (h *Handler) GetPostmortemsStats(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	rows, err := h.Queries.CountPostmortemsByState(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count postmortems")
		return
	}
	counts := map[string]int64{"draft": 0, "approved": 0, "discarded": 0}
	for _, row := range rows {
		counts[row.State] = row.N
	}
	writeJSON(w, http.StatusOK, counts)
}

// GetPostmortem returns one postmortem.
func (h *Handler) GetPostmortem(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	pmID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	pm, err := h.Queries.GetPostmortem(r.Context(), db.GetPostmortemParams{ID: pmID, WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "postmortem not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load postmortem")
		return
	}
	writeJSON(w, http.StatusOK, postmortemToResponse(pm))
}

// ApprovePostmortem marks a draft postmortem approved (keep it).
func (h *Handler) ApprovePostmortem(w http.ResponseWriter, r *http.Request) {
	h.resolvePostmortem(w, r, "approved")
}

// DiscardPostmortem marks a draft postmortem discarded (drop it).
func (h *Handler) DiscardPostmortem(w http.ResponseWriter, r *http.Request) {
	h.resolvePostmortem(w, r, "discarded")
}

func (h *Handler) resolvePostmortem(w http.ResponseWriter, r *http.Request, state string) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	pmID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}

	pm, err := h.Queries.ResolvePostmortem(r.Context(), db.ResolvePostmortemParams{
		ID:          pmID,
		WorkspaceID: workspaceID,
		State:       state,
		ResolvedBy:  parseUUID(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// Either the postmortem does not exist in this workspace, or it was
		// already resolved. Distinguish for a clearer client contract.
		if _, gerr := h.Queries.GetPostmortem(r.Context(), db.GetPostmortemParams{ID: pmID, WorkspaceID: workspaceID}); errors.Is(gerr, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "postmortem not found")
		} else {
			writeError(w, http.StatusConflict, "postmortem was already resolved")
		}
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve postmortem")
		return
	}

	h.TaskService.PublishPostmortemEvent(protocol.EventPostmortemResolved, workspaceID, pm)
	resp := postmortemToResponse(pm)
	if state == "approved" {
		// The postmortem is already approved; a memory write failure must
		// not undo that, so report it instead of failing the request.
		applied, err := h.TaskService.ApplyPostmortemRules(r.Context(), pm)
		if err != nil {
			slog.Error("postmortem rules not stored as agent memory",
				"postmortem_id", util.UUIDToString(pm.ID), "error", err)
		}
		resp.AppliedRules = &applied
	}
	writeJSON(w, http.StatusOK, resp)
}

func encodePostmortemCursor(createdAt time.Time, id pgtype.UUID) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + util.UUIDToString(id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodePostmortemCursor(cursor string) (time.Time, pgtype.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, pgtype.UUID{}, err
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, pgtype.UUID{}, errors.New("malformed cursor")
	}
	cursorTime, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return time.Time{}, pgtype.UUID{}, err
	}
	cursorID, err := util.ParseUUID(parts[1])
	if err != nil {
		return time.Time{}, pgtype.UUID{}, err
	}
	return cursorTime, cursorID, nil
}
