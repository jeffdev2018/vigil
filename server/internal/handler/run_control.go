package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Pause, steer, resume (K19). A human asks a running run to pause; the
// daemon honours it at the next safe boundary (after a tool result) and
// acks with the session pointers; the human leaves steering instructions
// as typed task messages; resume enqueues a follow-up run on the same
// runtime session with the instructions as its handoff. Nothing here
// interrupts a tool call in flight.

const (
	AuditRunPaused       = "run.pause_requested"
	AuditRunSteered      = "run.steered"
	AuditRunResumed      = "run.resumed"
	SteeringMessageType  = "steering_instruction"
	ErrCodeRunNotRunning = "run_not_running"
	ErrCodeRunNotPaused  = "run_not_paused"
	ErrCodeRunNoSteering = "run_no_instruction"
	pausedResumeNoteLead = "Steering instruction from a human while this run was paused. Apply it before anything else, and keep the work already done:"
)

type RunControlState struct {
	TaskID          string   `json:"task_id"`
	Status          string   `json:"status"`
	PausePending    bool     `json:"pause_pending"`
	Instructions    []string `json:"instructions"`
	ResumedByTaskID *string  `json:"resumed_by_task_id"`
}

func (h *Handler) runControlState(r *http.Request, task db.AgentTaskQueue) RunControlState {
	rows, _ := h.Queries.ListSteeringInstructions(r.Context(), task.ID)
	instructions := make([]string, 0, len(rows))
	for _, m := range rows {
		instructions = append(instructions, m.Content.String)
	}
	return RunControlState{TaskID: uuidToString(task.ID), Status: task.Status, PausePending: task.PauseRequestedAt.Valid, Instructions: instructions, ResumedByTaskID: uuidToPtr(task.ResumedByTaskID)}
}

// controllableRun loads the issue and its running or paused run.
func (h *Handler) controllableRun(w http.ResponseWriter, r *http.Request) (db.Issue, db.AgentTaskQueue, bool) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.Issue{}, db.AgentTaskQueue{}, false
	}
	task, err := h.Queries.GetControllableTaskForIssue(r.Context(), issue.ID)
	if err != nil {
		writeErrorCode(w, http.StatusConflict, ErrCodeRunNotRunning, "no running or paused run on this issue")
		return db.Issue{}, db.AgentTaskQueue{}, false
	}
	return issue, task, true
}

