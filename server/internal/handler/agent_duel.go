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
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Agent duel (K39). Two agents run the same issue independently and at the
// same time, each in a fresh session, neither touching the issue itself.
// Once both runs finished, an arbiter (the internal LLM) scores quality and
// trajectory on top of the measured cost and duration; a human then confirms
// the winner. A candidate that failed for good makes the duel inconclusive.
// The verdict is data for K43 (competence history); it changes nothing on
// the issue by itself.

const (
	AuditDuel             = "duel"
	ErrCodeDuelIdentical  = "duel_executors_identical"
	ErrCodeDuelActive     = "duel_already_active"
	ErrCodeDuelRunPending = "duel_run_pending"
	duelTranscriptTail    = 4000
	duelBrief             = "Duel run: another agent works on this same issue independently, at the same time. Work on your own branch; do not merge, close, reassign or change the status of the issue. End with a clear summary of what you delivered and how you verified it — a human compares both results and picks one."
	duelArbiterPrompt     = `You are the arbiter of a duel between two AI coding agents that worked independently on the same task. Compare candidate A and candidate B on the quality of their result, the soundness of their trajectory (few wasted steps, verification done, clear final summary) and their efficiency (cost, duration, tool calls). Reply with one JSON object only: {"winner":"a"|"b"|"tie","quality_a":0-100,"quality_b":0-100,"summary_a":"one sentence on A's trajectory","summary_b":"one sentence on B's trajectory","reasoning":"two or three sentences justifying the winner"}.`
)

type AgentDuelRequest struct {
	AgentAID string `json:"agent_a_id"`
	AgentBID string `json:"agent_b_id"`
}

type duelMetrics struct {
	CostUsdTicks    int64 `json:"cost_usd_ticks"`
	DurationSeconds int64 `json:"duration_seconds"`
	ToolCalls       int   `json:"tool_calls"`
	Messages        int   `json:"messages"`
}

// duelVerdict is the JSONB stored once both runs finished: measured metrics
// always, the arbiter's scores when it answered.
type duelVerdict struct {
	Winner    string      `json:"winner,omitempty"`
	QualityA  *int        `json:"quality_a,omitempty"`
	QualityB  *int        `json:"quality_b,omitempty"`
	SummaryA  string      `json:"summary_a,omitempty"`
	SummaryB  string      `json:"summary_b,omitempty"`
	Reasoning string      `json:"reasoning,omitempty"`
	MetricsA  duelMetrics `json:"metrics_a"`
	MetricsB  duelMetrics `json:"metrics_b"`
}

type DuelSideResponse struct {
	AgentID         string  `json:"agent_id"`
	TaskID          string  `json:"task_id"`
	TaskStatus      string  `json:"task_status"`
	Outcome         *string `json:"outcome"`
	CostUsdTicks    int64   `json:"cost_usd_ticks"`
	DurationSeconds int64   `json:"duration_seconds"`
	ToolCalls       int     `json:"tool_calls"`
	QualityScore    *int    `json:"quality_score"`
	Summary         string  `json:"summary"`
}

type AgentDuelResponse struct {
	ID            string           `json:"id"`
	IssueID       string           `json:"issue_id"`
	Status        string           `json:"status"`
	A             DuelSideResponse `json:"a"`
	B             DuelSideResponse `json:"b"`
	ArbiterWinner *string          `json:"arbiter_winner"`
	Reasoning     string           `json:"reasoning"`
	ArbiterError  *string          `json:"arbiter_error"`
	Winner        *string          `json:"winner"`
	ConfirmedBy   *string          `json:"confirmed_by"`
	ConfirmedAt   *string          `json:"confirmed_at"`
	CreatedAt     string           `json:"created_at"`
	SettledAt     *string          `json:"settled_at"`
}

