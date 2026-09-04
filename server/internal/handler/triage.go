package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	triageDefaultPageSize = 50
	triageMaxPageSize     = 100
	triageMaxBatchAccept  = 100
	// One retention sweep touches at most this many rows, so a large backlog
	// drains over consecutive runs instead of holding one long transaction.
	triageRetentionSweepBatch = 500
)

// TriageSourceStats is one inbound source and its 24h activity.
type TriageSourceStats struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	RefID      string `json:"ref_id"`
	Name       string `json:"name"`
	Mode       string `json:"mode"`
	Items24h   int64  `json:"items_24h"`
	Dropped24h int64  `json:"dropped_24h"`
}

// TriageStatsResponse summarizes the triage queue for the workspace. In M1
// every captured item is shadow (routing is unchanged), so pending stays zero
// and the shadow fields carry the measurement.
type TriageStatsResponse struct {
	Pending                 int64               `json:"pending"`
	ShadowPending           int64               `json:"shadow_pending"`
	Dropped24h              int64               `json:"dropped_24h"`
	OldestPendingAgeSeconds int64               `json:"oldest_pending_age_seconds"`
	Sources                 []TriageSourceStats `json:"sources"`
}

// TriageItemResponse is one queue entry as the UI renders it. Payload is the
// stored capture JSONB (size + embedded trigger payload or truncation stub).
type TriageItemResponse struct {
	ID                 string          `json:"id"`
	SourceID           string          `json:"source_id"`
	SourceName         string          `json:"source_name"`
	SourceKind         string          `json:"source_kind"`
	OriginType         string          `json:"origin_type"`
	Title              string          `json:"title"`
	BodyMarkdown       string          `json:"body_markdown"`
	Payload            json.RawMessage `json:"payload"`
	State              string          `json:"state"`
	CollapseCount      int32           `json:"collapse_count"`
	DropReason         string          `json:"drop_reason,omitempty"`
	ResolutionReason   string          `json:"resolution_reason,omitempty"`
	IssueID            string          `json:"issue_id,omitempty"`
	DuplicateOfIssueID string          `json:"duplicate_of_issue_id,omitempty"`
	FirstSeenAt        time.Time       `json:"first_seen_at"`
	ResolvedAt         *time.Time      `json:"resolved_at,omitempty"`
	Revision           int64           `json:"revision"`
}

// GetTriageStats returns queue volume for the workspace: real pending items,
// the M1 shadow measurement, and per-source 24h counts including dropped
// deliveries — the silent-loss population triage exists to hold.
func (h *Handler) GetTriageStats(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	ctx := r.Context()

	byState, err := h.Queries.CountTriageItemsByState(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load triage stats")
		return
	}
	var pending, shadowPending int64
	for _, row := range byState {
		if row.State != triage.StatePending {
			continue
		}
		if row.Shadow {
			shadowPending += row.N
		} else {
			pending += row.N
		}
	}

	recent, err := h.Queries.CountRecentTriageItemsBySource(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load triage stats")
		return
	}
	type sourceActivity struct {
		items, dropped int64
	}
	activity := make(map[string]*sourceActivity)
	var dropped24h int64
	for _, row := range recent {
		id := util.UUIDToString(row.SourceID)
		act := activity[id]
		if act == nil {
			act = &sourceActivity{}
			activity[id] = act
		}
		act.items += row.N
		if row.State == triage.StateDropped {
			act.dropped += row.N
			dropped24h += row.N
		}
	}

	age, err := h.Queries.OldestRealPendingTriageAgeSeconds(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load triage stats")
		return
	}

	sources, err := h.Queries.ListTriageSources(ctx, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load triage stats")
		return
	}
	resp := TriageStatsResponse{
		Pending:                 pending,
		ShadowPending:           shadowPending,
		Dropped24h:              dropped24h,
		OldestPendingAgeSeconds: age,
		Sources:                 []TriageSourceStats{},
	}
	for _, src := range sources {
		id := util.UUIDToString(src.ID)
		stats := TriageSourceStats{
			ID:    id,
			Kind:  src.Kind,
			RefID: util.UUIDToString(src.RefID),
			Name:  src.Name,
			Mode:  src.Mode,
		}
		if act := activity[id]; act != nil {
			stats.Items24h = act.items
			stats.Dropped24h = act.dropped
		}
		resp.Sources = append(resp.Sources, stats)
	}
	writeJSON(w, http.StatusOK, resp)
}

