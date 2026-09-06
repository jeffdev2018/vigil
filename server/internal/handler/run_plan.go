package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Living run plan (F04). A run publishes the checklist it is working through so
// a reader can see where it is without opening the transcript.
//
// The plan is a task_message of type 'plan', not a table and not a column on
// agent_task_queue. Everything the feature needs already exists on that row:
// persistence, ordering by seq, the task:message realtime broadcast, presence
// in the transcript at the moment it was published, and purge with the run.
// "Replaced every turn" is therefore "the highest seq wins", not an UPDATE — so
// the transcript keeps every version the run believed on the way through.

const (
	// A plan is a checklist, not a work breakdown structure. Thirty items is
	// well past the point where a reader stops reading and the run stops
	// maintaining it honestly.
	runPlanMaxItems = 30
	// One line each. Longer than this is a description, and the run has the
	// issue body and its comments for that.
	runPlanMaxTextLen = 200
	// Plans are allocated seqs from a reserved band above anything the daemon
	// can produce, because the daemon's counter is in-process and starts at 1
	// for every run — see CreateTaskPlanMessage. A run would have to emit a
	// million messages to reach the band, and run limits (K03) cap turns and
	// tool calls long before that.
	//
	// ponytail: a fixed band, not a sequence. If a run ever legitimately
	// crosses it the allocator still holds (GREATEST keeps climbing); the
	// thing that would break first is nothing, because display order comes
	// from created_at, not from seq.
	runPlanSeqFloor = 1_000_000
)

// runPlanStatuses are the three states an item can be in. Closed on the write
// side (an unknown status is a typo, and a 400 says so) and open on the read
// side (an installed client must render a status a newer server accepted).
var runPlanStatuses = map[string]bool{
	"pending":     true,
	"in_progress": true,
	"done":        true,
}

// RunPlanItem is one checklist entry.
type RunPlanItem struct {
	Text   string `json:"text"`
	Status string `json:"status"`
}

// RunPlan is the run's current checklist plus the seq of the message carrying
// it, so a client can tell a newer plan from the one it already rendered.
type RunPlan struct {
	Items []RunPlanItem `json:"items"`
	Seq   int           `json:"seq"`
}

type setRunPlanRequest struct {
	Items []RunPlanItem `json:"items"`
}

