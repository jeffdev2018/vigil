package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Racing attempts (F11 / JEF-6). One issue, N independent attempts, the human
// keeps one. The data and dispatch layers landed with the run_group table and
// TaskService.EnqueueRunGroupAttempt; this file is the API that reaches them.
//
// An attempt is an ordinary agent_task_queue row carrying run_group_id, so the
// losing attempts are cancelled through CancelTaskByUser — the same path the
// issue cancel button uses. That is what makes the daemon clean their branch
// and worktree: settling a race is, for the daemon, N-1 user cancellations.
const (
	AuditRunGroup = "run_group"

	// A race needs at least two runners, and past a handful the cost of the
	// fan-out stops buying anything a human will actually read side by side.
	// Written once onto run_group.attempt_count, which is what the cap was
	// checked against.
	minRunGroupAttempts = 2
	maxRunGroupAttempts = 5

	ErrCodeRunGroupActive  = "run_group_already_active"
	ErrCodeRunGroupSettled = "run_group_already_settled"
)

type RunGroupAttemptRequest struct {
	AgentID string `json:"agent_id"`
	Model   string `json:"model"`
}

type StartRunGroupRequest struct {
	Attempts []RunGroupAttemptRequest `json:"attempts"`
	Note     string                   `json:"note"`
}

// RunGroupAttemptResponse is one attempt. diff_unified is the consolidated
// patch when the daemon recorded one; diff_truncated says the run produced a
// diff too large to store, so "no unified diff" is not "no changes".
type RunGroupAttemptResponse struct {
	TaskID        string          `json:"task_id"`
	AgentID       string          `json:"agent_id"`
	Status        string          `json:"status"`
	Model         string          `json:"model"`
	DiffStat      json.RawMessage `json:"diff_stat"`
	DiffUnified   *string         `json:"diff_unified"`
	DiffTruncated bool            `json:"diff_truncated"`
	CreatedAt     string          `json:"created_at"`
	CompletedAt   *string         `json:"completed_at"`
}

type RunGroupResponse struct {
	ID           string                    `json:"id"`
	IssueID      string                    `json:"issue_id"`
	Status       string                    `json:"status"`
	AttemptCount int32                     `json:"attempt_count"`
	WinnerTaskID *string                   `json:"winner_task_id"`
	CreatedBy    *string                   `json:"created_by"`
	CreatedAt    string                    `json:"created_at"`
	SettledAt    *string                   `json:"settled_at"`
	Attempts     []RunGroupAttemptResponse `json:"attempts"`
}

func runGroupAttemptToResponse(task db.AgentTaskQueue) RunGroupAttemptResponse {
	out := RunGroupAttemptResponse{
		TaskID:      uuidToString(task.ID),
		AgentID:     uuidToString(task.AgentID),
		Status:      task.Status,
		Model:       task.ModelOverride.String,
		DiffUnified: textToPtr(task.DiffUnified),
		CreatedAt:   timestampToString(task.CreatedAt),
		CompletedAt: timestampToPtr(task.CompletedAt),
	}
	if len(task.DiffStat) > 0 {
		out.DiffStat = json.RawMessage(task.DiffStat)
		// See RecordTaskDiff: a stat with no unified diff is exactly the
		// "the patch exceeded the stored bound" case.
		out.DiffTruncated = !task.DiffUnified.Valid
	}
	return out
}

func runGroupToResponse(group db.RunGroup, attempts []db.AgentTaskQueue) RunGroupResponse {
	out := RunGroupResponse{
		ID:           uuidToString(group.ID),
		IssueID:      uuidToString(group.IssueID),
		Status:       group.Status,
		AttemptCount: group.AttemptCount,
		WinnerTaskID: uuidToPtr(group.WinnerTaskID),
		CreatedBy:    uuidToPtr(group.CreatedBy),
		CreatedAt:    timestampToString(group.CreatedAt),
		SettledAt:    timestampToPtr(group.SettledAt),
		Attempts:     make([]RunGroupAttemptResponse, 0, len(attempts)),
	}
	for _, task := range attempts {
		out.Attempts = append(out.Attempts, runGroupAttemptToResponse(task))
	}
	return out
}

