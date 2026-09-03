package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Decision Cards (K01): a typed question an agent asks a human on an issue.
// The card is data: options, a recommendation, an urgency. The answer is
// recorded once and handed to a new run through the handoff note.

const (
	decisionMaxOptions     = 8
	decisionMaxQuestionLen = 4 << 10
	decisionMaxOptionLen   = 512
	decisionMaxAnswerLen   = 4 << 10
)

type DecisionOption struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Impact string `json:"impact,omitempty"`
}

type DecisionAnswer struct {
	OptionID     string `json:"option_id,omitempty"`
	ModifiedText string `json:"modified_text,omitempty"`
}

type IssueDecisionResponse struct {
	ID                  string           `json:"id"`
	IssueID             string           `json:"issue_id"`
	TaskID              string           `json:"task_id,omitempty"`
	AskedByType         string           `json:"asked_by_type"`
	AskedByID           string           `json:"asked_by_id"`
	Question            string           `json:"question"`
	Options             []DecisionOption `json:"options"`
	RecommendedOptionID string           `json:"recommended_option_id,omitempty"`
	Urgency             string           `json:"urgency"`
	Response            *DecisionAnswer  `json:"response"`
	RespondedByType     string           `json:"responded_by_type,omitempty"`
	RespondedByID       string           `json:"responded_by_id,omitempty"`
	RespondedAt         *string          `json:"responded_at"`
	ResumeTaskID        string           `json:"resume_task_id,omitempty"`
	CreatedAt           string           `json:"created_at"`
}

func issueDecisionToResponse(d db.IssueDecision) IssueDecisionResponse {
	var options []DecisionOption
	_ = json.Unmarshal(d.Options, &options)
	if options == nil {
		options = []DecisionOption{}
	}
	var answer *DecisionAnswer
	if len(d.Response) > 0 {
		var a DecisionAnswer
		if json.Unmarshal(d.Response, &a) == nil {
			answer = &a
		}
	}
	return IssueDecisionResponse{
		ID:                  uuidToString(d.ID),
		IssueID:             uuidToString(d.IssueID),
		TaskID:              uuidToString(d.TaskID),
		AskedByType:         d.AskedByType,
		AskedByID:           uuidToString(d.AskedByID),
		Question:            d.Question,
		Options:             options,
		RecommendedOptionID: d.RecommendedOptionID.String,
		Urgency:             d.Urgency,
		Response:            answer,
		RespondedByType:     d.RespondedByType.String,
		RespondedByID:       uuidToString(d.RespondedByID),
		RespondedAt:         timestampToPtr(d.RespondedAt),
		ResumeTaskID:        uuidToString(d.ResumeTaskID),
		CreatedAt:           timestampToString(d.CreatedAt),
	}
}

