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
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// "Show me first" (K69, lot 2).
//
// An agent in preview mode gets its writes journaled as pending effects
// instead of applied: the API answers 202 with the unchanged resource. When
// the run ends, one decision (K63) lists every pending effect with the exact
// action to approve; approval replays them in order and journals the result
// as ordinary, reversible effects; a refusal, or a failed run, discards them.
// The gate is two-step by construction: the decision shows the payload, its
// question names the action, and the impact line spells out what cannot be
// undone afterwards.

const (
	AuditEffectPreviewAsked   = "effect_preview.asked"
	AuditEffectPreviewApplied = "effect_preview.applied"
	previewApplyOptionID      = "apply_effects"
	previewDiscardOptionID    = "discard_effects"
)

// previewRun is the run behind the request when its agent holds writes for approval.
func (h *Handler) previewRun(r *http.Request) (agentID, taskID pgtype.UUID, ok bool) {
	agentID, taskID, ok = effectActor(r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	if !service.AgentPreviewsEffects(r.Context(), h.Queries, agentID) {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return agentID, taskID, true
}

// recordPending journals a held write and returns the row so the 202 body can name it.
func (h *Handler) recordPending(r *http.Request, agentID, taskID, wsID, issueID pgtype.UUID, kind, targetType string, targetID pgtype.UUID, after, payload map[string]any, reversible bool) (db.AgentEffect, bool) {
	eff, err := service.RecordPendingAgentEffect(r.Context(), h.Queries, service.AgentEffectParams{
		WorkspaceID: wsID, TaskID: taskID, AgentID: agentID, IssueID: issueID,
		Kind: kind, TargetType: targetType, TargetID: targetID, After: after, Reversible: reversible,
	}, payload)
	if err != nil {
		slog.Warn("agent effect: record pending failed", "kind", kind, "task_id", uuidToString(taskID), "error", err)
		return db.AgentEffect{}, false
	}
	return eff, true
}

// writePending answers a held write: 202, the resource as it still is, and
// the pending effect id so the agent can refer to it.
func writePending(w http.ResponseWriter, eff db.AgentEffect, resource any) {
	w.Header().Set("X-Pending-Effect", uuidToString(eff.ID))
	writeJSON(w, http.StatusAccepted, resource)
}

// rawFieldsToMap decodes an issue update's request fields for the payload.
func rawFieldsToMap(raw map[string]json.RawMessage) map[string]any {
	out := map[string]any{}
	for k, v := range raw {
		switch k {
		case "expected_revision", "title_base", "description_base", "attachment_ids":
			continue
		}
		var any_ any
		if err := json.Unmarshal(v, &any_); err == nil {
			out[k] = any_
		}
	}
	return out
}

// settlePendingEffects runs at the end of a task: a failed run drops its
// held writes; a completed one files the decision that lists them.
func (h *Handler) settlePendingEffects(ctx context.Context, task db.AgentTaskQueue, succeeded bool) {
	pending, err := h.Queries.ListPendingAgentEffectsForTask(ctx, task.ID)
	if err != nil || len(pending) == 0 {
		return
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	if !succeeded {
		for _, eff := range pending {
			_, _ = h.Queries.SetAgentEffectStatus(ctx, db.SetAgentEffectStatusParams{ID: eff.ID, WorkspaceID: eff.WorkspaceID, Status: service.EffectRejected, Error: pgtype.Text{String: "run failed", Valid: true}})
		}
		return
	}
	agentName := "the agent"
	if agent, err := h.Queries.GetAgent(ctx, task.AgentID); err == nil {
		agentName = agent.Name
	}
	lines := make([]string, 0, len(pending))
	irreversible := 0
	for i, eff := range pending {
		lines = append(lines, fmt.Sprintf("%d. %s", i+1, describePending(eff)))
		if !eff.Reversible {
			irreversible++
		}
	}
	question := fmt.Sprintf("Show me first · %s proposes %d change(s) on this issue\n\n%s\n\nDo you approve applying these %d changes, yes or no?",
		agentName, len(pending), strings.Join(lines, "\n"), len(pending))
	impact := fmt.Sprintf("%d change(s) applied and reversible for the undo window", len(pending))
	if irreversible > 0 {
		impact = fmt.Sprintf("%d change(s) applied; %d of them cannot be undone afterwards", len(pending), irreversible)
	}
	options, _ := json.Marshal([]DecisionOption{
		{ID: previewApplyOptionID, Label: "Approve and apply", Impact: impact},
		{ID: previewDiscardOptionID, Label: "Discard", Impact: "nothing is applied"},
	})
	decision, err := h.Queries.CreateIssueDecision(ctx, db.CreateIssueDecisionParams{
		WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, TaskID: task.ID, AskedByType: "agent", AskedByID: task.AgentID,
		Question: question, Options: options, Urgency: "high", SlaDeadlineAt: h.decisionDeadline(ctx, issue.WorkspaceID),
	})
	if err != nil {
		slog.Warn("effect preview: file decision failed", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	if _, err := h.Queries.SetAgentEffectsDecision(ctx, db.SetAgentEffectsDecisionParams{TaskID: task.ID, DecisionID: decision.ID}); err != nil {
		slog.Warn("effect preview: link effects failed", "task_id", uuidToString(task.ID), "error", err)
	}
	h.notifyDecisionRequested(ctx, issue, decision, "agent", uuidToString(task.AgentID))
	h.audit(ctx, issue.WorkspaceID, "agent", uuidToString(task.AgentID), AuditEffectPreviewAsked, "issue", issue.ID,
		map[string]any{"task_id": uuidToString(task.ID), "decision_id": uuidToString(decision.ID), "effects": len(pending)}, nil)
}

// describePending renders one held write for the decision card.
func describePending(eff db.AgentEffect) string {
	var payload map[string]any
	_ = json.Unmarshal(eff.Payload, &payload)
	str := func(k string) string {
		if v, ok := payload[k].(string); ok {
			return v
		}
		return ""
	}
	switch eff.Kind {
	case service.EffectIssueUpdate:
		parts := make([]string, 0, len(payload))
		for k, v := range payload {
			parts = append(parts, fmt.Sprintf("%s → %v", k, v))
		}
		return "Issue update: " + strings.Join(parts, ", ")
	case service.EffectCommentCreate:
		return "Comment: " + truncate(str("content"), 200)
	case service.EffectNoteCreate:
		return "New note: " + str("title")
	case service.EffectNoteUpdate:
		return "Note edit: " + truncate(str("content"), 120)
	case service.EffectNoteArchive:
		return "Archive note"
	case service.EffectCommentDelete:
		return "Delete comment"
	case service.EffectNoteDelete:
		return "Delete note"
	case service.EffectTriageVerdict:
		return "Triage verdict: " + str("verdict") + " (" + truncate(str("reason"), 120) + ")"
	default:
		return eff.Kind
	}
}

// applyPreviewForDecision settles a "show me first" decision: apply or
// discard the held writes. Returns false when the decision is not one.
func (h *Handler) applyPreviewForDecision(ctx context.Context, decision db.IssueDecision, optionID, actorType, actorID string) bool {
	effects, err := h.Queries.ListAgentEffectsForDecision(ctx, decision.ID)
	if err != nil || len(effects) == 0 {
		return false
	}
	applied, rejected := 0, 0
	for _, eff := range effects {
		if eff.Status != service.EffectPending {
			continue
		}
		if optionID != previewApplyOptionID {
			_, _ = h.Queries.SetAgentEffectStatus(ctx, db.SetAgentEffectStatusParams{ID: eff.ID, WorkspaceID: eff.WorkspaceID, Status: service.EffectRejected})
			rejected++
			continue
		}
		if err := h.applyPendingEffect(ctx, eff); err != nil {
			slog.Warn("effect preview: apply failed", "effect_id", uuidToString(eff.ID), "kind", eff.Kind, "error", err)
			_, _ = h.Queries.SetAgentEffectStatus(ctx, db.SetAgentEffectStatusParams{ID: eff.ID, WorkspaceID: eff.WorkspaceID, Status: service.EffectRejected, Error: pgtype.Text{String: truncate(err.Error(), 500), Valid: true}})
			rejected++
			continue
		}
		_, _ = h.Queries.SetAgentEffectStatus(ctx, db.SetAgentEffectStatusParams{ID: eff.ID, WorkspaceID: eff.WorkspaceID, Status: service.EffectApproved})
		applied++
	}
	h.audit(ctx, decision.WorkspaceID, actorType, actorID, AuditEffectPreviewApplied, "issue", decision.IssueID,
		map[string]any{"decision_id": uuidToString(decision.ID), "option": optionID, "applied": applied, "rejected": rejected}, nil)
	// A human discarding a run's writes is a correction like an undo: it
	// counts toward the breaker.
	if optionID != previewApplyOptionID && rejected > 0 {
		h.checkUndoBreaker(ctx, decision.WorkspaceID, effects[0].AgentID, actorID, h.undoSettings(ctx, decision.WorkspaceID))
	}
	return true
}

// applyPendingEffect replays one held write with the run's attribution and
// journals the outcome as ordinary, reversible effects.
func (h *Handler) applyPendingEffect(ctx context.Context, eff db.AgentEffect) error {
	var payload map[string]any
	if len(eff.Payload) > 0 {
		if err := json.Unmarshal(eff.Payload, &payload); err != nil {
			return fmt.Errorf("decode payload: %w", err)
		}
	}
	str := func(k string) (string, bool) {
		v, ok := payload[k].(string)
		return v, ok
	}
	journal := func(kind, targetType string, targetID pgtype.UUID, before, after map[string]any, reversible bool) {
		service.RecordAgentEffect(ctx, h.Queries, service.AgentEffectParams{
			WorkspaceID: eff.WorkspaceID, TaskID: eff.TaskID, AgentID: eff.AgentID, IssueID: eff.IssueID,
			Kind: kind, TargetType: targetType, TargetID: targetID, Before: before, After: after, Reversible: reversible,
		})
	}
	switch eff.Kind {
	case service.EffectIssueUpdate:
		prev, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		params := db.UpdateIssueParams{
			ID: prev.ID, Title: pgtype.Text{String: prev.Title, Valid: true}, Description: prev.Description,
			Status: pgtype.Text{String: prev.Status, Valid: true}, Priority: pgtype.Text{String: prev.Priority, Valid: true},
			AssigneeType: prev.AssigneeType, AssigneeID: prev.AssigneeID, StartDate: prev.StartDate, DueDate: prev.DueDate,
			ParentIssueID: prev.ParentIssueID, ProjectID: prev.ProjectID, Stage: prev.Stage,
		}
		for k, v := range payload {
			s, isStr := v.(string)
			switch k {
			case "status":
				if isStr {
					params.Status = pgtype.Text{String: s, Valid: true}
				}
			case "priority":
				if isStr {
					params.Priority = pgtype.Text{String: s, Valid: true}
				}
			case "title":
				if isStr {
					params.Title = pgtype.Text{String: s, Valid: true}
				}
			case "description":
				params.Description = pgtype.Text{String: s, Valid: isStr}
			case "assignee_type":
				params.AssigneeType = pgtype.Text{String: s, Valid: isStr}
			case "assignee_id":
				params.AssigneeID = pgtype.UUID{}
				if isStr {
					if u, err := util.ParseUUID(s); err == nil {
						params.AssigneeID = u
					}
				}
			case "project_id":
				params.ProjectID = pgtype.UUID{}
				if isStr {
					if u, err := util.ParseUUID(s); err == nil {
						params.ProjectID = u
					}
				}
			case "due_date", "start_date":
				d := pgtype.Date{}
				if isStr {
					if t, err := time.Parse("2006-01-02", s); err == nil {
						d = pgtype.Date{Time: t, Valid: true}
					}
				}
				if k == "due_date" {
					params.DueDate = d
				} else {
					params.StartDate = d
				}
			}
		}
		next, err := h.Queries.UpdateIssue(ctx, params)
		if err != nil {
			return err
		}
		h.recordIssueEffectsFor(ctx, eff.TaskID, eff.AgentID, prev, next)
		if prev.Status != next.Status {
			h.audit(ctx, next.WorkspaceID, "agent", uuidToString(eff.AgentID), AuditIssueStatus, "issue", next.ID, map[string]any{"from": prev.Status, "to": next.Status}, nil)
		}
		h.broadcastIssueAfterUndo(ctx, eff.WorkspaceID, next.ID, "")
		return nil
	case service.EffectCommentCreate:
		content, _ := str("content")
		commentType, ok := str("type")
		if !ok || commentType == "" {
			commentType = "comment"
		}
		parentID := pgtype.UUID{}
		if s, ok := str("parent_id"); ok {
			if u, err := util.ParseUUID(s); err == nil {
				parentID = u
			}
		}
		created, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
			ID: dbid.NewV7(), IssueID: eff.IssueID, WorkspaceID: eff.WorkspaceID, AuthorType: "agent", AuthorID: eff.AgentID,
			Content: content, Type: commentType, ParentID: parentID, SourceTaskID: eff.TaskID,
		})
		if err != nil {
			return err
		}
		comment := created.Comment()
		h.indexWhy(ctx, comment.WorkspaceID, whySourceComment, comment.ID, comment.IssueID, comment.Content)
		h.publish(protocol.EventCommentCreated, uuidToString(eff.WorkspaceID), "agent", uuidToString(eff.AgentID), map[string]any{
			"comment": commentToResponse(comment, nil, nil), "issue_revision": created.IssueRevision,
		})
		journal(service.EffectCommentCreate, "comment", comment.ID, map[string]any{}, map[string]any{"type": comment.Type, "excerpt": truncate(comment.Content, 200)}, true)
		return nil
	case service.EffectNoteCreate:
		title, _ := str("title")
		content, _ := str("content")
		tags := []string{}
		if raw, ok := payload["tags"].([]any); ok {
			for _, t := range raw {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
		}
		pinned, _ := payload["pinned"].(bool)
		note, err := h.Queries.CreateWorkspaceNote(ctx, db.CreateWorkspaceNoteParams{
			ID: dbid.NewV7(), WorkspaceID: eff.WorkspaceID, Title: title, Content: content, Tags: tags, Pinned: pinned,
			Source: "agent", SourceAgentID: eff.AgentID, SourceTaskID: eff.TaskID, CreatedByType: "agent", CreatedByID: eff.AgentID,
		})
		if err != nil {
			return err
		}
		h.publish(protocol.EventWorkspaceNoteCreated, uuidToString(eff.WorkspaceID), "agent", uuidToString(eff.AgentID), map[string]any{"note": workspaceNoteToResponse(note)})
		journal(service.EffectNoteCreate, "workspace_note", note.ID, map[string]any{}, map[string]any{"title": note.Title}, true)
		return nil
	case service.EffectNoteUpdate:
		note, err := h.Queries.GetWorkspaceNote(ctx, db.GetWorkspaceNoteParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		params := db.UpdateWorkspaceNoteParams{ID: note.ID, WorkspaceID: note.WorkspaceID, ExpectedRevision: note.Revision}
		if v, ok := str("title"); ok {
			params.Title = pgtype.Text{String: v, Valid: true}
		}
		if v, ok := str("content"); ok {
			params.Content = pgtype.Text{String: v, Valid: true}
		}
		if v, ok := payload["pinned"].(bool); ok {
			params.Pinned = pgtype.Bool{Bool: v, Valid: true}
		}
		if raw, ok := payload["tags"].([]any); ok {
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
		h.publish(protocol.EventWorkspaceNoteUpdated, uuidToString(eff.WorkspaceID), "agent", uuidToString(eff.AgentID), map[string]any{"note": workspaceNoteToResponse(updated)})
		journal(service.EffectNoteUpdate, "workspace_note", note.ID,
			map[string]any{"title": note.Title, "content": note.Content, "tags": note.Tags, "pinned": note.Pinned},
			map[string]any{"title": updated.Title, "content": updated.Content, "tags": updated.Tags, "pinned": updated.Pinned}, true)
		return nil
	case service.EffectNoteArchive:
		note, err := h.Queries.GetWorkspaceNote(ctx, db.GetWorkspaceNoteParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		updated, err := h.Queries.SetWorkspaceNoteArchived(ctx, db.SetWorkspaceNoteArchivedParams{
			ID: note.ID, WorkspaceID: note.WorkspaceID, ArchivedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}, MergedInto: note.MergedInto,
		})
		if err != nil {
			return err
		}
		h.publish(protocol.EventWorkspaceNoteUpdated, uuidToString(eff.WorkspaceID), "agent", uuidToString(eff.AgentID), map[string]any{"note": workspaceNoteToResponse(updated)})
		journal(service.EffectNoteArchive, "workspace_note", note.ID, map[string]any{"archived": false}, map[string]any{"archived": true}, true)
		return nil
	case service.EffectTriageVerdict:
		verdict, _ := str("verdict")
		reason, _ := str("reason")
		encoded, _ := json.Marshal(TriageVerdict{Verdict: verdict, Reason: reason})
		item, err := h.Queries.SetTriageItemVerdict(ctx, db.SetTriageItemVerdictParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID, Verdict: encoded, VerdictAgentID: eff.AgentID})
		if err != nil {
			return err
		}
		h.publishTriageUpdated(eff.WorkspaceID, item.ID)
		journal(service.EffectTriageVerdict, "triage_item", item.ID, map[string]any{}, map[string]any{"verdict": verdict, "reason": reason, "verdict_revision": item.VerdictRevision}, true)
		return nil
	case service.EffectCommentDelete:
		comment, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		deleted, err := h.Queries.DeleteComment(ctx, db.DeleteCommentParams{ID: comment.ID, WorkspaceID: comment.WorkspaceID})
		if err != nil {
			return err
		}
		if !deleted.Changed {
			return errors.New("comment already gone")
		}
		h.unindexWhy(ctx, whySourceComment, comment.ID)
		h.publish(protocol.EventCommentDeleted, uuidToString(eff.WorkspaceID), "agent", uuidToString(eff.AgentID), map[string]any{
			"comment_id": uuidToString(comment.ID), "issue_id": uuidToString(comment.IssueID),
		})
		journal(service.EffectCommentDelete, "comment", comment.ID, commentEffectSnapshot(comment), map[string]any{}, true)
		return nil
	case service.EffectNoteDelete:
		note, err := h.Queries.GetWorkspaceNote(ctx, db.GetWorkspaceNoteParams{ID: eff.TargetID, WorkspaceID: eff.WorkspaceID})
		if err != nil {
			return err
		}
		if _, err := h.Queries.DeleteWorkspaceNote(ctx, db.DeleteWorkspaceNoteParams{ID: note.ID, WorkspaceID: note.WorkspaceID}); err != nil {
			return err
		}
		h.publish(protocol.EventWorkspaceNoteDeleted, uuidToString(eff.WorkspaceID), "agent", uuidToString(eff.AgentID), map[string]any{"note": workspaceNoteToResponse(note)})
		journal(service.EffectNoteDelete, "workspace_note", note.ID, noteEffectSnapshot(note), map[string]any{}, true)
		return nil
	default:
		return errors.New("no replay for kind " + eff.Kind)
	}
}

// --- effect mode -----------------------------------------------------------

// GetAgentEffectMode: GET /api/agents/{id}/effect-mode.
func (h *Handler) GetAgentEffectMode(w http.ResponseWriter, r *http.Request) {
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": uuidToString(agent.ID), "mode": agent.EffectMode})
}

// SetAgentEffectMode: PUT /api/agents/{id}/effect-mode {mode: apply|preview}.
func (h *Handler) SetAgentEffectMode(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	agent, ok := h.loadAgentForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.canManageAgent(w, r, agent) {
		return
	}
	var req struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || (req.Mode != service.EffectModeApply && req.Mode != service.EffectModePreview) {
		writeError(w, http.StatusBadRequest, "mode must be apply or preview")
		return
	}
	updated, err := h.Queries.SetAgentEffectMode(r.Context(), db.SetAgentEffectModeParams{ID: agent.ID, EffectMode: req.Mode})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the effect mode")
		return
	}
	h.audit(r.Context(), agent.WorkspaceID, "member", userID, AuditUndoSettings, "agent", agent.ID, map[string]any{"effect_mode": req.Mode, "from": agent.EffectMode}, nil)
	writeJSON(w, http.StatusOK, map[string]any{"agent_id": uuidToString(agent.ID), "mode": updated.EffectMode})
}