// ListTriageItems returns the visible queue (shadow measurement rows are
// never listed) for one state, newest first, keyset-paginated.
func (h *Handler) ListTriageItems(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	if state == "" {
		state = triage.StatePending
	}
	switch state {
	case triage.StatePending, triage.StateAccepted, triage.StateDismissed, triage.StateMerged:
	default:
		writeError(w, http.StatusBadRequest, "state must be one of: pending, accepted, dismissed, merged")
		return
	}

	limit := triageDefaultPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		if parsed > triageMaxPageSize {
			parsed = triageMaxPageSize
		}
		limit = parsed
	}

	params := db.ListTriageItemsParams{
		WorkspaceID: workspaceID,
		State:       state,
		PageLimit:   int32(limit),
	}
	if cursor := r.URL.Query().Get("cursor"); cursor != "" {
		cursorTime, cursorID, err := decodeTriageCursor(cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid cursor")
			return
		}
		params.CursorTime = pgtype.Timestamptz{Time: cursorTime, Valid: true}
		params.CursorID = cursorID
	}

	rows, err := h.Queries.ListTriageItems(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list triage items")
		return
	}

	sources, err := h.Queries.ListTriageSources(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list triage items")
		return
	}
	sourceByID := make(map[string]db.TriageSource, len(sources))
	for _, src := range sources {
		sourceByID[util.UUIDToString(src.ID)] = src
	}

	items := make([]TriageItemResponse, 0, len(rows))
	for _, row := range rows {
		items = append(items, triageItemToResponse(row, sourceByID))
	}

	resp := struct {
		Items      []TriageItemResponse `json:"items"`
		NextCursor string               `json:"next_cursor,omitempty"`
	}{Items: items}
	if len(rows) == limit {
		last := rows[len(rows)-1]
		resp.NextCursor = encodeTriageCursor(last.FirstSeenAt.Time, last.ID)
	}
	writeJSON(w, http.StatusOK, resp)
}

func triageItemToResponse(row db.TriageItem, sourceByID map[string]db.TriageSource) TriageItemResponse {
	resp := TriageItemResponse{
		ID:            util.UUIDToString(row.ID),
		SourceID:      util.UUIDToString(row.SourceID),
		OriginType:    row.OriginType,
		Title:         row.Title,
		BodyMarkdown:  row.BodyMarkdown,
		Payload:       json.RawMessage(row.Payload),
		State:         row.State,
		CollapseCount: row.CollapseCount,
		FirstSeenAt:   row.FirstSeenAt.Time,
		Revision:      row.Revision,
	}
	if len(row.Payload) == 0 {
		resp.Payload = json.RawMessage(`{}`)
	}
	if row.DropReason.Valid {
		resp.DropReason = row.DropReason.String
	}
	if row.ResolutionReason.Valid {
		resp.ResolutionReason = row.ResolutionReason.String
	}
	if row.IssueID.Valid {
		resp.IssueID = util.UUIDToString(row.IssueID)
	}
	if row.DuplicateOfIssueID.Valid {
		resp.DuplicateOfIssueID = util.UUIDToString(row.DuplicateOfIssueID)
	}
	if row.ResolvedAt.Valid {
		resolvedAt := row.ResolvedAt.Time
		resp.ResolvedAt = &resolvedAt
	}
	if src, ok := sourceByID[resp.SourceID]; ok {
		resp.SourceName = src.Name
		resp.SourceKind = src.Kind
	}
	return resp
}