// GetRunControlState: GET /api/issues/{id}/run/state.
func (h *Handler) GetRunControlState(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	task, err := h.Queries.GetControllableTaskForIssue(r.Context(), issue.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"run": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": h.runControlState(r, task)})
}

// PauseRun: POST /api/issues/{id}/run/pause — 202 until the daemon acks.
func (h *Handler) PauseRun(w http.ResponseWriter, r *http.Request) {
	issue, task, ok := h.controllableRun(w, r)
	if !ok {
		return
	}
	if task.Status == "paused" {
		writeJSON(w, http.StatusOK, map[string]any{"run": h.runControlState(r, task)})
		return
	}
	updated, err := h.Queries.RequestTaskPause(r.Context(), task.ID)
	if err != nil {
		writeErrorCode(w, http.StatusConflict, ErrCodeRunNotRunning, "the run is no longer running")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", requestUserID(r), AuditRunPaused, "task", task.ID, map[string]any{"issue_id": uuidToString(issue.ID)}, nil)
	h.publish(protocol.EventTaskProgress, uuidToString(issue.WorkspaceID), "member", requestUserID(r), map[string]any{"task_id": uuidToString(task.ID), "issue_id": uuidToString(issue.ID), "pause_pending": true})
	writeJSON(w, http.StatusAccepted, map[string]any{"run": h.runControlState(r, updated)})
}

// SteerRun: POST /api/issues/{id}/run/steer {instruction} — only on a paused run.
func (h *Handler) SteerRun(w http.ResponseWriter, r *http.Request) {
	issue, task, ok := h.controllableRun(w, r)
	if !ok {
		return
	}
	if task.Status != "paused" {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeRunNotPaused, "pause the run before steering it")
		return
	}
	var req struct {
		Instruction string `json:"instruction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Instruction) == "" {
		writeError(w, http.StatusBadRequest, "instruction is required")
		return
	}
	seq, err := h.Queries.NextTaskMessageSeq(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to place the instruction")
		return
	}
	if _, err := h.Queries.CreateTaskMessage(r.Context(), db.CreateTaskMessageParams{
		ID: dbid.NewV7(), TaskID: task.ID, Seq: seq, Type: SteeringMessageType, Content: pgtype.Text{String: strings.TrimSpace(req.Instruction), Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the instruction")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", requestUserID(r), AuditRunSteered, "task", task.ID, map[string]any{"issue_id": uuidToString(issue.ID), "seq": seq}, nil)
	h.publish(protocol.EventTaskProgress, uuidToString(issue.WorkspaceID), "member", requestUserID(r), map[string]any{"task_id": uuidToString(task.ID), "issue_id": uuidToString(issue.ID), "steered": true})
	writeJSON(w, http.StatusCreated, map[string]any{"run": h.runControlState(r, task)})
}

// ResumeRun: POST /api/issues/{id}/run/resume — a follow-up run on the same
// session, carrying every instruction left while paused.
func (h *Handler) ResumeRun(w http.ResponseWriter, r *http.Request) {
	issue, task, ok := h.controllableRun(w, r)
	if !ok {
		return
	}
	if task.Status != "paused" {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeRunNotPaused, "only a paused run can be resumed")
		return
	}
	rows, err := h.Queries.ListSteeringInstructions(r.Context(), task.ID)
	if err != nil || (len(rows) == 0 && !task.PreemptedAt.Valid) {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeRunNoSteering, "leave an instruction before resuming")
		return
	}
	var note strings.Builder
	if len(rows) == 0 {
		// Preemption (K41): a suspended run continues as it was.
		note.WriteString("Resumed by a human after being suspended for an urgent issue. Continue from where you stopped; nothing else changed.")
	} else {
		note.WriteString(pausedResumeNoteLead)
	}
	for _, m := range rows {
		note.WriteString("\n- " + m.Content.String)
	}
	child, err := h.TaskService.EnqueueTaskForIssueWithHandoff(r.Context(), issue, note.String(), parseUUID(requestUserID(r)))
	if err != nil {
		writeError(w, http.StatusConflict, "could not queue the resumed run: "+err.Error())
		return
	}
	if task.SessionID.Valid && task.SessionID.String != "" {
		if err := h.Queries.SetTaskResumeContext(r.Context(), db.SetTaskResumeContextParams{ID: child.ID, SessionID: task.SessionID, WorkDir: task.WorkDir}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to pin the session on the resumed run")
			return
		}
	}
	if _, err := h.Queries.MarkTaskResumed(r.Context(), db.MarkTaskResumedParams{ID: task.ID, ResumedByTaskID: child.ID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to close the paused run")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", requestUserID(r), AuditRunResumed, "task", task.ID, map[string]any{"issue_id": uuidToString(issue.ID), "resumed_by_task_id": uuidToString(child.ID), "instructions": len(rows)}, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"run": h.runControlState(r, child), "paused_task_id": uuidToString(task.ID)})
}

// AckTaskPaused: POST /api/daemon/tasks/{taskId}/paused — the daemon
// stopped at a safe boundary and reports where the session lives.
func (h *Handler) AckTaskPaused(w http.ResponseWriter, r *http.Request) {
	task, ok := h.requireDaemonTaskAccess(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	var req struct {
		SessionID  string `json:"session_id"`
		WorkDir    string `json:"work_dir"`
		BranchName string `json:"branch_name"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	paused, err := h.Queries.MarkTaskPaused(r.Context(), db.MarkTaskPausedParams{
		ID: task.ID, SessionID: pgtype.Text{String: req.SessionID, Valid: req.SessionID != ""}, WorkDir: pgtype.Text{String: req.WorkDir, Valid: req.WorkDir != ""}, BranchName: pgtype.Text{String: req.BranchName, Valid: req.BranchName != ""},
	})
	if err != nil {
		// The run ended naturally first: the pause is moot, not an error.
		writeJSON(w, http.StatusOK, map[string]any{"status": task.Status, "paused": false})
		return
	}
	h.revokeRunSecrets(r.Context(), task.ID, "run_paused", "system", "")
	if paused.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(r.Context(), paused.IssueID); err == nil {
			h.publish(protocol.EventTaskProgress, uuidToString(issue.WorkspaceID), "system", "", map[string]any{"task_id": uuidToString(paused.ID), "issue_id": uuidToString(issue.ID), "status": "paused"})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": paused.Status, "paused": true})
}
