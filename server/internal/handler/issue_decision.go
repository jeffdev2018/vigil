package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type IssueDecisionResponse struct {
	ID            string   `json:"id"`
	IssueID       string   `json:"issue_id"`
	AgentID       string   `json:"agent_id"`
	SourceTaskID  string   `json:"source_task_id"`
	RecipientID   string   `json:"recipient_id"`
	RequestedBy   string   `json:"requested_by"`
	RequesterType string   `json:"requester_type"`
	Question      string   `json:"question"`
	Context       string   `json:"context"`
	Options       []string `json:"options"`
	Status        string   `json:"status"`
	Answer        *string  `json:"answer"`
	AnsweredBy    *string  `json:"answered_by"`
	AnsweredAt    *string  `json:"answered_at"`
	ResumeTaskID  *string  `json:"resume_task_id"`
	CreatedAt     string   `json:"created_at"`
}

func decisionResponse(row db.IssueDecision) IssueDecisionResponse {
	resp := IssueDecisionResponse{ID: uuidToString(row.ID), IssueID: uuidToString(row.IssueID), AgentID: uuidToString(row.AgentID),
		SourceTaskID: uuidToString(row.SourceTaskID), RecipientID: uuidToString(row.RecipientID), RequestedBy: uuidToString(row.RequestedBy),
		RequesterType: row.RequesterType, Question: row.Question, Context: row.Context, Options: []string{}, Status: row.Status,
		Answer: textToPtr(row.Answer), AnsweredBy: uuidToPtr(row.AnsweredBy), ResumeTaskID: uuidToPtr(row.ResumeTaskID), CreatedAt: timestampToString(row.CreatedAt)}
	_ = json.Unmarshal(row.Options, &resp.Options) // Only validated string arrays enter this table.
	if row.AnsweredAt.Valid {
		value := timestampToString(row.AnsweredAt)
		resp.AnsweredAt = &value
	}
	return resp
}

// A deleted chat loses chat_session_id. Require positive issue provenance, never
// infer that a source is public from a nullable relationship alone.
func decisionIssueSource(source db.AgentTaskQueue, issueID pgtype.UUID) bool {
	if source.IssueID != issueID || source.ChatSessionID.Valid || source.TriggerEvidenceKind.String == "chat" {
		return false
	}
	var memory map[string]json.RawMessage
	if len(source.MemoryContext) > 0 {
		if json.Unmarshal(source.MemoryContext, &memory) != nil {
			return false
		}
		if marker, present := memory["is_chat"]; present {
			return strings.TrimSpace(string(marker)) == "false"
		}
	}
	switch source.TriggerEvidenceKind.String {
	case "comment", "issue_assignment", "autopilot_run", "rule_version", "rerun", "delegated_failure":
		return true
	default:
		return false
	}
}

type CreateIssueDecisionRequest struct {
	ID           string   `json:"id"`
	SourceTaskID string   `json:"source_task_id"`
	RecipientID  string   `json:"recipient_id"`
	Question     string   `json:"question"`
	Context      string   `json:"context"`
	Options      []string `json:"options"`
}