// StartRunGroup: POST /api/issues/{id}/run-groups.
func (h *Handler) StartRunGroup(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req StartRunGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Attempts) < minRunGroupAttempts || len(req.Attempts) > maxRunGroupAttempts {
		writeError(w, http.StatusBadRequest, "a race runs between 2 and 5 attempts")
		return
	}
	// Resolve every agent before creating anything: a half-created group whose
	// second agent does not exist would hold the issue's one open-race slot.
	agents := make([]db.Agent, 0, len(req.Attempts))
	for _, attempt := range req.Attempts {
		agentID, ok := parseUUIDOrBadRequest(w, attempt.AgentID, "agent id")
		if !ok {
			return
		}
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: issue.WorkspaceID})
		if err != nil || agent.ArchivedAt.Valid {
			writeError(w, http.StatusUnprocessableEntity, "agent not found in this workspace")
			return
		}
		agents = append(agents, agent)
	}
	if active, err := h.Queries.CountActiveRunGroupsForIssue(r.Context(), issue.ID); err == nil && active > 0 {
		writeErrorCode(w, http.StatusConflict, ErrCodeRunGroupActive, "a race is already running on this issue")
		return
	}

	userID := parseUUID(requestUserID(r))
	groupID := dbid.NewV7()
	group, err := h.Queries.CreateRunGroup(r.Context(), db.CreateRunGroupParams{
		ID:           groupID,
		WorkspaceID:  issue.WorkspaceID,
		IssueID:      issue.ID,
		AttemptCount: int32(len(req.Attempts)),
		CreatedBy:    userID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the race")
		return
	}

	// model_override is passed through, not checked against the runtime's model
	// catalogue: that catalogue is daemon-reported and routinely absent, so a
	// strict check would refuse valid models whenever discovery has not run.
	// The daemon is the layer that knows, and it already reports an unusable
	// model as a run failure.
	note := strings.TrimSpace(req.Note)
	queued := make([]db.AgentTaskQueue, 0, len(agents))
	for i, agent := range agents {
		task, err := h.TaskService.EnqueueRunGroupAttempt(r.Context(), issue, agent.ID, group.ID, strings.TrimSpace(req.Attempts[i].Model), note, userID)
		if err != nil {
			h.unwindRunGroup(r, group, queued)
			writeError(w, http.StatusInternalServerError, "failed to queue the attempt: "+err.Error())
			return
		}
		queued = append(queued, task)
	}

	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditRunGroup, "issue", issue.ID, map[string]any{
		"run_group_id": uuidToString(group.ID), "attempts": len(queued), "started": true,
	}, nil)
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	writeJSON(w, http.StatusCreated, map[string]any{"group": runGroupToResponse(group, queued)})
}

// unwindRunGroup rolls a partial fan-out back: the attempts already queued are
// cancelled and the group is abandoned, so the issue's one open-race slot is
// released instead of being held by a group that will never run.
func (h *Handler) unwindRunGroup(r *http.Request, group db.RunGroup, queued []db.AgentTaskQueue) {
	for _, task := range queued {
		if _, err := h.TaskService.CancelTaskByUser(r.Context(), task.ID); err != nil {
			slog.Warn("run group: unwind cancel failed", "task_id", uuidToString(task.ID), "error", err)
		}
	}
	if _, err := h.Queries.AbandonRunGroup(r.Context(), group.ID); err != nil {
		slog.Warn("run group: unwind abandon failed", "run_group_id", uuidToString(group.ID), "error", err)
	}
}

// ListIssueRunGroups: GET /api/issues/{id}/run-groups.
func (h *Handler) ListIssueRunGroups(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	groups, err := h.Queries.ListRunGroupsForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list races")
		return
	}
	// One read for every attempt of every group, then a group-by in memory.
	tasks, err := h.Queries.ListRunGroupTasksForIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list attempts")
		return
	}
	byGroup := map[string][]db.AgentTaskQueue{}
	for _, task := range tasks {
		key := uuidToString(task.RunGroupID)
		byGroup[key] = append(byGroup[key], task)
	}
	out := make([]RunGroupResponse, 0, len(groups))
	for _, group := range groups {
		out = append(out, runGroupToResponse(group, byGroup[uuidToString(group.ID)]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": out})
}

// loadRunGroupForUser is the tenant guard for the group-scoped endpoints: the
// group must belong to the request's workspace, and the caller must be able to
// see the issue it races on.
func (h *Handler) loadRunGroupForUser(w http.ResponseWriter, r *http.Request) (db.RunGroup, db.Issue, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "run group id")
	if !ok {
		return db.RunGroup{}, db.Issue{}, false
	}
	wsUUID, err := util.ParseUUID(h.resolveWorkspaceID(r))
	if err != nil {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.RunGroup{}, db.Issue{}, false
	}
	group, err := h.Queries.GetRunGroupInWorkspace(r.Context(), db.GetRunGroupInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "race not found")
		return db.RunGroup{}, db.Issue{}, false
	}
	issue, ok := h.loadIssueForUser(w, r, uuidToString(group.IssueID))
	if !ok {
		return db.RunGroup{}, db.Issue{}, false
	}
	return group, issue, true
}