// SetRunPlan publishes (and thereby replaces) the plan of the caller's own run.
//
// Authorization is the run's own task token, not workspace membership: the plan
// says what the AGENT believes it is doing, so a human — even the one who
// started the run — must not be able to write one. The route therefore sits in
// the member group WITHOUT RequireHumanActor, and this handler requires the
// server-set X-Actor-Source: task_token and an X-Task-ID naming this very task.
func (h *Handler) SetRunPlan(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")
	taskUUID, ok := parseUUIDOrBadRequest(w, taskID, "task_id")
	if !ok {
		return
	}

	// X-Actor-Source is server-set: the auth middleware strips any
	// client-supplied value and stamps "task_token" only for an `mat_`
	// credential, whose (agent_id, task_id) binding the agent process cannot
	// forge. Without it, a member JWT carrying a hand-written X-Task-ID would
	// reach the same code.
	if r.Header.Get("X-Actor-Source") != "task_token" {
		writeError(w, http.StatusForbidden, "only the run itself can publish its plan")
		return
	}
	callerTaskID, err := util.ParseUUID(strings.TrimSpace(r.Header.Get("X-Task-ID")))
	if err != nil || uuidToString(callerTaskID) != uuidToString(taskUUID) {
		writeError(w, http.StatusForbidden, "a run can only publish its own plan")
		return
	}

	task, err := h.Queries.GetAgentTask(r.Context(), taskUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	// Same workspace gate as the other task-scoped user routes: the token binds
	// the task, this binds the workspace the request selected.
	wsID := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if wsID == "" || wsID != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}

	// A finished run has nothing left to plan, and a late write would rewrite
	// the record of what it was doing when it stopped.
	if isTerminalTaskStatus(task.Status) {
		writeError(w, http.StatusConflict, "run has finished; its plan can no longer change")
		return
	}

	var req setRunPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	items, err := normalizeRunPlanItems(req.Items)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	inputJSON, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		slog.Error("run plan: failed to encode items", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist run plan")
		return
	}
	id, err := uuid.NewV7()
	if err != nil {
		slog.Error("run plan: failed to generate message id", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist run plan")
		return
	}

	msg, err := h.Queries.CreateTaskPlanMessage(r.Context(), db.CreateTaskPlanMessageParams{
		ID:       pgtype.UUID{Bytes: [16]byte(id), Valid: true},
		TaskID:   taskUUID,
		SeqFloor: runPlanSeqFloor,
		Content:  runPlanSummary(items),
		Input:    inputJSON,
	})
	if err != nil {
		slog.Error("run plan: failed to persist", "task_id", taskID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to persist run plan")
		return
	}

	h.touchTaskActivity(r, task.ID)
	// Same broadcast path as any other message, so the transcript, the chat
	// footer and the execution log all update without a refetch. Only runs the
	// UI streams (issue- and chat-backed) have subscribers, matching the reach
	// ReportTaskMessages publishes with.
	if task.IssueID.Valid || task.ChatSessionID.Valid {
		h.publishTask(protocol.EventTaskMessage, wsID, "agent", uuidToString(task.AgentID), taskID,
			taskMessageToPayload(msg, taskID, uuidToString(task.IssueID)))
	}

	writeJSON(w, http.StatusCreated, RunPlan{Items: items, Seq: int(msg.Seq)})
}

// normalizeRunPlanItems validates and cleans the submitted checklist. Returns
// the items to store, or the error to send back verbatim as a 400 — the caller
// is an agent, and a message naming the rule it broke is what lets it fix the
// call without a round trip through a human.
func normalizeRunPlanItems(items []RunPlanItem) ([]RunPlanItem, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("items is required and must hold at least one entry")
	}
	if len(items) > runPlanMaxItems {
		return nil, fmt.Errorf("a plan holds at most %d items, got %d", runPlanMaxItems, len(items))
	}

	out := make([]RunPlanItem, 0, len(items))
	inProgress := 0
	for i, item := range items {
		// Sanitize before measuring: the length limit is on what gets stored,
		// and the sanitizer is what decides that. These strings land in a JSONB
		// column, so a stray NUL anywhere in the batch fails the insert (GH
		// #7098) — the same hazard the message batch carries.
		text := strings.TrimSpace(util.SanitizeTextForPostgres(item.Text))
		if text == "" {
			return nil, fmt.Errorf("item %d has no text", i+1)
		}
		if len([]rune(text)) > runPlanMaxTextLen {
			return nil, fmt.Errorf("item %d is %d characters; the limit is %d",
				i+1, len([]rune(text)), runPlanMaxTextLen)
		}
		status := util.SanitizeTextForPostgres(item.Status)
		if !runPlanStatuses[status] {
			return nil, fmt.Errorf("item %d has status %q; expected pending, in_progress or done", i+1, item.Status)
		}
		if status == "in_progress" {
			inProgress++
		}
		out = append(out, RunPlanItem{Text: text, Status: status})
	}
	// One thing at a time is the whole point of the checklist: a plan with two
	// items in progress does not tell a reader where the run is.
	if inProgress > 1 {
		return nil, fmt.Errorf("%d items are in_progress; at most one may be", inProgress)
	}
	return out, nil
}

// runPlanSummary is the one-line body the transcript shows for the plan entry
// ("3/7 done"). The checklist itself lives in the message input; this is the
// collapsed row.
func runPlanSummary(items []RunPlanItem) string {
	done := 0
	for _, item := range items {
		if item.Status == "done" {
			done++
		}
	}
	return fmt.Sprintf("%d/%d done", done, len(items))
}

// runPlanFromMessage reads a stored plan row back into the wire shape. Returns
// nil when the row's input is not a plan document, so a hand-written or
// truncated row costs the run its plan block rather than the whole response.
func runPlanFromMessage(m db.TaskMessage) *RunPlan {
	if len(m.Input) == 0 {
		return nil
	}
	var stored struct {
		Items []RunPlanItem `json:"items"`
	}
	if err := json.Unmarshal(m.Input, &stored); err != nil || len(stored.Items) == 0 {
		return nil
	}
	return &RunPlan{Items: stored.Items, Seq: int(m.Seq)}
}

// hydrateTaskPlans attaches each run's current plan to the issue-facing task
// rows. One query for the whole list, then a map join — never one query per
// task, which would be an N+1 over a list the UI always renders in full.
//
// The plan is display metadata: a failure here leaves every row without one,
// which the UI already renders as "no plan block", rather than taking the
// execution log down with it.
func (h *Handler) hydrateTaskPlans(ctx context.Context, resp []AgentTaskResponse) {
	if len(resp) == 0 {
		return
	}
	ids := make([]pgtype.UUID, 0, len(resp))
	for _, task := range resp {
		if id, err := util.ParseUUID(task.ID); err == nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	rows, err := h.Queries.ListLatestTaskPlans(ctx, ids)
	if err != nil || len(rows) == 0 {
		return
	}
	byTask := make(map[string]*RunPlan, len(rows))
	for _, row := range rows {
		if plan := runPlanFromMessage(row); plan != nil {
			byTask[uuidToString(row.TaskID)] = plan
		}
	}
	for i := range resp {
		if plan, ok := byTask[resp[i].ID]; ok {
			resp[i].Plan = plan
		}
	}
}