func (h *Handler) CreateIssueDecision(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Actor-Source") == "cloud_pat" {
		writeError(w, http.StatusForbidden, "use a human or issue run credential")
		return
	}
	issue, userID, ok := h.loadIssueForDelivery(w, r)
	if !ok {
		return
	}
	var req CreateIssueDecisionRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&req) != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	id, ok := parseUUIDOrBadRequest(w, req.ID, "id")
	if !ok {
		return
	}
	sourceID, ok := parseUUIDOrBadRequest(w, req.SourceTaskID, "source_task_id")
	if !ok {
		return
	}
	recipientID, ok := parseUUIDOrBadRequest(w, req.RecipientID, "recipient_id")
	if !ok {
		return
	}
	req.ID, req.SourceTaskID, req.RecipientID = uuidToString(id), uuidToString(sourceID), uuidToString(recipientID)
	req.Question, req.Context = strings.TrimSpace(req.Question), strings.TrimSpace(req.Context)
	if req.Question == "" || utf8.RuneCountInString(req.Question) > 1000 || utf8.RuneCountInString(req.Context) > 8000 || len(req.Options) > 8 {
		writeError(w, 400, "question, context or options exceed their limits")
		return
	}
	if req.Options == nil {
		req.Options = []string{}
	}
	seen := map[string]bool{}
	for i, option := range req.Options {
		option = strings.TrimSpace(option)
		if option == "" || utf8.RuneCountInString(option) > 500 || seen[option] {
			writeError(w, 400, "options must be distinct nonempty strings of at most 500 characters")
			return
		}
		seen[option], req.Options[i] = true, option
	}
	requesterType, requesterID := "member", parseUUID(userID)
	if r.Header.Get("X-Actor-Source") == "task_token" {
		if r.Header.Get("X-Task-ID") != req.SourceTaskID {
			writeError(w, 403, "a run may request decisions only for itself")
			return
		}
		requesterType = "agent"
		requesterID, ok = parseUUIDOrBadRequest(w, r.Header.Get("X-Agent-ID"), "agent credential")
		if !ok {
			return
		}
	} else if recipientID != requesterID {
		// Enforce this before the retry lookup too: its durable row may already
		// contain an answer. Human requesters must not read another recipient's
		// response by replaying a creation request.
		writeError(w, http.StatusForbidden, "humans may request decisions only for themselves")
		return
	}
	hash, err := deliveryHash(req)
	if err != nil {
		writeError(w, 500, "failed to encode decision")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start decision")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	// Match workspace teardown's lock ordering before creating a dependent row.
	if _, err := q.LockWorkspaceForChatSessionCreate(r.Context(), issue.WorkspaceID); err != nil {
		writeError(w, 404, "workspace not found")
		return
	}
	issue, err = q.LockIssueForDescriptionUpdate(r.Context(), db.LockIssueForDescriptionUpdateParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, 404, "issue not found")
		return
	}
	previous, err := q.GetIssueDecision(r.Context(), db.GetIssueDecisionParams{ID: id, WorkspaceID: issue.WorkspaceID})
	if err == nil {
		if previous.IssueID != issue.ID || previous.RequesterType != requesterType || previous.RequestedBy != requesterID || previous.InputHash != hash {
			writeError(w, 409, "decision identifier already used")
			return
		}
		writeJSON(w, 200, decisionResponse(previous))
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 500, "failed to read decision")
		return
	}
	source, err := q.GetAgentTask(r.Context(), sourceID)
	if err != nil || !decisionIssueSource(source, issue.ID) || (requesterType == "agent" && source.AgentID != requesterID) {
		writeError(w, 404, "issue run not found")
		return
	}
	agent, err := q.GetAgent(r.Context(), source.AgentID)
	if err != nil || agent.WorkspaceID != issue.WorkspaceID || (requesterType == "member" && !h.canAccessPrivateAgent(r.Context(), agent, "member", userID, uuidToString(issue.WorkspaceID))) {
		writeError(w, 404, "issue run not found")
		return
	}
	if _, err := q.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: recipientID, WorkspaceID: issue.WorkspaceID}); err != nil || !h.canAccessPrivateAgent(r.Context(), agent, "member", req.RecipientID, uuidToString(issue.WorkspaceID)) {
		writeError(w, 400, "recipient must be a workspace member who can view this agent")
		return
	}
	count, err := q.CountOpenIssueDecisions(r.Context(), db.CountOpenIssueDecisionsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, 500, "failed to read decisions")
		return
	}
	if count >= 20 {
		writeError(w, 409, "resolve existing decisions before requesting more")
		return
	}
	options, _ := json.Marshal(req.Options)
	row, err := q.CreateIssueDecision(r.Context(), db.CreateIssueDecisionParams{ID: id, WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AgentID: source.AgentID, SourceTaskID: sourceID,
		RecipientID: recipientID, RequestedBy: requesterID, RequesterType: requesterType, Question: req.Question, Context: req.Context, Options: options, InputHash: hash})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, 409, "decision identifier already used")
		} else {
			writeError(w, 500, "failed to save decision")
		}
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "failed to commit decision")
		return
	}
	writeJSON(w, 201, decisionResponse(row))
}

