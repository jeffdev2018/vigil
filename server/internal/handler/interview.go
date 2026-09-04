package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Requirement Interview (K13): before coding on a vague issue, an agent asks
// one to three multiple-choice questions at once. They are Decision Cards
// (K01) sharing a group; the issue parks in the workspace's "Waiting for PM"
// status (a custom status in the blocked category, created on first use so
// the board shows it as its own chip) and returns to its previous status when
// the last answer lands, which is the only moment the run resumes.

const (
	interviewMaxQuestions = 3
	interviewStatusKey    = "waiting_for_pm"
	interviewStatusName   = "Waiting for PM"
	interviewStatusColor  = "#f59e0b"

	ErrCodeInterviewPending = "interview_pending"
)

// decisionInput is one card as asked; normalizeDecisionInput is the shared
// validation for a single card and each interview question.
type decisionInput struct {
	Question            string           `json:"question"`
	Options             []DecisionOption `json:"options"`
	RecommendedOptionID string           `json:"recommended_option_id"`
	Urgency             string           `json:"urgency"`
}

func normalizeDecisionInput(req *decisionInput) error {
	req.Question = strings.TrimSpace(req.Question)
	if req.Question == "" || len(req.Question) > decisionMaxQuestionLen {
		return fmt.Errorf("question is required (at most %d bytes)", decisionMaxQuestionLen)
	}
	if len(req.Options) < 2 || len(req.Options) > decisionMaxOptions {
		return fmt.Errorf("between 2 and %d options are required", decisionMaxOptions)
	}
	seen := map[string]bool{}
	for i := range req.Options {
		o := &req.Options[i]
		o.ID = strings.TrimSpace(o.ID)
		o.Label = strings.TrimSpace(o.Label)
		o.Impact = strings.TrimSpace(o.Impact)
		if o.ID == "" || o.Label == "" || len(o.Label) > decisionMaxOptionLen || len(o.Impact) > decisionMaxOptionLen {
			return fmt.Errorf("options[%d] needs an id and a label (at most %d bytes each)", i, decisionMaxOptionLen)
		}
		if seen[o.ID] {
			return errors.New("option ids must be unique")
		}
		seen[o.ID] = true
	}
	req.RecommendedOptionID = strings.TrimSpace(req.RecommendedOptionID)
	if req.RecommendedOptionID != "" && !seen[req.RecommendedOptionID] {
		return errors.New("recommended_option_id must be one of the options")
	}
	switch req.Urgency {
	case "":
		req.Urgency = "normal"
	case "low", "normal", "high":
	default:
		return errors.New("urgency must be low, normal or high")
	}
	return nil
}

