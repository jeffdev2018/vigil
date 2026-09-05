package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Undo for agent actions (K69).
//
// Every side effect an agent run produces through the API is journaled
// (service.RecordAgentEffect) with the state it replaced. A member can then
// reverse one effect or a whole run, newest effect first, within the
// workspace's undo window. Reversal is best effort per effect: what could not
// be reversed is reported, never hidden. Undoing too many of one agent's runs
// in a day trips a breaker that lowers the agent's trust mode one notch and
// files an inbox item for the managers.

const (
	AuditAgentEffectReversed = "agent_effect.reversed"
	AuditUndoSettings        = "undo.settings_updated"
	InboxTypeUndoBreaker     = "agent_undo_breaker"
)

// effectActor is the run behind a task-token request: the only writes the
// journal cares about. Any other credential (member JWT, cloud PAT) is not a
// run, so nothing is recorded.
func effectActor(r *http.Request) (agentID, taskID pgtype.UUID, ok bool) {
	if r.Header.Get("X-Actor-Source") != "task_token" {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if err := agentID.Scan(strings.TrimSpace(r.Header.Get("X-Agent-ID"))); err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if err := taskID.Scan(strings.TrimSpace(r.Header.Get("X-Task-ID"))); err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return agentID, taskID, true
}

// recordEffect journals one side effect of the request's run, if it is one.
func (h *Handler) recordEffect(r *http.Request, wsID, issueID pgtype.UUID, kind, targetType string, targetID pgtype.UUID, before, after map[string]any, reversible bool) {
	agentID, taskID, ok := effectActor(r)
	if !ok {
		return
	}
	service.RecordAgentEffect(r.Context(), h.Queries, service.AgentEffectParams{
		WorkspaceID: wsID, TaskID: taskID, AgentID: agentID, IssueID: issueID,
		Kind: kind, TargetType: targetType, TargetID: targetID, Before: before, After: after, Reversible: reversible,
	})
}

// recordIssueEffects journals every field a run changed on an issue.
func (h *Handler) recordIssueEffects(r *http.Request, prev, next db.Issue) {
	agentID, taskID, ok := effectActor(r)
	if !ok {
		return
	}
	h.recordIssueEffectsFor(r.Context(), taskID, agentID, prev, next)
}

// recordIssueEffectsFor is recordIssueEffects for a known run (replay of a held write).
func (h *Handler) recordIssueEffectsFor(ctx context.Context, taskID, agentID pgtype.UUID, prev, next db.Issue) {
	record := func(kind string, before, after map[string]any) {
		service.RecordAgentEffect(ctx, h.Queries, service.AgentEffectParams{
			WorkspaceID: next.WorkspaceID, TaskID: taskID, AgentID: agentID, IssueID: next.ID,
			Kind: kind, TargetType: "issue", TargetID: next.ID, Before: before, After: after, Reversible: true,
		})
	}
	rec := func(kind, field string, before, after any) {
		record(kind, map[string]any{"field": field, "value": before}, map[string]any{"field": field, "value": after})
	}
	if prev.Status != next.Status {
		rec(service.EffectIssueStatus, "status", prev.Status, next.Status)
	}
	if prev.AssigneeType.String != next.AssigneeType.String || uuidToString(prev.AssigneeID) != uuidToString(next.AssigneeID) {
		record(service.EffectIssueField,
			map[string]any{"field": "assignee", "assignee_type": textToPtr(prev.AssigneeType), "assignee_id": uuidToPtr(prev.AssigneeID)},
			map[string]any{"field": "assignee", "assignee_type": textToPtr(next.AssigneeType), "assignee_id": uuidToPtr(next.AssigneeID)})
	}
	if prev.Priority != next.Priority {
		rec(service.EffectIssueField, "priority", prev.Priority, next.Priority)
	}
	if prev.Title != next.Title {
		rec(service.EffectIssueField, "title", prev.Title, next.Title)
	}
	if textToPtr(prev.Description) != textToPtr(next.Description) && (prev.Description.String != next.Description.String || prev.Description.Valid != next.Description.Valid) {
		rec(service.EffectIssueField, "description", textToPtr(prev.Description), textToPtr(next.Description))
	}
	if datePtrChanged(prev.DueDate, next.DueDate) {
		rec(service.EffectIssueField, "due_date", dateToPtr(prev.DueDate), dateToPtr(next.DueDate))
	}
	if datePtrChanged(prev.StartDate, next.StartDate) {
		rec(service.EffectIssueField, "start_date", dateToPtr(prev.StartDate), dateToPtr(next.StartDate))
	}
	if uuidToString(prev.ProjectID) != uuidToString(next.ProjectID) {
		rec(service.EffectIssueField, "project_id", uuidToPtr(prev.ProjectID), uuidToPtr(next.ProjectID))
	}
}

func datePtrChanged(a, b pgtype.Date) bool {
	pa, pb := dateToPtr(a), dateToPtr(b)
	if (pa == nil) != (pb == nil) {
		return true
	}
	return pa != nil && *pa != *pb
}

// --- responses -------------------------------------------------------------

type AgentEffectResponse struct {
	ID             string          `json:"id"`
	TaskID         string          `json:"task_id"`
	AgentID        string          `json:"agent_id"`
	AgentName      string          `json:"agent_name"`
	IssueID        *string         `json:"issue_id"`
	Kind           string          `json:"kind"`
	TargetType     string          `json:"target_type"`
	TargetID       string          `json:"target_id"`
	Before         json.RawMessage `json:"before"`
	After          json.RawMessage `json:"after"`
	Reversible     bool            `json:"reversible"`
	Status         string          `json:"status"`
	DecisionID     *string         `json:"decision_id"`
	Payload        json.RawMessage `json:"payload"`
	ReversedAt     *time.Time      `json:"reversed_at"`
	ReversedByType *string         `json:"reversed_by_type"`
	ReverseError   *string         `json:"reverse_error"`
	WithinWindow   bool            `json:"within_window"`
	ExpiresAt      time.Time       `json:"expires_at"`
	CreatedAt      time.Time       `json:"created_at"`
}

func (h *Handler) effectsToResponse(ctx context.Context, rows []db.AgentEffect, window time.Duration) []AgentEffectResponse {
	names := map[string]string{}
	out := make([]AgentEffectResponse, 0, len(rows))
	now := time.Now()
	for _, e := range rows {
		agentKey := uuidToString(e.AgentID)
		if _, seen := names[agentKey]; !seen {
			names[agentKey] = ""
			if a, err := h.Queries.GetAgent(ctx, e.AgentID); err == nil {
				names[agentKey] = a.Name
			}
		}
		expires := e.CreatedAt.Time.Add(window)
		resp := AgentEffectResponse{
			ID: uuidToString(e.ID), TaskID: uuidToString(e.TaskID), AgentID: agentKey, AgentName: names[agentKey],
			IssueID: uuidToPtr(e.IssueID), Kind: e.Kind, TargetType: e.TargetType, TargetID: uuidToString(e.TargetID),
			Before: json.RawMessage(e.Before), After: json.RawMessage(e.After), Reversible: e.Reversible,
			Status: e.Status, DecisionID: uuidToPtr(e.DecisionID), Payload: json.RawMessage(e.Payload),
			WithinWindow: now.Before(expires), ExpiresAt: expires, CreatedAt: e.CreatedAt.Time,
		}
		if len(resp.Payload) == 0 {
			resp.Payload = json.RawMessage("{}")
		}
		if len(resp.Before) == 0 {
			resp.Before = json.RawMessage("{}")
		}
		if len(resp.After) == 0 {
			resp.After = json.RawMessage("{}")
		}
		if e.ReversedAt.Valid {
			t := e.ReversedAt.Time
			resp.ReversedAt = &t
		}
		resp.ReversedByType = textToPtr(e.ReversedByType)
		resp.ReverseError = textToPtr(e.ReverseError)
		out = append(out, resp)
	}
	return out
}

func (h *Handler) undoSettings(ctx context.Context, wsID pgtype.UUID) service.Undo {
	ws, err := h.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		return service.DefaultUndo
	}
	return service.UndoSettings(ws.Settings)
}