func (h *Handler) ListIssueDecisions(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, 403, "decision inbox requires a human")
		return
	}
	member, ok := h.requireWorkspaceMember(w, r, h.resolveWorkspaceID(r), "workspace not found")
	if !ok {
		return
	}
	params := db.ListIssueDecisionsForRecipientParams{WorkspaceID: member.WorkspaceID, RecipientID: member.UserID, History: r.URL.Query().Get("history") == "true"}
	if cursor := r.URL.Query().Get("before_id"); cursor != "" {
		id, ok := parseUUIDOrBadRequest(w, cursor, "before_id")
		if !ok {
			return
		}
		row, err := h.Queries.GetIssueDecision(r.Context(), db.GetIssueDecisionParams{ID: id, WorkspaceID: member.WorkspaceID})
		if err != nil || row.RecipientID != member.UserID {
			writeError(w, 404, "decision cursor not found")
			return
		}
		params.BeforeID = id
	}
	rows, err := h.Queries.ListIssueDecisionsForRecipient(r.Context(), params)
	if err != nil {
		writeError(w, 500, "failed to read decisions")
		return
	}
	resp := struct {
		Decisions    []IssueDecisionResponse `json:"decisions"`
		NextBeforeID *string                 `json:"next_before_id"`
	}{Decisions: []IssueDecisionResponse{}}
	if len(rows) > 50 {
		rows = rows[:50]
		value := uuidToString(rows[49].ID)
		resp.NextBeforeID = &value
	}
	for _, row := range rows {
		resp.Decisions = append(resp.Decisions, decisionResponse(row))
	}
	writeJSON(w, 200, resp)
}

func (h *Handler) loadDecision(w http.ResponseWriter, r *http.Request, q *db.Queries, issue db.Issue, userID string, allowRequester bool) (db.IssueDecision, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "decisionID"), "decision_id")
	if !ok {
		return db.IssueDecision{}, false
	}
	row, err := q.GetIssueDecision(r.Context(), db.GetIssueDecisionParams{ID: id, WorkspaceID: issue.WorkspaceID})
	allowed := err == nil && row.RecipientID == parseUUID(userID)
	if isMachineCredentialActor(r) {
		allowed = allowRequester && r.Header.Get("X-Actor-Source") == "task_token" && r.Header.Get("X-Task-ID") == uuidToString(row.SourceTaskID) && r.Header.Get("X-Agent-ID") == uuidToString(row.AgentID)
	}
	if err != nil || row.IssueID != issue.ID || !allowed {
		writeError(w, 404, "decision not found")
		return db.IssueDecision{}, false
	}
	return row, true
}

func (h *Handler) GetIssueDecision(w http.ResponseWriter, r *http.Request) {
	issue, userID, ok := h.loadIssueForDelivery(w, r)
	if !ok {
		return
	}
	row, ok := h.loadDecision(w, r, h.Queries, issue, userID, true)
	if !ok {
		return
	}
	writeJSON(w, 200, decisionResponse(row))
}

