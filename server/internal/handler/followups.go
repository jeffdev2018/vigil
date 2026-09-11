package handler

// Follow-ups (OS plan, vague B, réveil programmé): "come back to this issue
// at 9 tomorrow with this note" as one verb for members, agent runs, the
// CLI and MCP. A follow-up is a deferred run of the issue's agent; it shows
// on the issue, in the runs page (blocked on "deferred") and in the agenda,
// and it can be cancelled by anyone who can see the issue.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	AuditFollowupScheduled = "followup.scheduled"
	AuditFollowupCancelled = "followup.cancelled"
)

type FollowupResponse struct {
	ID              string  `json:"id"`
	IssueID         string  `json:"issue_id"`
	AgentID         string  `json:"agent_id"`
	AgentName       string  `json:"agent_name"`
	FiresAt         string  `json:"fires_at"`
	Note            string  `json:"note"`
	ScheduledByType string  `json:"scheduled_by_type"`
	ScheduledByID   *string `json:"scheduled_by_id"`
	CreatedAt       string  `json:"created_at"`
}

func followupResponse(id, issueID, agentID pgtype.UUID, agentName string, fireAt pgtype.Timestamptz, summary pgtype.Text, createdAt pgtype.Timestamptz, originator, ref pgtype.UUID) FollowupResponse {
	byType := "agent"
	if originator.Valid {
		byType = "member"
	}
	return FollowupResponse{
		ID: uuidToString(id), IssueID: uuidToString(issueID), AgentID: uuidToString(agentID), AgentName: agentName,
		FiresAt: timestampToString(fireAt), Note: service.FollowupNoteFromSummary(summary.String),
		ScheduledByType: byType, ScheduledByID: uuidToPtr(ref), CreatedAt: timestampToString(createdAt),
	}
}

func (h *Handler) followupSettings(ctx context.Context, wsID pgtype.UUID) service.FollowupSettings {
	ws, err := h.Queries.GetWorkspace(ctx, wsID)
	if err != nil {
		slog.Warn("followup settings: get workspace failed", "workspace_id", uuidToString(wsID), "error", err)
		return service.FollowupSettingsFrom(nil)
	}
	return service.FollowupSettingsFrom(ws.Settings)
}

// scheduleFollowup is the shared core: resolves the agent, files the task,
// audits and publishes. actorType/actorID come from resolveActor.
func (h *Handler) scheduleFollowup(ctx context.Context, wsID pgtype.UUID, issue db.Issue, agentID pgtype.UUID, when, note, actorType, actorID string) (FollowupResponse, int, error) {
	agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: wsID})
	if err != nil {
		return FollowupResponse{}, http.StatusNotFound, errors.New("agent not found in this workspace")
	}
	byID := parseUUID(actorID)
	task, err := service.ScheduleFollowup(ctx, h.Queries, service.FollowupInput{
		WorkspaceID: wsID, IssueID: issue.ID, Agent: agent, When: when, Note: note,
		ByType: actorType, ByID: byID, Settings: h.followupSettings(ctx, wsID),
	})
	if err != nil {
		var budget service.ErrFollowupBudget
		if errors.As(err, &budget) {
			return FollowupResponse{}, http.StatusTooManyRequests, err
		}
		return FollowupResponse{}, http.StatusBadRequest, err
	}
	resp := followupResponse(task.ID, task.IssueID, task.AgentID, agent.Name, task.FireAt, task.TriggerSummary, task.CreatedAt, task.OriginatorUserID, task.TriggerEvidenceRefID)
	h.audit(ctx, wsID, actorType, actorID, AuditFollowupScheduled, "agent_task", task.ID, map[string]any{"issue_id": uuidToString(issue.ID), "agent_id": uuidToString(agent.ID), "fires_at": resp.FiresAt, "note": resp.Note}, nil)
	h.publish(protocol.EventFollowupChanged, uuidToString(wsID), actorType, actorID, map[string]any{"issue_id": uuidToString(issue.ID), "followup_id": uuidToString(task.ID), "change": "scheduled", "followup": resp})
	return resp, http.StatusCreated, nil
}