// ListIssueAgentEffects: GET /api/issues/{id}/agent-effects.
func (h *Handler) ListIssueAgentEffects(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListAgentEffectsForIssue(r.Context(), db.ListAgentEffectsForIssueParams{WorkspaceID: issue.WorkspaceID, IssueID: issue.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent effects")
		return
	}
	settings := h.undoSettings(r.Context(), issue.WorkspaceID)
	writeJSON(w, http.StatusOK, map[string]any{
		"effects":      h.effectsToResponse(r.Context(), rows, time.Duration(settings.WindowHours)*time.Hour),
		"window_hours": settings.WindowHours,
	})
}

// --- undo ------------------------------------------------------------------

type undoSkip struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

type undoBreaker struct {
	Tripped   bool   `json:"tripped"`
	TrustMode string `json:"trust_mode,omitempty"`
}

type undoReport struct {
	Reversed int                   `json:"reversed"`
	Skipped  []undoSkip            `json:"skipped"`
	Breaker  undoBreaker           `json:"breaker"`
	Effects  []AgentEffectResponse `json:"effects"`
}

// UndoTaskEffects: POST /api/tasks/{id}/undo reverses every reversible,
// not-yet-reversed effect of one run, newest first.
func (h *Handler) UndoTaskEffects(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	issue, ok := h.loadIssueForUser(w, r, uuidToString(task.IssueID))
	if !ok {
		return
	}
	rows, err := h.Queries.ListAgentEffectsForTask(r.Context(), db.ListAgentEffectsForTaskParams{WorkspaceID: issue.WorkspaceID, TaskID: taskID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent effects")
		return
	}
	report := h.undoEffects(r, issue.WorkspaceID, userID, rows)
	writeJSON(w, http.StatusOK, report)
}

// UndoAgentEffect: POST /api/agent-effects/{id}/undo reverses one effect.
func (h *Handler) UndoAgentEffect(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	effectID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	wsID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(wsID)); !ok {
		return
	}
	eff, err := h.Queries.GetAgentEffect(r.Context(), db.GetAgentEffectParams{ID: effectID, WorkspaceID: wsID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent effect not found")
		return
	}
	if eff.IssueID.Valid {
		if _, ok := h.loadIssueForUser(w, r, uuidToString(eff.IssueID)); !ok {
			return
		}
	}
	report := h.undoEffects(r, wsID, userID, []db.AgentEffect{eff})
	writeJSON(w, http.StatusOK, report)
}

// undoEffects reverses rows in the order given (callers pass newest first),
// marks each outcome on the row, audits the batch and checks the breaker.
func (h *Handler) undoEffects(r *http.Request, wsID pgtype.UUID, userID string, rows []db.AgentEffect) undoReport {
	ctx := r.Context()
	settings := h.undoSettings(ctx, wsID)
	window := time.Duration(settings.WindowHours) * time.Hour
	report := undoReport{Skipped: []undoSkip{}}
	touchedIssues := map[string]pgtype.UUID{}
	var agentID pgtype.UUID
	for _, eff := range rows {
		agentID = eff.AgentID
		skip := func(reason string) {
			report.Skipped = append(report.Skipped, undoSkip{ID: uuidToString(eff.ID), Kind: eff.Kind, Reason: reason})
		}
		switch {
		case eff.Status != service.EffectApplied:
			// A held write (pending, approved, rejected) is not itself a
			// change: its replay is journaled as applied rows of its own.
			skip("not_applied")
			continue
		case eff.ReversedAt.Valid:
			skip("already_reversed")
			continue
		case !eff.Reversible:
			skip("not_reversible")
			continue
		case time.Now().After(eff.CreatedAt.Time.Add(window)):
			skip("window_expired")
			continue
		}
		if err := h.reverseEffect(ctx, eff); err != nil {
			slog.Warn("undo: reverse effect failed", append(logger.RequestAttrs(r), "effect_id", uuidToString(eff.ID), "kind", eff.Kind, "error", err)...)
			_ = h.Queries.SetAgentEffectReverseError(ctx, db.SetAgentEffectReverseErrorParams{ID: eff.ID, WorkspaceID: wsID, ReverseError: pgtype.Text{String: truncate(err.Error(), 500), Valid: true}})
			skip("reverse_failed: " + truncate(err.Error(), 200))
			continue
		}
		if _, err := h.Queries.MarkAgentEffectReversed(ctx, db.MarkAgentEffectReversedParams{
			ID: eff.ID, WorkspaceID: wsID, ReversedByType: pgtype.Text{String: "member", Valid: true}, ReversedByID: parseUUID(userID),
		}); err != nil {
			skip("mark_failed")
			continue
		}
		report.Reversed++
		if eff.IssueID.Valid {
			touchedIssues[uuidToString(eff.IssueID)] = eff.IssueID
		}
		h.audit(ctx, wsID, "member", userID, AuditAgentEffectReversed, eff.TargetType, eff.TargetID,
			map[string]any{"effect_id": uuidToString(eff.ID), "kind": eff.Kind, "task_id": uuidToString(eff.TaskID), "agent_id": uuidToString(eff.AgentID)}, nil)
	}
	for _, issueID := range touchedIssues {
		h.broadcastIssueAfterUndo(ctx, wsID, issueID, userID)
	}
	if report.Reversed > 0 && agentID.Valid {
		report.Breaker = h.checkUndoBreaker(ctx, wsID, agentID, userID, settings)
	}
	// Fresh rows so the client sees reversed_at / reverse_error without a refetch.
	fresh := make([]db.AgentEffect, 0, len(rows))
	for _, eff := range rows {
		if e, err := h.Queries.GetAgentEffect(ctx, db.GetAgentEffectParams{ID: eff.ID, WorkspaceID: wsID}); err == nil {
			fresh = append(fresh, e)
		}
	}
	report.Effects = h.effectsToResponse(ctx, fresh, window)
	return report
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// reverseEffect applies the inverse of one journaled effect.
func (h *Handler) reverseEffect(ctx context.Context, eff db.AgentEffect) error {
	var before map[string]any
	if len(eff.Before) > 0 {
		if err := json.Unmarshal(eff.Before, &before); err != nil {
			return fmt.Errorf("decode before: %w", err)
		}
	}
	switch eff.Kind {
	case service.EffectIssueStatus:
		status, _ := before["value"].(string)
		if status == "" {
			return errors.New("no previous status recorded")
		}
		_, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: eff.TargetID, Status: status, WorkspaceID: eff.WorkspaceID})
		return err
	case service.EffectIssueField:
		return h.reverseIssueField(ctx, eff, before)
	case service.EffectCommentCreate:
		deleted, err := h.Queries.DeleteComment(ctx, db.DeleteCommentParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		if !deleted.Changed {
			return errors.New("comment already gone")
		}
		h.unindexWhy(ctx, whySourceComment, eff.TargetID)
		h.publish(protocol.EventCommentDeleted, uuidToString(eff.WorkspaceID), "member", "", map[string]any{
			"comment_id": uuidToString(eff.TargetID), "issue_id": uuidToString(eff.IssueID),
		})
		return nil
	case service.EffectCommentUpdate:
		content, _ := before["content"].(string)
		existing, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		updated, err := h.Queries.UpdateComment(ctx, db.UpdateCommentParams{ID: eff.TargetID, Content: content, SourceTaskID: existing.SourceTaskID})
		if err != nil {
			return err
		}
		comment := updated.Comment()
		h.indexWhy(ctx, comment.WorkspaceID, whySourceComment, comment.ID, comment.IssueID, comment.Content)
		h.publish(protocol.EventCommentUpdated, uuidToString(eff.WorkspaceID), "member", "", map[string]any{
			"comment": commentToResponse(comment, nil, nil), "issue_id": uuidToString(comment.IssueID),
		})
		return nil
	case service.EffectNoteCreate:
		rows, err := h.Queries.DeleteWorkspaceNote(ctx, db.DeleteWorkspaceNoteParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		if rows == 0 {
			return errors.New("note already gone")
		}
		h.publish(protocol.EventWorkspaceNoteDeleted, uuidToString(eff.WorkspaceID), "member", "", map[string]any{"note_id": uuidToString(eff.TargetID)})
		return nil
	case service.EffectNoteUpdate:
		note, err := h.Queries.GetWorkspaceNote(ctx, db.GetWorkspaceNoteParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		params := db.UpdateWorkspaceNoteParams{ID: note.ID, WorkspaceID: note.WorkspaceID, ExpectedRevision: note.Revision}
		if v, ok := before["title"].(string); ok {
			params.Title = pgtype.Text{String: v, Valid: true}
		}
		if v, ok := before["content"].(string); ok {
			params.Content = pgtype.Text{String: v, Valid: true}
		}
		if v, ok := before["pinned"].(bool); ok {
			params.Pinned = pgtype.Bool{Bool: v, Valid: true}
		}
		if raw, ok := before["tags"].([]any); ok {
			tags := make([]string, 0, len(raw))
			for _, t := range raw {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
			params.Tags = tags
		}
		updated, err := h.Queries.UpdateWorkspaceNote(ctx, params)
		if err != nil {
			return err
		}
		h.publish(protocol.EventWorkspaceNoteUpdated, uuidToString(eff.WorkspaceID), "member", "", map[string]any{"note": workspaceNoteToResponse(updated)})
		return nil
	case service.EffectNoteArchive:
		updated, err := h.Queries.SetWorkspaceNoteArchived(ctx, db.SetWorkspaceNoteArchivedParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		h.publish(protocol.EventWorkspaceNoteUpdated, uuidToString(eff.WorkspaceID), "member", "", map[string]any{"note": workspaceNoteToResponse(updated)})
		return nil
	case service.EffectTriageVerdict:
		item, err := h.Queries.ClearTriageItemVerdict(ctx, db.ClearTriageItemVerdictParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("triage item is no longer pending")
		}
		if err != nil {
			return err
		}
		h.publishTriageUpdated(eff.WorkspaceID, item.ID)
		return nil
	default:
		return fmt.Errorf("no reversal for kind %q", eff.Kind)
	}
}

// reverseIssueField puts one field back, leaving every other field as it is now.
func (h *Handler) reverseIssueField(ctx context.Context, eff db.AgentEffect, before map[string]any) error {
	current, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
	if err != nil {
		return err
	}
	params := db.UpdateIssueParams{
		ID: current.ID, Title: pgtype.Text{String: current.Title, Valid: true}, Description: current.Description,
		Status: pgtype.Text{String: current.Status, Valid: true}, Priority: pgtype.Text{String: current.Priority, Valid: true},
		AssigneeType: current.AssigneeType, AssigneeID: current.AssigneeID, StartDate: current.StartDate, DueDate: current.DueDate,
		ParentIssueID: current.ParentIssueID, ProjectID: current.ProjectID, Stage: current.Stage,
	}
	field, _ := before["field"].(string)
	str := func(v any) pgtype.Text {
		if s, ok := v.(string); ok {
			return pgtype.Text{String: s, Valid: true}
		}
		return pgtype.Text{}
	}
	uid := func(v any) pgtype.UUID {
		if s, ok := v.(string); ok {
			if u, err := util.ParseUUID(s); err == nil {
				return u
			}
		}
		return pgtype.UUID{}
	}
	date := func(v any) pgtype.Date {
		if s, ok := v.(string); ok {
			if t, err := time.Parse("2006-01-02", s); err == nil {
				return pgtype.Date{Time: t, Valid: true}
			}
		}
		return pgtype.Date{}
	}
	switch field {
	case "assignee":
		params.AssigneeType = str(before["assignee_type"])
		params.AssigneeID = uid(before["assignee_id"])
	case "priority":
		params.Priority = str(before["value"])
	case "title":
		params.Title = str(before["value"])
	case "description":
		params.Description = str(before["value"])
	case "due_date":
		params.DueDate = date(before["value"])
	case "start_date":
		params.StartDate = date(before["value"])
	case "project_id":
		params.ProjectID = uid(before["value"])
	default:
		return fmt.Errorf("no reversal for issue field %q", field)
	}
	_, err = h.Queries.UpdateIssue(ctx, params)
	return err
}

// broadcastIssueAfterUndo emits issue:updated with every flag set: the
// reversal may have touched any column, and a refetch is cheaper than a diff.
func (h *Handler) broadcastIssueAfterUndo(ctx context.Context, wsID, issueID pgtype.UUID, userID string) {
	issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: wsID})
	if err != nil {
		return
	}
	resp := issueToResponse(issue, h.getIssuePrefix(ctx, wsID))
	h.fillStatusCategory(ctx, wsID, &resp)
	h.publish(protocol.EventIssueUpdated, uuidToString(wsID), "member", userID, map[string]any{
		"issue": resp, "assignee_changed": true, "status_changed": true, "priority_changed": true, "project_changed": true,
		"start_date_changed": true, "due_date_changed": true, "undo": true,
	})
}

// checkUndoBreaker lowers the agent's trust mode one notch and alerts the
// managers when this undo crossed the threshold of runs undone in 24 hours.
func (h *Handler) checkUndoBreaker(ctx context.Context, wsID, agentID pgtype.UUID, userID string, settings service.Undo) undoBreaker {
	if settings.BreakerThreshold <= 0 {
		return undoBreaker{}
	}
	since := pgtype.Timestamptz{Time: time.Now().Add(-24 * time.Hour), Valid: true}
	count, err := h.Queries.CountAgentRunsReversedSince(ctx, db.CountAgentRunsReversedSinceParams{WorkspaceID: wsID, AgentID: agentID, Since: since})
	if err != nil || count != int64(settings.BreakerThreshold) {
		// Fires once, on the undo that reaches the threshold; more undos the
		// same day do not pile up notifications.
		return undoBreaker{}
	}
	agent, err := h.Queries.GetAgent(ctx, agentID)
	if err != nil {
		return undoBreaker{}
	}
	out := undoBreaker{Tripped: true, TrustMode: agent.TrustMode}
	if rank, ok := trustRank[agent.TrustMode]; ok && rank > 0 {
		lowered := trustOrder[rank-1]
		if _, err := h.Queries.SetAgentTrustMode(ctx, db.SetAgentTrustModeParams{ID: agent.ID, TrustMode: lowered}); err == nil {
			out.TrustMode = lowered
			reason := fmt.Sprintf("undo breaker: %d run(s) undone in 24h", count)
			if _, err := h.Queries.CreateTrustModeChange(ctx, db.CreateTrustModeChangeParams{
				ID: dbid.NewV7(), WorkspaceID: wsID, AgentID: agent.ID, FromMode: agent.TrustMode, ToMode: lowered,
				Reason: pgtype.Text{String: reason, Valid: true}, TriggeredByType: "system_suggested", TriggeredByID: parseUUID(userID),
			}); err != nil {
				slog.Warn("undo breaker: record trust change failed", "agent_id", uuidToString(agent.ID), "error", err)
			}
			h.audit(ctx, wsID, "system", "", AuditTrustModeChanged, "agent", agent.ID, map[string]any{"from": agent.TrustMode, "to": lowered, "reason": reason}, nil)
		}
	}
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, wsID)
	if err != nil {
		return out
	}
	details, _ := json.Marshal(map[string]any{"agent_id": uuidToString(agent.ID), "runs_undone": count, "trust_mode": out.TrustMode})
	for _, rcpt := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: wsID, RecipientType: rcpt.Type, RecipientID: rcpt.ID, Type: InboxTypeUndoBreaker, Severity: "action_required",
			Title:     fmt.Sprintf("%d runs of %s were undone today", count, agent.Name),
			Body:      pgtype.Text{String: "The agent now runs in " + out.TrustMode + " mode. Review its recent runs before raising its trust again.", Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(wsID), "system", "", map[string]any{"item": inboxToResponse(item)})
	}
	return out
}

// --- settings --------------------------------------------------------------

// GetUndoSettings: GET /api/undo-settings.
func (h *Handler) GetUndoSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, h.undoSettings(r.Context(), wsUUID))
}

// PutUndoSettings: PUT /api/undo-settings {window_hours, breaker_threshold}.
func (h *Handler) PutUndoSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req service.Undo
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.WindowHours < 1 || req.WindowHours > 24*30 || req.BreakerThreshold < 0 || req.BreakerThreshold > 100 {
		writeError(w, http.StatusBadRequest, "window_hours must be between 1 and 720 and breaker_threshold between 0 and 100")
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	settings := map[string]any{}
	if len(ws.Settings) > 0 {
		_ = json.Unmarshal(ws.Settings, &settings)
	}
	settings["undo"] = req
	raw, _ := json.Marshal(settings)
	if _, err := h.Queries.UpdateWorkspace(r.Context(), db.UpdateWorkspaceParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save undo settings")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditUndoSettings, "workspace", wsUUID, map[string]any{"window_hours": req.WindowHours, "breaker_threshold": req.BreakerThreshold}, nil)
	writeJSON(w, http.StatusOK, req)
}