// Answer and resume are separate commits: unavailable runtimes must not lose a
// human answer. The issue lock serializes answers, cancellations and resumes.
func (h *Handler) AnswerIssueDecision(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, 403, "only the human recipient can answer")
		return
	}
	issue, userID, ok := h.loadIssueForDelivery(w, r)
	if !ok {
		return
	}
	var req struct {
		Status string `json:"status"`
		Answer string `json:"answer"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&req) != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	req.Answer = strings.TrimSpace(req.Answer)
	if (req.Status != "answered" && req.Status != "cancelled") || (req.Status == "answered" && (req.Answer == "" || utf8.RuneCountInString(req.Answer) > 4000)) || (req.Status == "cancelled" && req.Answer != "") {
		writeError(w, 400, "provide an answer or cancel without an answer")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start answer")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	if _, err := q.LockIssueForDescriptionUpdate(r.Context(), db.LockIssueForDescriptionUpdateParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID}); err != nil {
		writeError(w, 404, "issue not found")
		return
	}
	row, ok := h.loadDecision(w, r, q, issue, userID, false)
	if !ok {
		return
	}
	if row.Status != "open" {
		if row.Status != req.Status || row.Answer.String != req.Answer || row.AnsweredBy != parseUUID(userID) {
			writeError(w, 409, "this decision already has a final response")
			return
		}
		writeJSON(w, 200, decisionResponse(row))
		return
	}
	row, err = q.AnswerIssueDecision(r.Context(), db.AnswerIssueDecisionParams{ID: row.ID, WorkspaceID: issue.WorkspaceID, Status: req.Status, Answer: pgtype.Text{String: req.Answer, Valid: req.Status == "answered"}, AnsweredBy: parseUUID(userID)})
	if err != nil {
		writeError(w, 500, "failed to save answer")
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "failed to commit answer")
		return
	}
	writeJSON(w, 200, decisionResponse(row))
}

func (h *Handler) ResumeIssueDecision(w http.ResponseWriter, r *http.Request) {
	if isMachineCredentialActor(r) {
		writeError(w, 403, "only the human recipient can resume")
		return
	}
	issue, userID, ok := h.loadIssueForDelivery(w, r)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to start resume")
		return
	}
	defer tx.Rollback(r.Context())
	q := h.Queries.WithTx(tx)
	issue, err = q.LockIssueForDescriptionUpdate(r.Context(), db.LockIssueForDescriptionUpdateParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		writeError(w, 404, "issue not found")
		return
	}
	row, ok := h.loadDecision(w, r, q, issue, userID, false)
	if !ok {
		return
	}
	if row.Status != "answered" {
		writeError(w, 409, "answer the decision before resuming")
		return
	}
	if row.ResumeTaskID.Valid {
		writeJSON(w, 200, decisionResponse(row))
		return
	}
	source, err := q.GetAgentTask(r.Context(), row.SourceTaskID)
	if err != nil || !decisionIssueSource(source, issue.ID) || source.AgentID != row.AgentID || (source.Status != "completed" && source.Status != "failed" && source.Status != "cancelled") {
		writeError(w, 409, "source run must be available and finished; your answer is saved")
		return
	}
	agent, err := q.GetAgent(r.Context(), row.AgentID)
	if err != nil || agent.WorkspaceID != issue.WorkspaceID || !h.canInvokeAgent(r.Context(), agent, "member", userID, userID, uuidToString(issue.WorkspaceID)) {
		writeError(w, 403, "not allowed to invoke this agent; your answer is saved")
		return
	}
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		writeError(w, 409, "agent needs an available runtime; your answer is saved")
		return
	}
	active, err := q.HasActiveTaskForIssueAndAgent(r.Context(), db.HasActiveTaskForIssueAndAgentParams{IssueID: issue.ID, AgentID: row.AgentID})
	if err != nil {
		writeError(w, 500, "failed to check pending work; your answer is saved")
		return
	}
	if active {
		writeError(w, 409, "another run is pending; your answer is saved")
		return
	}
	handoff := fmt.Sprintf("Human response to decision %s, source run %s.\nContinue this issue using the recorded response below. This is not delivery acceptance or permission to bypass any tool approval.\n\nQuestion:\n%s\n\nContext:\n%s\n\nAnswer from member %s:\n%s", uuidToString(row.ID), uuidToString(source.ID), row.Question, row.Context, userID, row.Answer.String)
	task, err := h.TaskService.EnqueueIssueFollowupInTx(r.Context(), q, issue, source, parseUUID(userID), handoff)
	if err != nil {
		if errors.Is(err, service.ErrDuplicatePendingTask) {
			writeError(w, 409, "another run is pending; your answer is saved")
		} else {
			writeError(w, 500, "failed to queue follow-up; your answer is saved")
		}
		return
	}
	row, err = q.SetIssueDecisionResume(r.Context(), db.SetIssueDecisionResumeParams{ID: row.ID, WorkspaceID: issue.WorkspaceID, TaskID: task.ID})
	if err != nil {
		writeError(w, 500, "failed to link follow-up; your answer is saved")
		return
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, 500, "failed to commit follow-up; your answer is saved")
		return
	}
	h.TaskService.BroadcastTaskQueued(r.Context(), task)
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	writeJSON(w, 201, decisionResponse(row))
}