func encodeTriageCursor(firstSeen time.Time, id pgtype.UUID) string {
	raw := firstSeen.UTC().Format(time.RFC3339Nano) + "|" + util.UUIDToString(id)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeTriageCursor(cursor string) (time.Time, pgtype.UUID, error) {
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

// acceptResult is the typed outcome of one accept attempt, shared by the
// single and batch endpoints.
type acceptResult struct {
	outcome     string // accepted | duplicate | limit_reached | not_found | not_pending | error
	issue       db.Issue
	duplicateOf db.Issue
	prefix      string
}

// acceptTriageItemCore resolves one pending item into a real issue while
// holding the item row locked, so two humans accepting the same item can
// never create two issues. The acceptor is the issue's creator; the assignee
// and project are inherited from the origin autopilot while it still exists.
func (h *Handler) acceptTriageItemCore(ctx context.Context, workspaceID pgtype.UUID, userID string, itemID pgtype.UUID) acceptResult {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return acceptResult{outcome: "error"}
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	item, err := qtx.LockTriageItemForResolution(ctx, db.LockTriageItemForResolutionParams{
		ID: itemID, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return acceptResult{outcome: "not_found"}
	}
	if err != nil {
		return acceptResult{outcome: "error"}
	}
	if item.State != triage.StatePending {
		return acceptResult{outcome: "not_pending"}
	}

	var assigneeType pgtype.Text
	var assigneeID, projectID pgtype.UUID
	if item.OriginType == "autopilot" && item.OriginID.Valid {
		if ap, aerr := h.Queries.GetAutopilotInWorkspace(ctx, db.GetAutopilotInWorkspaceParams{
			ID: item.OriginID, WorkspaceID: workspaceID,
		}); aerr == nil {
			if ap.AssigneeType != "" && ap.AssigneeID.Valid {
				assigneeType = pgtype.Text{String: ap.AssigneeType, Valid: true}
				assigneeID = ap.AssigneeID
			}
			projectID = ap.ProjectID
		}
	}

	prefix := h.getIssuePrefix(ctx, workspaceID)
	filler := h.newStatusCategoryFiller(ctx, workspaceID)
	result, err := h.IssueService.Create(ctx, service.IssueCreateParams{
		WorkspaceID:  workspaceID,
		Title:        item.Title,
		Description:  pgtype.Text{String: item.BodyMarkdown, Valid: item.BodyMarkdown != ""},
		Status:       "todo",
		Priority:     "none",
		AssigneeType: assigneeType,
		AssigneeID:   assigneeID,
		CreatorType:  "member",
		CreatorID:    parseUUID(userID),
		ProjectID:    projectID,
		OriginType:   pgtype.Text{String: item.OriginType, Valid: true},
		OriginID:     item.OriginID,
	}, service.IssueCreateOpts{
		BroadcastPayload: func(issue db.Issue, attachments []db.Attachment, labels []db.IssueLabel) map[string]any {
			resp := issueToResponse(issue, prefix)
			filler(&resp)
			return map[string]any{"issue": resp}
		},
	})

	if errors.Is(err, service.ErrActiveDuplicate) {
		if result.DuplicateIssue == nil {
			return acceptResult{outcome: "error"}
		}
		if _, merr := qtx.MergePendingTriageItem(ctx, db.MergePendingTriageItemParams{
			ID:                 item.ID,
			WorkspaceID:        workspaceID,
			DuplicateOfIssueID: result.DuplicateIssue.ID,
			ResolvedBy:         parseUUID(userID),
		}); merr != nil {
			return acceptResult{outcome: "error"}
		}
		if cerr := tx.Commit(ctx); cerr != nil {
			return acceptResult{outcome: "error"}
		}
		h.publishTriageResolved(workspaceID, item.ID, triage.StateMerged)
		return acceptResult{outcome: "duplicate", duplicateOf: *result.DuplicateIssue, prefix: prefix}
	}

	var limitErr *service.IssueLimitReachedError
	if errors.As(err, &limitErr) {
		// The item stays pending: hold, never lose. The caller maps this to
		// the workspace's 402 issue-limit contract.
		return acceptResult{outcome: "limit_reached"}
	}
	if err != nil {
		return acceptResult{outcome: "error"}
	}

	if _, err := qtx.AcceptPendingTriageItem(ctx, db.AcceptPendingTriageItemParams{
		ID:          item.ID,
		WorkspaceID: workspaceID,
		IssueID:     result.Issue.ID,
		ResolvedBy:  parseUUID(userID),
	}); err != nil {
		return acceptResult{outcome: "error"}
	}
	if err := tx.Commit(ctx); err != nil {
		return acceptResult{outcome: "error"}
	}
	h.publishTriageResolved(workspaceID, item.ID, triage.StateAccepted)
	return acceptResult{outcome: "accepted", issue: result.Issue, prefix: prefix}
}

// AcceptTriageItem accepts one pending item: the issue is created through
// the ordinary funnel (duplicate guard, quota, events, task enqueue) and the
// item is marked accepted in the same logical move.
func (h *Handler) AcceptTriageItem(w http.ResponseWriter, r *http.Request) {
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

	res := h.acceptTriageItemCore(r.Context(), workspaceID, userID, itemID)
	switch res.outcome {
	case "accepted":
		filler := h.newStatusCategoryFiller(r.Context(), workspaceID)
		resp := issueToResponse(res.issue, res.prefix)
		filler(&resp)
		writeJSON(w, http.StatusOK, map[string]any{
			"item_id": util.UUIDToString(itemID),
			"state":   triage.StateAccepted,
			"issue":   resp,
		})
	case "duplicate":
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":                       "duplicate",
			"item_id":                    util.UUIDToString(itemID),
			"state":                      triage.StateMerged,
			"duplicate_issue_id":         util.UUIDToString(res.duplicateOf.ID),
			"duplicate_issue_identifier": fmt.Sprintf("%s-%d", res.prefix, res.duplicateOf.Number),
		})
	case "limit_reached":
		writeError(w, http.StatusPaymentRequired, "workspace has reached its issue limit; the item stays in the queue")
	case "not_found":
		writeError(w, http.StatusNotFound, "triage item not found")
	case "not_pending":
		writeError(w, http.StatusConflict, "triage item was already resolved")
	default:
		writeError(w, http.StatusInternalServerError, "failed to accept triage item")
	}
}

// DismissTriageItem removes one pending item from the queue without creating
// an issue. The dismissal is recorded, not deleted.
func (h *Handler) DismissTriageItem(w http.ResponseWriter, r *http.Request) {
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
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to dismiss triage item")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	item, err := qtx.LockTriageItemForResolution(r.Context(), db.LockTriageItemForResolutionParams{
		ID: itemID, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "triage item not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to dismiss triage item")
		return
	}
	if item.State != triage.StatePending {
		writeError(w, http.StatusConflict, "triage item was already resolved")
		return
	}
	if _, err := qtx.DismissPendingTriageItem(r.Context(), db.DismissPendingTriageItemParams{
		ID:               item.ID,
		WorkspaceID:      workspaceID,
		ResolutionReason: pgtype.Text{String: req.Reason, Valid: req.Reason != ""},
		ResolvedBy:       parseUUID(userID),
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to dismiss triage item")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to dismiss triage item")
		return
	}
	h.publishTriageResolved(workspaceID, item.ID, triage.StateDismissed)
	writeJSON(w, http.StatusOK, map[string]any{
		"item_id": util.UUIDToString(itemID),
		"state":   triage.StateDismissed,
	})
}