func (h *Handler) ListIssueDecisions(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListIssueDecisions(r.Context(), db.ListIssueDecisionsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		slog.Warn("list issue decisions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load decisions")
		return
	}
	out := make([]IssueDecisionResponse, 0, len(rows))
	for _, d := range rows {
		out = append(out, issueDecisionToResponse(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"decisions": out})
}

// AskIssueDecision files a card. From a run (task token) the run id comes
// from X-Task-ID; a member can file one by hand too.
func (h *Handler) AskIssueDecision(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Question            string           `json:"question"`
		Options             []DecisionOption `json:"options"`
		RecommendedOptionID string           `json:"recommended_option_id"`
		Urgency             string           `json:"urgency"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" || len(req.Question) > decisionMaxQuestionLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("question is required (at most %d bytes)", decisionMaxQuestionLen))
		return
	}
	if len(req.Options) < 2 || len(req.Options) > decisionMaxOptions {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("between 2 and %d options are required", decisionMaxOptions))
		return
	}
	seen := map[string]bool{}
	for i := range req.Options {
		o := &req.Options[i]
		o.ID = strings.TrimSpace(o.ID)
		o.Label = strings.TrimSpace(o.Label)
		o.Impact = strings.TrimSpace(o.Impact)
		if o.ID == "" || o.Label == "" || len(o.Label) > decisionMaxOptionLen || len(o.Impact) > decisionMaxOptionLen {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("options[%d] needs an id and a label (at most %d bytes each)", i, decisionMaxOptionLen))
			return
		}
		if seen[o.ID] {
			writeError(w, http.StatusBadRequest, "option ids must be unique")
			return
		}
		seen[o.ID] = true
	}
	if req.RecommendedOptionID != "" && !seen[req.RecommendedOptionID] {
		writeError(w, http.StatusBadRequest, "recommended_option_id must be one of the options")
		return
	}
	switch req.Urgency {
	case "":
		req.Urgency = "normal"
	case "low", "normal", "high":
	default:
		writeError(w, http.StatusBadRequest, "urgency must be low, normal or high")
		return
	}
	options, _ := json.Marshal(req.Options)

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	var taskID pgtype.UUID
	if task, ok := h.taskFromRequestHeader(r); ok && task.IssueID == issue.ID {
		taskID = task.ID
	}

	decision, err := h.Queries.CreateIssueDecision(r.Context(), db.CreateIssueDecisionParams{
		WorkspaceID:         issue.WorkspaceID,
		IssueID:             issue.ID,
		TaskID:              taskID,
		AskedByType:         actorType,
		AskedByID:           parseUUID(actorID),
		Question:            req.Question,
		Options:             options,
		RecommendedOptionID: pgtype.Text{String: req.RecommendedOptionID, Valid: req.RecommendedOptionID != ""},
		Urgency:             req.Urgency,
	})
	if err != nil {
		slog.Warn("create issue decision failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to file decision")
		return
	}
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	writeJSON(w, http.StatusCreated, map[string]any{"decision": issueDecisionToResponse(decision)})
}

// RespondIssueDecision records the human answer once and, when the issue is
// agent-assigned, queues a run carrying it in the handoff note.
func (h *Handler) RespondIssueDecision(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	decisionID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "decisionId"), "decision id")
	if !ok {
		return
	}
	var req DecisionAnswer
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.OptionID = strings.TrimSpace(req.OptionID)
	req.ModifiedText = strings.TrimSpace(req.ModifiedText)
	if (req.OptionID == "") == (req.ModifiedText == "") {
		writeErrorCode(w, http.StatusBadRequest, "invalid_decision", "answer with exactly one of option_id or modified_text")
		return
	}
	if len(req.ModifiedText) > decisionMaxAnswerLen {
		writeErrorCode(w, http.StatusBadRequest, "invalid_decision", fmt.Sprintf("modified_text exceeds %d bytes", decisionMaxAnswerLen))
		return
	}

	ctx := r.Context()
	decision, err := h.Queries.GetIssueDecision(ctx, db.GetIssueDecisionParams{ID: decisionID, IssueID: issue.ID})
	if err != nil {
		writeError(w, http.StatusNotFound, "decision not found")
		return
	}
	var options []DecisionOption
	_ = json.Unmarshal(decision.Options, &options)
	chosen := ""
	if req.OptionID != "" {
		for _, o := range options {
			if o.ID == req.OptionID {
				chosen = o.Label
			}
		}
		if chosen == "" {
			writeErrorCode(w, http.StatusBadRequest, "invalid_decision", "option_id is not one of the card's options")
			return
		}
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	answer, _ := json.Marshal(req)
	updated, err := h.Queries.RespondIssueDecision(ctx, db.RespondIssueDecisionParams{
		ID:              decisionID,
		Response:        answer,
		RespondedByType: pgtype.Text{String: actorType, Valid: true},
		RespondedByID:   parseUUID(actorID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusConflict, "already_decided", "this decision was already answered")
		return
	}
	if err != nil {
		slog.Warn("respond issue decision failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to record decision")
		return
	}

	// Hand the answer to the agent: one new run with the decision up front.
	// A human-assigned issue just keeps the recorded answer.
	if issue.AssigneeType.String == "agent" && issue.AssigneeID.Valid {
		note := decisionHandoffNote(decision.Question, chosen, req)
		if task, err := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, issue, note, parseUUID(userID)); err != nil {
			slog.Warn("decision: enqueue resume run failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		} else if err := h.Queries.SetIssueDecisionResumeTask(ctx, db.SetIssueDecisionResumeTaskParams{ID: decisionID, ResumeTaskID: task.ID}); err == nil {
			updated.ResumeTaskID = task.ID
		}
	}
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	writeJSON(w, http.StatusOK, map[string]any{"decision": issueDecisionToResponse(updated)})
}

// decisionHandoffNote is what the resumed run reads first.
func decisionHandoffNote(question, chosenLabel string, answer DecisionAnswer) string {
	if answer.ModifiedText != "" {
		return fmt.Sprintf("Decision on «%s»: the human chose a different path — %s", question, answer.ModifiedText)
	}
	return fmt.Sprintf("Decision on «%s»: the human chose option %q (%s). Proceed accordingly.", question, answer.OptionID, chosenLabel)
}
