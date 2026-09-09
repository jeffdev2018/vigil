package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/goalstate"
)

// Goal loop (long tasks): the goal an issue is worked toward and the state
// of the run chain pursuing it. The state itself is written by the judge
// (service.GoalLoopService); members write the goal, pause and resume the
// chain, and answer the agent's questions; a run asks its question here.

const (
	AuditGoalUpdated  = "goal.updated"
	AuditGoalPaused   = "goal.paused"
	AuditGoalResumed  = "goal.resumed"
	AuditGoalAnswered = "goal.answered"
	AuditGoalAsked    = "goal.asked"
)

// GetIssueGoal: GET /api/issues/{id}/goal → {goal: State|null}
func (h *Handler) GetIssueGoal(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if h.GoalLoop == nil {
		writeJSON(w, http.StatusOK, map[string]any{"goal": nil})
		return
	}
	st, err := h.GoalLoop.State(r.Context(), issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the goal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal": st})
}

// SetIssueGoal: PUT /api/issues/{id}/goal {goal, max_continuations}
func (h *Handler) SetIssueGoal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if h.GoalLoop == nil {
		writeError(w, http.StatusServiceUnavailable, "the goal loop is not available on this server")
		return
	}
	var req struct {
		Goal             string `json:"goal"`
		MaxContinuations *int   `json:"max_continuations"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	max := 0
	if req.MaxContinuations != nil {
		max = *req.MaxContinuations
	} else if st, err := h.GoalLoop.State(r.Context(), issue); err == nil && st != nil {
		max = st.MaxContinuations
	} else {
		max = service.GoalLoopSettingsFrom(nil).MaxContinuations
	}
	goal, err := h.GoalLoop.SetGoal(r.Context(), issue, req.Goal, max, "member", parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", userID, AuditGoalUpdated, "issue", issue.ID, map[string]any{"max_continuations": max}, nil)
	h.publishIssueAuxChanged(r, issue, "member", userID)
	writeJSON(w, http.StatusOK, map[string]any{"goal": service.GoalStateOf(goal)})
}

// PauseIssueGoal: POST /api/issues/{id}/goal/pause
func (h *Handler) PauseIssueGoal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if h.GoalLoop == nil {
		writeError(w, http.StatusServiceUnavailable, "the goal loop is not available on this server")
		return
	}
	goal, err := h.GoalLoop.Pause(r.Context(), issue)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to pause the goal loop")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", userID, AuditGoalPaused, "issue", issue.ID, nil, nil)
	h.publishIssueAuxChanged(r, issue, "member", userID)
	writeJSON(w, http.StatusOK, map[string]any{"goal": service.GoalStateOf(goal)})
}

// ResumeIssueGoal: POST /api/issues/{id}/goal/resume
func (h *Handler) ResumeIssueGoal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if h.GoalLoop == nil {
		writeError(w, http.StatusServiceUnavailable, "the goal loop is not available on this server")
		return
	}
	goal, err := h.GoalLoop.Resume(r.Context(), issue, parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resume the goal loop")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", userID, AuditGoalResumed, "issue", issue.ID, nil, nil)
	h.publishIssueAuxChanged(r, issue, "member", userID)
	writeJSON(w, http.StatusOK, map[string]any{"goal": service.GoalStateOf(goal)})
}

// AnswerIssueGoal: POST /api/issues/{id}/goal/answer {answer}
func (h *Handler) AnswerIssueGoal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if h.GoalLoop == nil {
		writeError(w, http.StatusServiceUnavailable, "the goal loop is not available on this server")
		return
	}
	var req struct {
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Answer) == "" {
		writeError(w, http.StatusBadRequest, "answer is required")
		return
	}
	name := ""
	if user, err := h.Queries.GetUser(r.Context(), parseUUID(userID)); err == nil {
		name = user.Name
	}
	goal, err := h.GoalLoop.Answer(r.Context(), issue, req.Answer, parseUUID(userID), name)
	switch {
	case errors.Is(err, service.ErrGoalNotWaiting):
		writeError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", userID, AuditGoalAnswered, "issue", issue.ID, nil, nil)
	h.publishIssueAuxChanged(r, issue, "member", userID)
	writeJSON(w, http.StatusOK, map[string]any{"goal": service.GoalStateOf(goal)})
}

// AskIssueGoalQuestion: POST /api/issues/{id}/goal/question {question, kind, options}
// A CLI run asks the team through its machine credential (X-Task-ID names
// the run); the goal loop turns it into a needs_user_input verdict when the
// run settles. A member may file one too, on the issue's last run, to put
// the chain on hold behind a question they want the agent to see answered.
func (h *Handler) AskIssueGoalQuestion(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if h.GoalLoop == nil {
		writeError(w, http.StatusServiceUnavailable, "the goal loop is not available on this server")
		return
	}
	var req struct {
		Question string   `json:"question"`
		Kind     string   `json:"kind"`
		Options  []string `json:"options"`
		RunID    string   `json:"run_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	runID := strings.TrimSpace(r.Header.Get("X-Task-ID"))
	actorType, actorID := "member", requestUserID(r)
	if isMachineCredentialActor(r) {
		if runID == "" {
			writeError(w, http.StatusForbidden, "a run may only ask on its own behalf")
			return
		}
	} else if runID == "" {
		runID = strings.TrimSpace(req.RunID)
	}
	taskID, ok := parseUUIDOrBadRequest(w, runID, "run_id")
	if !ok {
		return
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil || task.IssueID != issue.ID {
		writeError(w, http.StatusBadRequest, "run_id is not a run of this issue")
		return
	}
	if isMachineCredentialActor(r) {
		actorType, actorID = "agent", uuidToString(task.AgentID)
	}
	goal, err := h.GoalLoop.AskQuestion(r.Context(), task, goalstate.Question{Prompt: req.Question, Kind: req.Kind, Options: req.Options})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditGoalAsked, "issue", issue.ID, map[string]any{"run_id": uuidToString(task.ID)}, nil)
	if actorType == "member" {
		// A member's question from outside a run is not a run's closing
		// question: the judge would not see the run settle. Raise it now.
		st := service.GoalStateOf(goal)
		if st.Question != nil {
			h.GoalLoop.RaiseQuestionNow(r.Context(), issue, task, goal)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"goal": service.GoalStateOf(goal)})
}

// goalStateForTask is what a claimed run is handed: nil when the issue has
// no goal row yet.
func (h *Handler) goalStateForTask(r *http.Request, issueID pgtype.UUID) *goalstate.State {
	if h.GoalLoop == nil || !issueID.Valid {
		return nil
	}
	issue, err := h.Queries.GetIssue(r.Context(), issueID)
	if err != nil {
		return nil
	}
	st, err := h.GoalLoop.State(r.Context(), issue)
	if err != nil {
		return nil
	}
	return st
}