// BatchAcceptTriageItem is one accept outcome inside a batch.
type BatchAcceptTriageItem struct {
	ID                       string `json:"id"`
	Outcome                  string `json:"outcome"` // accepted | duplicate | limit_reached | not_found | not_pending | error
	IssueID                  string `json:"issue_id,omitempty"`
	DuplicateOfIssueID       string `json:"duplicate_of_issue_id,omitempty"`
	DuplicateIssueIdentifier string `json:"duplicate_issue_identifier,omitempty"`
}

// BatchAcceptTriageItems accepts up to 100 items with one request. It always
// returns 200 with per-item outcomes; a batch stops at the first quota 402 —
// remaining items stay pending, which is exactly the hold-not-lose contract.
func (h *Handler) BatchAcceptTriageItems(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids must not be empty")
		return
	}
	if len(req.IDs) > triageMaxBatchAccept {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d items per batch", triageMaxBatchAccept))
		return
	}

	results := make([]BatchAcceptTriageItem, 0, len(req.IDs))
	for _, raw := range req.IDs {
		itemID, err := util.ParseUUID(raw)
		if err != nil {
			results = append(results, BatchAcceptTriageItem{ID: raw, Outcome: "not_found"})
			continue
		}
		res := h.acceptTriageItemCore(r.Context(), workspaceID, userID, itemID)
		entry := BatchAcceptTriageItem{ID: util.UUIDToString(itemID), Outcome: res.outcome}
		switch res.outcome {
		case "accepted":
			entry.IssueID = util.UUIDToString(res.issue.ID)
		case "duplicate":
			entry.DuplicateOfIssueID = util.UUIDToString(res.duplicateOf.ID)
			entry.DuplicateIssueIdentifier = fmt.Sprintf("%s-%d", res.prefix, res.duplicateOf.Number)
		case "limit_reached":
			// Stop the batch: everything after this would fail the same way.
			results = append(results, entry)
			writeJSON(w, http.StatusOK, struct {
				Items   []BatchAcceptTriageItem `json:"items"`
				Stopped string                  `json:"stopped"`
			}{Items: results, Stopped: "issue_limit_reached"})
			return
		}
		results = append(results, entry)
	}
	writeJSON(w, http.StatusOK, struct {
		Items []BatchAcceptTriageItem `json:"items"`
	}{Items: results})
}