// ensureInterviewStatus returns the workspace's Waiting for PM status key,
// creating the custom status on first use.
func (h *Handler) ensureInterviewStatus(ctx context.Context, wsID pgtype.UUID) (string, error) {
	entry, err := h.Queries.GetIssueStatusEntryByKey(ctx, db.GetIssueStatusEntryByKeyParams{WorkspaceID: wsID, Key: interviewStatusKey})
	if err == nil {
		if entry.ArchivedAt.Valid {
			return "", fmt.Errorf("the %q status is archived; restore it to run interviews", interviewStatusName)
		}
		return entry.Key, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	if err := issuestatus.Ensure(ctx, h.Queries, wsID); err != nil {
		return "", err
	}
	created, msg, err := h.createIssueStatusEntry(ctx, wsID, db.CreateIssueStatusEntryParams{
		WorkspaceID: wsID,
		Key:         interviewStatusKey,
		Name:        interviewStatusName,
		Description: "Waiting for a product answer before work continues.",
		Category:    issuestatus.Blocked,
		Color:       interviewStatusColor,
	})
	if err != nil {
		return "", err
	}
	if msg != "" {
		return "", errors.New(msg)
	}
	return created.Key, nil
}

// AskRequirementInterview — POST /api/issues/{id}/interview.
func (h *Handler) AskRequirementInterview(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Questions []decisionInput `json:"questions"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Questions) < 1 || len(req.Questions) > interviewMaxQuestions {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("between 1 and %d questions are required", interviewMaxQuestions))
		return
	}
	for i := range req.Questions {
		if err := normalizeDecisionInput(&req.Questions[i]); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("questions[%d]: %s", i, err))
			return
		}
	}
	ctx := r.Context()
	if n, err := h.Queries.CountPendingInterviewQuestions(ctx, issue.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check pending questions")
		return
	} else if n > 0 {
		writeErrorCode(w, http.StatusConflict, ErrCodeInterviewPending, "this issue already waits on an interview; answer it first")
		return
	}
	waitingKey, err := h.ensureInterviewStatus(ctx, issue.WorkspaceID)
	if err != nil {
		slog.Warn("interview: ensure status failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to prepare the Waiting for PM status")
		return
	}
	// Where the issue returns once answered. An issue already parked (an
	// earlier interview answered by hand) resumes as in progress.
	resumeStatus := issue.Status
	if resumeStatus == waitingKey {
		resumeStatus = issuestatus.InProgress
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	var taskID pgtype.UUID
	if task, ok := h.taskFromRequestHeader(r); ok && task.IssueID == issue.ID {
		taskID = task.ID
	}
	groupID := dbid.NewV7()
	created := make([]IssueDecisionResponse, 0, len(req.Questions))
	for i, q := range req.Questions {
		options, _ := json.Marshal(q.Options)
		decision, err := h.Queries.CreateIssueDecision(ctx, db.CreateIssueDecisionParams{
			WorkspaceID:           issue.WorkspaceID,
			IssueID:               issue.ID,
			TaskID:                taskID,
			AskedByType:           actorType,
			AskedByID:             parseUUID(actorID),
			Question:              q.Question,
			Options:               options,
			RecommendedOptionID:   pgtype.Text{String: q.RecommendedOptionID, Valid: q.RecommendedOptionID != ""},
			Urgency:               q.Urgency,
			InterviewGroupID:      groupID,
			InterviewPosition:     pgtype.Int4{Int32: int32(i + 1), Valid: true},
			InterviewResumeStatus: pgtype.Text{String: resumeStatus, Valid: true},
			SlaDeadlineAt:         h.decisionDeadline(ctx, issue.WorkspaceID),
		})
		if err != nil {
			slog.Warn("interview: create question failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
			writeError(w, http.StatusInternalServerError, "failed to file the interview")
			return
		}
		h.notifyDecisionRequested(ctx, issue, decision, actorType, actorID)
		created = append(created, issueDecisionToResponse(decision))
	}
	parked, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issue.ID, Status: waitingKey, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		slog.Warn("interview: park issue failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to park the issue")
		return
	}
	h.publishIssueMoved(r, parked, actorType, actorID)
	writeJSON(w, http.StatusCreated, map[string]any{"decisions": created, "status": waitingKey})
}

// publishIssueStatusChanged emits issue:updated with the fresh row so boards
// move the card at once.
func (h *Handler) publishIssueMoved(r *http.Request, issue db.Issue, actorType, actorID string) {
	resp := issueToResponse(issue, h.getIssuePrefix(r.Context(), issue.WorkspaceID))
	h.fillStatusCategory(r.Context(), issue.WorkspaceID, &resp)
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{"issue": resp})
}

// interviewAnswerNote is the handoff note of the resumed run: every question
// with its answer, in the order they were asked.
func interviewAnswerNote(group []db.IssueDecision) string {
	var b strings.Builder
	b.WriteString("Requirement interview answered:")
	for i, d := range group {
		var options []DecisionOption
		_ = json.Unmarshal(d.Options, &options)
		var answer DecisionAnswer
		_ = json.Unmarshal(d.Response, &answer)
		label := answer.ModifiedText
		if label == "" {
			label = answer.OptionID
			for _, o := range options {
				if o.ID == answer.OptionID {
					label = fmt.Sprintf("%s (%s)", o.Label, o.ID)
				}
			}
		}
		fmt.Fprintf(&b, "\n%d. %s — %s", i+1, d.Question, label)
	}
	b.WriteString("\nProceed with these answers; do not ask them again.")
	return b.String()
}

// finishInterviewIfComplete runs after an interview question is answered:
// while a sibling is pending nothing happens; once the last one lands the
// issue returns to its previous status and, on an agent-assigned issue, one
// run resumes with every answer in order. Returns the resume task id.
func (h *Handler) finishInterviewIfComplete(r *http.Request, issue db.Issue, decision db.IssueDecision, userID, actorType, actorID string) (pgtype.UUID, bool) {
	ctx := r.Context()
	pending, err := h.Queries.CountPendingInterviewQuestions(ctx, issue.ID)
	if err != nil || pending > 0 {
		return pgtype.UUID{}, false
	}
	group, err := h.Queries.ListInterviewGroup(ctx, db.ListInterviewGroupParams{InterviewGroupID: decision.InterviewGroupID, IssueID: issue.ID})
	if err != nil || len(group) == 0 {
		return pgtype.UUID{}, false
	}
	resumeStatus := decision.InterviewResumeStatus.String
	if resumeStatus == "" {
		resumeStatus = issuestatus.InProgress
	}
	restored := issue
	if issue.Status == interviewStatusKey {
		if fresh, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issue.ID, Status: resumeStatus, WorkspaceID: issue.WorkspaceID}); err != nil {
			slog.Warn("interview: restore status failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		} else {
			restored = fresh
			h.publishIssueMoved(r, fresh, actorType, actorID)
		}
	}
	if restored.AssigneeType.String != "agent" || !restored.AssigneeID.Valid {
		return pgtype.UUID{}, true
	}
	task, err := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, restored, interviewAnswerNote(group), parseUUID(userID))
	if err != nil {
		slog.Warn("interview: enqueue resume run failed", append(logger.RequestAttrs(r), "error", err, "issue_id", uuidToString(issue.ID))...)
		return pgtype.UUID{}, true
	}
	return task.ID, true
}