// POST /api/issues/{id}/followups {when, note, agent_id?}
// A run schedules its own agent; a member names the agent or relies on the
// issue's agent assignee.
func (h *Handler) CreateIssueFollowup(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// Project roles (K60): scheduling/cancelling a follow-up spawns real agent
	// work on the issue, so it is a write, not a read.
	if !h.requireProjectWrite(w, r, issue.ProjectID) {
		return
	}
	var req struct {
		When    string `json:"when"`
		Note    string `json:"note"`
		AgentID string `json:"agent_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wsID := issue.WorkspaceID
	actorType, actorID := h.resolveActor(r, userID, uuidToString(wsID))
	var agentID pgtype.UUID
	switch {
	case actorType == "agent":
		id, ok := parseUUIDOrBadRequest(w, actorID, "agent id")
		if !ok {
			return
		}
		agentID = id
	case req.AgentID != "":
		id, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
		if !ok {
			return
		}
		agentID = id
	case issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid:
		agentID = issue.AssigneeID
	default:
		writeError(w, http.StatusBadRequest, "agent_id is required when the issue is not assigned to an agent")
		return
	}
	resp, status, err := h.scheduleFollowup(r.Context(), wsID, issue, agentID, req.When, req.Note, actorType, actorID)
	if err != nil {
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, status, map[string]any{"followup": resp})
}

// GET /api/issues/{id}/followups — the pending follow-ups, soonest first.
func (h *Handler) ListIssueFollowups(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListIssueFollowups(r.Context(), db.ListIssueFollowupsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list follow-ups")
		return
	}
	out := make([]FollowupResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, followupResponse(row.ID, row.IssueID, row.AgentID, row.AgentName, row.FireAt, row.TriggerSummary, row.CreatedAt, row.OriginatorUserID, row.TriggerEvidenceRefID))
	}
	settings := h.followupSettings(r.Context(), issue.WorkspaceID)
	writeJSON(w, http.StatusOK, map[string]any{"followups": out, "budget": settings})
}

// DELETE /api/issues/{id}/followups/{followupId} — cancel before it fires.
func (h *Handler) CancelIssueFollowup(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	// Project roles (K60): scheduling/cancelling a follow-up spawns real agent
	// work on the issue, so it is a write, not a read.
	if !h.requireProjectWrite(w, r, issue.ProjectID) {
		return
	}
	followupID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "followupId"), "followup id")
	if !ok {
		return
	}
	row, err := h.Queries.GetIssueFollowup(r.Context(), db.GetIssueFollowupParams{ID: followupID, IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "follow-up not found")
		return
	}
	if row.Status != "deferred" {
		writeError(w, http.StatusConflict, "this follow-up already fired or was cancelled")
		return
	}
	if _, err := h.Queries.CancelAgentTask(r.Context(), followupID); err != nil {
		writeError(w, http.StatusConflict, "this follow-up already fired or was cancelled")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditFollowupCancelled, "agent_task", followupID, map[string]any{"issue_id": uuidToString(issue.ID), "note": service.FollowupNoteFromSummary(row.TriggerSummary.String)}, nil)
	h.publish(protocol.EventFollowupChanged, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{"issue_id": uuidToString(issue.ID), "followup_id": uuidToString(followupID), "change": "cancelled"})
	w.WriteHeader(http.StatusNoContent)
}

// AgendaFollowup is a scheduled wake-up in the unified agenda.
type AgendaFollowup struct {
	ID         string `json:"id"`
	IssueID    string `json:"issue_id"`
	Identifier string `json:"identifier"`
	IssueTitle string `json:"issue_title"`
	AgentID    string `json:"agent_id"`
	AgentName  string `json:"agent_name"`
	FiresAt    string `json:"fires_at"`
	Note       string `json:"note"`
}

func (h *Handler) agendaFollowups(ctx context.Context, wsID pgtype.UUID, prefix string, from, to time.Time) []AgendaFollowup {
	out := []AgendaFollowup{}
	rows, err := h.Queries.ListWorkspaceFollowupsBetween(ctx, db.ListWorkspaceFollowupsBetweenParams{WorkspaceID: wsID, Since: pgtype.Timestamptz{Time: from, Valid: true}, Until: pgtype.Timestamptz{Time: to, Valid: true}})
	if err != nil {
		slog.Warn("agenda followups: list failed", "workspace_id", uuidToString(wsID), "error", err)
		return out
	}
	for _, row := range rows {
		out = append(out, AgendaFollowup{
			ID: uuidToString(row.ID), IssueID: uuidToString(row.IssueID), Identifier: prefix + "-" + strconv.Itoa(int(row.IssueNumber)), IssueTitle: row.IssueTitle,
			AgentID: uuidToString(row.AgentID), AgentName: row.AgentName, FiresAt: timestampToString(row.FireAt), Note: service.FollowupNoteFromSummary(row.TriggerSummary.String),
		})
	}
	return out
}