// UpdateTriageSourceMode flips a source between gate, direct, and blocked.
// This is the M2 kill switch: routing changes on the next delivery.
func (h *Handler) UpdateTriageSource(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	sourceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch req.Mode {
	case string(triage.ModeGate), string(triage.ModeDirect), string(triage.ModeBlocked):
	default:
		writeError(w, http.StatusBadRequest, "mode must be one of: gate, direct, blocked")
		return
	}

	src, err := h.Queries.UpdateTriageSourceMode(r.Context(), db.UpdateTriageSourceModeParams{
		ID: sourceID, WorkspaceID: workspaceID, Mode: req.Mode,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "triage source not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update triage source")
		return
	}
	writeJSON(w, http.StatusOK, TriageSourceStats{
		ID:    util.UUIDToString(src.ID),
		Kind:  src.Kind,
		RefID: util.UUIDToString(src.RefID),
		Name:  src.Name,
		Mode:  src.Mode,
	})
}

// ExpireStaleTriageItems is the retention sweep behind the scheduler's
// triage_retention_sweep job. triage.Capture stamps every item with an
// expires_at (triage.DefaultRetention); an item nobody resolved by then
// leaves the queue as `expired` rather than being deleted, because resolved
// items are what the auto-classifier learns from (K61).
func (h *Handler) ExpireStaleTriageItems(ctx context.Context) (int64, error) {
	return h.Queries.ExpirePendingTriageItems(ctx, triageRetentionSweepBatch)
}

func (h *Handler) publishTriageResolved(workspaceID, itemID pgtype.UUID, state string) {
	h.Bus.Publish(events.Event{
		Type:        protocol.EventTriageResolved,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"item_id": util.UUIDToString(itemID),
			"state":   state,
		},
	})
}