// SettleRunGroup: POST /api/run-groups/{id}/settle {winner_task_id}. The winner
// is kept as it is; every other attempt still open is cancelled through the
// ordinary user-cancel path so the daemon drops its branch and worktree.
func (h *Handler) SettleRunGroup(w http.ResponseWriter, r *http.Request) {
	group, issue, ok := h.loadRunGroupForUser(w, r)
	if !ok {
		return
	}
	var req struct {
		WinnerTaskID string `json:"winner_task_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	winnerID, ok := parseUUIDOrBadRequest(w, req.WinnerTaskID, "winner_task_id")
	if !ok {
		return
	}
	attempts, err := h.Queries.ListRunGroupTasks(r.Context(), group.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list attempts")
		return
	}
	if !runGroupContainsTask(attempts, winnerID) {
		writeError(w, http.StatusBadRequest, "winner_task_id is not an attempt of this race")
		return
	}
	settled, err := h.Queries.SettleRunGroup(r.Context(), db.SettleRunGroupParams{ID: group.ID, WinnerTaskID: winnerID})
	if err != nil {
		writeErrorCode(w, http.StatusConflict, ErrCodeRunGroupSettled, "this race is no longer running")
		return
	}
	h.cancelRunGroupLosers(r, attempts, winnerID)
	h.finishRunGroup(w, r, settled, issue, map[string]any{
		"run_group_id": uuidToString(settled.ID), "winner_task_id": uuidToString(winnerID), "settled": true,
	})
}

// AbandonRunGroup: POST /api/run-groups/{id}/abandon. Same as settling, minus a
// winner: every open attempt is cancelled and the race kept nothing.
func (h *Handler) AbandonRunGroup(w http.ResponseWriter, r *http.Request) {
	group, issue, ok := h.loadRunGroupForUser(w, r)
	if !ok {
		return
	}
	attempts, err := h.Queries.ListRunGroupTasks(r.Context(), group.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list attempts")
		return
	}
	abandoned, err := h.Queries.AbandonRunGroup(r.Context(), group.ID)
	if err != nil {
		writeErrorCode(w, http.StatusConflict, ErrCodeRunGroupSettled, "this race is no longer running")
		return
	}
	h.cancelRunGroupLosers(r, attempts, pgtype.UUID{})
	h.finishRunGroup(w, r, abandoned, issue, map[string]any{
		"run_group_id": uuidToString(abandoned.ID), "abandoned": true,
	})
}

func runGroupContainsTask(attempts []db.AgentTaskQueue, taskID pgtype.UUID) bool {
	for _, task := range attempts {
		if task.ID == taskID {
			return true
		}
	}
	return false
}

// cancelRunGroupLosers cancels every attempt but the winner that has not
// reached a terminal status yet. CancelTaskByUser, not a raw status update: the
// human decided, and only that path settles the delegated-failure recovery
// signal, broadcasts task:cancelled and lets the daemon clean up.
func (h *Handler) cancelRunGroupLosers(r *http.Request, attempts []db.AgentTaskQueue, winnerID pgtype.UUID) {
	for _, task := range attempts {
		if winnerID.Valid && task.ID == winnerID {
			continue
		}
		switch task.Status {
		case "queued", "dispatched", "running", "waiting_local_directory", "deferred":
		default:
			continue
		}
		if _, err := h.TaskService.CancelTaskByUser(r.Context(), task.ID); err != nil {
			slog.Warn("run group: cancel losing attempt failed", "task_id", uuidToString(task.ID), "error", err)
		}
	}
}

func (h *Handler) finishRunGroup(w http.ResponseWriter, r *http.Request, group db.RunGroup, issue db.Issue, details map[string]any) {
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditRunGroup, "issue", issue.ID, details, nil)
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	// Re-read the attempts: the cancellations above changed their status.
	attempts, err := h.Queries.ListRunGroupTasks(r.Context(), group.ID)
	if err != nil {
		slog.Warn("run group: reload attempts failed", "run_group_id", uuidToString(group.ID), "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"group": runGroupToResponse(group, attempts)})
}

// recordRunGroupTaskDiff stores what one attempt changed, as the daemon
// measured it at Finalize.
//
// Best-effort and attempt-only: an ordinary run never carries a diff, and a
// task outside a group is ignored even if one arrives — its columns are not
// read by anything, and writing them would make the group-membership rule
// depend on the caller rather than on the row.
//
// A nil stat writes nothing. A stat with no patch is the truncated case and
// must still be written: it is exactly what tells the compare view the attempt
// produced a diff too large to show.
func (h *Handler) recordRunGroupTaskDiff(ctx context.Context, task db.AgentTaskQueue, stat *protocol.TaskDiffStat, unified string) {
	if stat == nil || !task.RunGroupID.Valid {
		return
	}
	encoded, err := json.Marshal(stat)
	if err != nil {
		slog.Warn("run group: could not encode the attempt's diff stat", "task_id", uuidToString(task.ID), "error", err)
		return
	}
	if _, err := h.Queries.RecordTaskDiff(ctx, db.RecordTaskDiffParams{
		ID:          task.ID,
		DiffStat:    encoded,
		DiffUnified: strToText(unified),
	}); err != nil {
		slog.Warn("run group: could not record the attempt's diff; the run stands, its column shows nothing",
			"task_id", uuidToString(task.ID), "run_group_id", uuidToString(task.RunGroupID), "error", err)
	}
}