func (h *Handler) duelToResponse(ctx context.Context, d db.AgentDuel) AgentDuelResponse {
	var v duelVerdict
	_ = json.Unmarshal(d.Verdict, &v)
	side := func(agentID, taskID, finalTaskID pgtype.UUID, outcome pgtype.Text, quality *int, summary string, stored duelMetrics) DuelSideResponse {
		if finalTaskID.Valid {
			taskID = finalTaskID
		}
		out := DuelSideResponse{AgentID: uuidToString(agentID), TaskID: uuidToString(taskID), Outcome: textToPtr(outcome), QualityScore: quality, Summary: summary, ToolCalls: stored.ToolCalls}
		if task, err := h.Queries.GetAgentTask(ctx, taskID); err == nil {
			out.TaskStatus = task.Status
			out.DurationSeconds = duelDuration(task)
		}
		out.CostUsdTicks, _ = h.Queries.SumTaskCostTicks(ctx, taskID)
		return out
	}
	out := AgentDuelResponse{
		ID: uuidToString(d.ID), IssueID: uuidToString(d.IssueID), Status: d.Status,
		A:         side(d.AgentAID, d.TaskAID, d.FinalTaskAID, d.OutcomeA, v.QualityA, v.SummaryA, v.MetricsA),
		B:         side(d.AgentBID, d.TaskBID, d.FinalTaskBID, d.OutcomeB, v.QualityB, v.SummaryB, v.MetricsB),
		Reasoning: v.Reasoning, ArbiterError: textToPtr(d.ArbiterError), Winner: textToPtr(d.Winner),
		ConfirmedBy: uuidToPtr(d.ConfirmedBy), ConfirmedAt: timestampToPtr(d.ConfirmedAt), CreatedAt: timestampToString(d.CreatedAt), SettledAt: timestampToPtr(d.SettledAt),
	}
	if v.Winner != "" {
		out.ArbiterWinner = &v.Winner
	}
	return out
}

func duelDuration(task db.AgentTaskQueue) int64 {
	if !task.StartedAt.Valid {
		return 0
	}
	end := time.Now()
	if task.CompletedAt.Valid {
		end = task.CompletedAt.Time
	}
	return int64(end.Sub(task.StartedAt.Time).Seconds())
}

// StartAgentDuel: POST /api/issues/{id}/duel.
func (h *Handler) StartAgentDuel(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req AgentDuelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.AgentAID == req.AgentBID {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeDuelIdentical, "a duel needs two different agents")
		return
	}
	agents := make([]db.Agent, 0, 2)
	for _, raw := range []string{req.AgentAID, req.AgentBID} {
		id, ok := parseUUIDOrBadRequest(w, raw, "agent id")
		if !ok {
			return
		}
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: issue.WorkspaceID})
		if err != nil || agent.ArchivedAt.Valid {
			writeError(w, http.StatusUnprocessableEntity, "agent not found in this workspace")
			return
		}
		agents = append(agents, agent)
	}
	if active, err := h.Queries.HasRunningAgentDuelForIssue(r.Context(), issue.ID); err == nil && active {
		writeErrorCode(w, http.StatusConflict, ErrCodeDuelActive, "a duel is already running on this issue")
		return
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	userID := parseUUID(requestUserID(r))
	tasks := make([]db.AgentTaskQueue, 0, 2)
	for i, agent := range agents {
		task, err := h.TaskService.EnqueueDuelRun(r.Context(), issue, agent.ID, fmt.Sprintf("%s\nYou are candidate %c.", duelBrief, 'A'+i), userID)
		if err != nil {
			for _, queued := range tasks { // do not leave the first candidate running alone
				if _, cerr := h.TaskService.CancelTask(r.Context(), queued.ID); cerr != nil {
					slog.Warn("duel: cancel first candidate failed", "task_id", uuidToString(queued.ID), "error", cerr)
				}
			}
			if errors.Is(err, service.ErrDuplicatePendingTask) {
				writeErrorCode(w, http.StatusConflict, ErrCodeDuelRunPending, agent.Name+" already has a pending run on this issue")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to queue the candidate run: "+err.Error())
			return
		}
		tasks = append(tasks, task)
	}
	duel, err := h.Queries.CreateAgentDuel(r.Context(), db.CreateAgentDuelParams{ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AgentAID: agents[0].ID, AgentBID: agents[1].ID, TaskAID: tasks[0].ID, TaskBID: tasks[1].ID, StartedBy: userID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the duel")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditDuel, "issue", issue.ID, map[string]any{"duel_id": uuidToString(duel.ID), "agent_a_id": uuidToString(agents[0].ID), "agent_b_id": uuidToString(agents[1].ID), "started": true}, nil)
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	writeJSON(w, http.StatusCreated, map[string]any{"duel": h.duelToResponse(r.Context(), duel)})
}

// GetIssueAgentDuel: GET /api/issues/{id}/duel — the latest duel.
func (h *Handler) GetIssueAgentDuel(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	d, err := h.Queries.GetLatestAgentDuelForIssue(r.Context(), issue.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"duel": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"duel": h.duelToResponse(r.Context(), d)})
}

// GetAgentDuel: GET /api/duels/{id}.
func (h *Handler) GetAgentDuel(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDuelForUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"duel": h.duelToResponse(r.Context(), d)})
}

