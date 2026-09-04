package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Handoff packets (K17): what a run leaves for whoever takes the issue
// next — objective, decisions, evidence, failed attempts, next action.
// Written by the agent (or a member) through the API, or by the server at
// completion when the run wrote none. Never mutated: a correction is a new
// packet. The latest packet rides the next claim so the resuming agent
// reads it before anything else.

const AuditHandoffPacketCreated = "handoff_packet.created"

type HandoffPacketRequest struct {
	RunID          string   `json:"run_id"`
	Objective      string   `json:"objective"`
	Decisions      []string `json:"decisions"`
	Evidence       []string `json:"evidence"`
	FailedAttempts []string `json:"failed_attempts"`
	NextAction     string   `json:"next_action"`
}

type HandoffPacketResponse struct {
	ID             string   `json:"id"`
	RunID          string   `json:"run_id"`
	IssueID        string   `json:"issue_id"`
	Objective      string   `json:"objective"`
	Decisions      []string `json:"decisions"`
	Evidence       []string `json:"evidence"`
	FailedAttempts []string `json:"failed_attempts"`
	NextAction     string   `json:"next_action"`
	CreatedByType  string   `json:"created_by_type"`
	CreatedByID    *string  `json:"created_by_id"`
	CreatedAt      string   `json:"created_at"`
}

func handoffPacketToResponse(p db.HandoffPacket) HandoffPacketResponse {
	return HandoffPacketResponse{
		ID: uuidToString(p.ID), RunID: uuidToString(p.RunID), IssueID: uuidToString(p.IssueID), Objective: p.Objective,
		Decisions: jsonStrings(p.Decisions), Evidence: jsonStrings(p.Evidence), FailedAttempts: jsonStrings(p.FailedAttempts),
		NextAction: p.NextAction.String, CreatedByType: p.CreatedByType, CreatedByID: uuidToPtr(p.CreatedByID), CreatedAt: timestampToString(p.CreatedAt),
	}
}

func cleanLines(in []string) []string {
	out := []string{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// CreateHandoffPacket: POST /api/issues/{id}/handoff-packet.
func (h *Handler) CreateHandoffPacket(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req HandoffPacketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Objective) == "" {
		writeError(w, http.StatusBadRequest, "objective is required")
		return
	}
	runID, ok := parseUUIDOrBadRequest(w, req.RunID, "run_id")
	if !ok {
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), runID)
	if err != nil || task.IssueID != issue.ID {
		writeError(w, http.StatusBadRequest, "run_id is not a run of this issue")
		return
	}
	createdByType, createdByID := "member", parseUUID(requestUserID(r))
	if isMachineCredentialActor(r) {
		if strings.TrimSpace(r.Header.Get("X-Task-ID")) != uuidToString(task.ID) {
			writeError(w, http.StatusForbidden, "a run may only hand off its own work")
			return
		}
		createdByType, createdByID = "agent", task.AgentID
	}
	packet, err := h.Queries.CreateHandoffPacket(r.Context(), db.CreateHandoffPacketParams{
		ID: dbid.NewV7(), RunID: task.ID, WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, Objective: strings.TrimSpace(req.Objective),
		Decisions: profileJSON(cleanLines(req.Decisions)), Evidence: profileJSON(cleanLines(req.Evidence)), FailedAttempts: profileJSON(cleanLines(req.FailedAttempts)),
		NextAction:    pgtype.Text{String: strings.TrimSpace(req.NextAction), Valid: strings.TrimSpace(req.NextAction) != ""},
		CreatedByType: createdByType, CreatedByID: createdByID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create handoff packet")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, createdByType, uuidToString(createdByID), AuditHandoffPacketCreated, "issue", issue.ID, map[string]any{"packet_id": uuidToString(packet.ID), "run_id": uuidToString(task.ID)}, nil)
	h.publishIssueAuxChanged(r, issue, createdByType, uuidToString(createdByID))
	writeJSON(w, http.StatusCreated, handoffPacketToResponse(packet))
}

// ListHandoffPackets: GET /api/issues/{id}/handoff-packets — chronological.
func (h *Handler) ListHandoffPackets(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListHandoffPackets(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list handoff packets")
		return
	}
	out := make([]HandoffPacketResponse, 0, len(rows))
	for _, p := range rows {
		out = append(out, handoffPacketToResponse(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"packets": out})
}

// GetLatestHandoffPacket: GET /api/issues/{id}/handoff-packet/latest.
func (h *Handler) GetLatestHandoffPacket(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	p, err := h.Queries.GetLatestHandoffPacket(r.Context(), issue.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"packet": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"packet": handoffPacketToResponse(p)})
}

// latestHandoffPacket is what the next claim carries; nil when none.
func (h *Handler) latestHandoffPacket(ctx context.Context, issueID pgtype.UUID) *HandoffPacketResponse {
	if !issueID.Valid {
		return nil
	}
	p, err := h.Queries.GetLatestHandoffPacket(ctx, issueID)
	if err != nil {
		return nil
	}
	out := handoffPacketToResponse(p)
	return &out
}

// ensureCompletionHandoffPacket (K17): a run that completes without writing
// its own packet gets a system one, so the next hand always has an
// objective and a next action. ponytail: derived from the issue and the
// delivery pointers, no second model summarising the transcript.
func (h *Handler) ensureCompletionHandoffPacket(ctx context.Context, task db.AgentTaskQueue, prURL string) {
	if !task.IssueID.Valid {
		return
	}
	if n, err := h.Queries.CountHandoffPacketsForRun(ctx, task.ID); err != nil || n > 0 {
		return
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return
	}
	evidence := []string{}
	next := "Review the run output on the issue and decide the next step."
	if prURL = strings.TrimSpace(prURL); prURL != "" {
		evidence = append(evidence, prURL)
		next = "Review the pull request " + prURL + " and merge or request changes."
	}
	if task.BranchName.Valid && task.BranchName.String != "" {
		evidence = append(evidence, "branch "+task.BranchName.String)
	}
	if _, err := h.Queries.CreateHandoffPacket(ctx, db.CreateHandoffPacketParams{
		ID: dbid.NewV7(), RunID: task.ID, WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, Objective: issue.Title,
		Decisions: profileJSON(nil), Evidence: profileJSON(evidence), FailedAttempts: profileJSON(nil),
		NextAction: pgtype.Text{String: next, Valid: true}, CreatedByType: "system",
	}); err != nil {
		slog.Warn("handoff packet: completion packet failed", "task_id", uuidToString(task.ID), "error", err)
	}
}