// ConfirmAgentDuel: POST /api/duels/{id}/confirm {winner: a|b|tie}. The
// human's word is final; it may differ from the arbiter's.
func (h *Handler) ConfirmAgentDuel(w http.ResponseWriter, r *http.Request) {
	d, ok := h.loadDuelForUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Winner string `json:"winner"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || (req.Winner != "a" && req.Winner != "b" && req.Winner != "tie") {
		writeError(w, http.StatusBadRequest, "winner must be a, b or tie")
		return
	}
	userID := requestUserID(r)
	confirmed, err := h.Queries.ConfirmAgentDuel(r.Context(), db.ConfirmAgentDuelParams{ID: d.ID, Winner: pgtype.Text{String: req.Winner, Valid: true}, ConfirmedBy: parseUUID(userID)})
	if err != nil {
		writeError(w, http.StatusConflict, "this duel is not awaiting a verdict")
		return
	}
	var v duelVerdict
	_ = json.Unmarshal(confirmed.Verdict, &v)
	actorType, actorID := h.resolveActor(r, userID, uuidToString(d.WorkspaceID))
	h.audit(r.Context(), d.WorkspaceID, actorType, actorID, AuditDuel, "issue", d.IssueID, map[string]any{"duel_id": uuidToString(d.ID), "winner": req.Winner, "arbiter_winner": v.Winner, "agent_a_id": uuidToString(d.AgentAID), "agent_b_id": uuidToString(d.AgentBID), "confirmed": true}, nil)
	if issue, err := h.Queries.GetIssue(r.Context(), d.IssueID); err == nil {
		// Learned competency (K43): a confirmed duel is a strong, separately weighted signal.
		h.recordDuelCompetency(r.Context(), issue, confirmed)
		h.publishIssueAuxChanged(r, issue, actorType, actorID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"duel": h.duelToResponse(r.Context(), confirmed)})
}

func (h *Handler) loadDuelForUser(w http.ResponseWriter, r *http.Request) (db.AgentDuel, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "duel id")
	if !ok {
		return db.AgentDuel{}, false
	}
	d, err := h.Queries.GetAgentDuel(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "duel not found")
		return db.AgentDuel{}, false
	}
	if _, ok := h.loadIssueForUser(w, r, uuidToString(d.IssueID)); !ok {
		return db.AgentDuel{}, false
	}
	return d, true
}

// updateDuelBarrier (K39) is called when a run reaches a terminal status:
// a completed candidate settles its side, a failed one only when no retry
// is coming; the second settlement ends the duel (arbiter or inconclusive).
func (h *Handler) updateDuelBarrier(ctx context.Context, task db.AgentTaskQueue) {
	root := task.ID
	if task.RetryOfTaskID.Valid {
		root = task.RetryOfTaskID
	} else if task.ParentTaskID.Valid {
		root = task.ParentTaskID
	}
	d, err := h.Queries.GetRunningAgentDuelForTask(ctx, db.GetRunningAgentDuelForTaskParams{TaskID: task.ID, RootTaskID: root})
	if err != nil {
		return
	}
	outcome := ""
	switch task.Status {
	case "completed":
		outcome = "completed"
	case "failed", "cancelled":
		if more, err := h.Queries.HasRunnableSuccessorForTask(ctx, task.ID); err == nil && more {
			return // a retry is coming; not final yet
		}
		outcome = "failed"
	default:
		return
	}
	side := "a"
	if task.ID == d.TaskBID || root == d.TaskBID {
		side = "b"
	}
	if (side == "a" && d.OutcomeA.Valid) || (side == "b" && d.OutcomeB.Valid) {
		return
	}
	d, err = h.Queries.SettleAgentDuelSide(ctx, db.SettleAgentDuelSideParams{ID: d.ID, Side: side, Outcome: outcome, FinalTaskID: task.ID})
	if err != nil {
		return
	}
	if !d.OutcomeA.Valid || !d.OutcomeB.Valid {
		h.publishDuelProgress(ctx, d)
		return
	}
	verdict := duelVerdict{}
	var transcriptA, transcriptB string
	verdict.MetricsA, transcriptA = h.duelRunFacts(ctx, d.FinalTaskAID)
	verdict.MetricsB, transcriptB = h.duelRunFacts(ctx, d.FinalTaskBID)
	status, arbiterErr := "verdict_ready", pgtype.Text{}
	if d.OutcomeA.String == "failed" || d.OutcomeB.String == "failed" {
		status = "inconclusive"
	} else if h.LLM == nil || !h.LLM.Enabled() {
		arbiterErr = pgtype.Text{String: "llm_disabled", Valid: true}
	} else if err := h.arbitrateDuel(ctx, d, &verdict, transcriptA, transcriptB); err != nil {
		slog.Warn("duel: arbiter failed", "duel_id", uuidToString(d.ID), "error", err)
		arbiterErr = pgtype.Text{String: err.Error(), Valid: true}
	}
	raw, _ := json.Marshal(verdict)
	settled, err := h.Queries.SetAgentDuelVerdict(ctx, db.SetAgentDuelVerdictParams{ID: d.ID, Status: status, Verdict: raw, ArbiterError: arbiterErr})
	if err != nil {
		slog.Warn("duel: settle failed", "duel_id", uuidToString(d.ID), "error", err)
		return
	}
	h.audit(ctx, d.WorkspaceID, "system", "", AuditDuel, "issue", d.IssueID, map[string]any{"duel_id": uuidToString(d.ID), "status": status, "arbiter_winner": verdict.Winner, "arbiter_error": arbiterErr.String}, nil)
	h.publishDuelProgress(ctx, settled)
}

// duelRunFacts measures one candidate run and returns the tail of its
// transcript for the arbiter.
func (h *Handler) duelRunFacts(ctx context.Context, taskID pgtype.UUID) (duelMetrics, string) {
	m := duelMetrics{}
	if task, err := h.Queries.GetAgentTask(ctx, taskID); err == nil {
		m.DurationSeconds = duelDuration(task)
	}
	m.CostUsdTicks, _ = h.Queries.SumTaskCostTicks(ctx, taskID)
	msgs, _ := h.Queries.ListTaskMessages(ctx, taskID)
	m.Messages = len(msgs)
	for _, msg := range msgs {
		if msg.Type == "tool_use" || msg.Type == "tool-use" {
			m.ToolCalls++
		}
	}
	transcript, _ := decisionTranscript(msgs)
	if len(transcript) > duelTranscriptTail {
		transcript = "…" + transcript[len(transcript)-duelTranscriptTail:]
	}
	return m, transcript
}

func (h *Handler) arbitrateDuel(ctx context.Context, d db.AgentDuel, v *duelVerdict, transcriptA, transcriptB string) error {
	issue, err := h.Queries.GetIssue(ctx, d.IssueID)
	if err != nil {
		return err
	}
	var input strings.Builder
	fmt.Fprintf(&input, "TASK: %s\n%s\n", issue.Title, issue.Description.String)
	for _, c := range []struct {
		label      string
		m          duelMetrics
		transcript string
	}{{"A", v.MetricsA, transcriptA}, {"B", v.MetricsB, transcriptB}} {
		fmt.Fprintf(&input, "\n=== CANDIDATE %s ===\ncost_usd_ticks=%d duration_seconds=%d tool_calls=%d messages=%d\n%s\n", c.label, c.m.CostUsdTicks, c.m.DurationSeconds, c.m.ToolCalls, c.m.Messages, c.transcript)
	}
	raw, err := h.LLM.GenerateJSON(ctx, "", duelArbiterPrompt, input.String(), 0.1, 1024)
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	var out struct {
		Winner    string `json:"winner"`
		QualityA  *int   `json:"quality_a"`
		QualityB  *int   `json:"quality_b"`
		SummaryA  string `json:"summary_a"`
		SummaryB  string `json:"summary_b"`
		Reasoning string `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil || (out.Winner != "a" && out.Winner != "b" && out.Winner != "tie") {
		return errors.New("malformed verdict")
	}
	v.Winner, v.QualityA, v.QualityB, v.SummaryA, v.SummaryB, v.Reasoning = out.Winner, out.QualityA, out.QualityB, out.SummaryA, out.SummaryB, out.Reasoning
	return nil
}

func (h *Handler) publishDuelProgress(ctx context.Context, d db.AgentDuel) {
	h.publish("duel:progress", uuidToString(d.WorkspaceID), "system", "", map[string]any{"issue_id": uuidToString(d.IssueID), "duel_id": uuidToString(d.ID), "status": d.Status})
}
