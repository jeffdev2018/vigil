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
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Reverting a conversation to one of its turns (F09).
//
// The user asks for it on a finished run; the daemon that owns the repository
// is the only party that can do it, because the branch lives on their machine.
// So this is a queue, not a request/response: POST enqueues a row, the
// runtime's next heartbeat claims it, and the daemon posts the outcome back.
//
// The destructive half is deliberately split. The daemon moves refs and reports
// success or a named refusal; only then does the server delete the runs and
// messages of the turns that no longer exist. Deleting first would leave a user
// whose branch could not be moved with a conversation missing its history and
// nothing to point at.

const (
	worktreeRevertStatusPending = "pending"
	worktreeRevertStatusClaimed = "claimed"
	worktreeRevertStatusDone    = "done"
	worktreeRevertStatusFailed  = "failed"

	// worktreeRevertClaimTimeout is how long a claim may sit before the request
	// goes back to pending. The 409 check counts a claimed request as in
	// flight, so a daemon that died holding one would otherwise block the
	// conversation until someone edited the database. Generously longer than
	// the work itself (a handful of local git commands) so a slow machine is
	// never handed the same revert twice.
	worktreeRevertClaimTimeout = 5 * time.Minute
)

// taskRevertable answers whether a run can be reverted to.
//
// Two conditions, and both matter. A checkpoint is what the revert resets the
// branch to, so without one there is nothing to do. Terminal is what makes the
// checkpoint trustworthy: a running row's checkpoint is from an earlier attempt
// of the same id (a re-dispatch keeps it), and reverting to it while the run is
// still writing would race the Finalize that is about to move the branch again.
func taskRevertable(t db.AgentTaskQueue) bool {
	if !t.CheckpointSha.Valid || strings.TrimSpace(t.CheckpointSha.String) == "" {
		return false
	}
	switch t.Status {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

// recordTaskTurnCheckpoint pins what a worktree run delivered, after the
// terminal transition has already committed.
//
// Outside that transaction on purpose: the checkpoint is an affordance, and a
// run whose work is done must not be reported as failed because a revert button
// could not be recorded. A failure here costs the button and nothing else.
func (h *Handler) recordTaskTurnCheckpoint(ctx context.Context, task db.AgentTaskQueue, checkpointSHA string) {
	checkpointSHA = strings.TrimSpace(checkpointSHA)
	if checkpointSHA == "" {
		return
	}
	if _, err := h.Queries.RecordTaskTurnCheckpoint(ctx, db.RecordTaskTurnCheckpointParams{
		ID:            task.ID,
		CheckpointSha: pgtype.Text{String: checkpointSHA, Valid: true},
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("record task turn checkpoint failed; the run stays complete but cannot be reverted to",
			"task_id", uuidToString(task.ID), "error", err)
	}
}

// WorktreeRevertResponse is what the enqueue endpoint and the poll endpoint
// return.
type WorktreeRevertResponse struct {
	RequestID string `json:"request_id"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func worktreeRevertToResponse(row db.WorktreeRevertRequest) WorktreeRevertResponse {
	return WorktreeRevertResponse{
		RequestID: uuidToString(row.ID),
		Status:    row.Status,
		Error:     row.Error.String,
	}
}

// RequestIssueRunRevert enqueues a revert of an issue's conversation branch to
// the turn a given run delivered.
//
// POST /api/issues/{id}/runs/{taskId}/revert
func (h *Handler) RequestIssueRunRevert(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "taskId")
	if !ok {
		return
	}

	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	// The run must belong to the issue the caller proved access to. Without
	// this the taskId path segment would be an unauthenticated read of any run
	// in any workspace.
	if uuidToString(task.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusNotFound, "run not found")
		return
	}
	if !taskRevertable(task) {
		writeError(w, http.StatusBadRequest,
			"this run has no worktree checkpoint, so there is nothing to revert to")
		return
	}
	if !task.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "this run is not bound to a runtime")
		return
	}

	runtime, err := h.Queries.GetAgentRuntime(r.Context(), task.RuntimeID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the runtime that produced this run is no longer registered")
		return
	}
	// Fail closed on the capability: a daemon that cannot revert would claim
	// the request, ignore the field it does not know, and never report — the
	// user would watch a spinner that can only end in the stale-claim sweeper.
	if !runtimeHasCapability(runtime.Metadata, protocol.DaemonCapabilityWorktreeRevertV1) {
		writeError(w, http.StatusBadRequest,
			"the Multica app on that machine does not support reverting a run. Update it and try again")
		return
	}

	// A daemon that died holding a claim would block this conversation for
	// good, because the in-flight check below counts 'claimed'. Released here
	// rather than on a timer: this is the only moment the staleness matters,
	// and it costs one UPDATE on a table that is normally empty.
	if _, err := h.Queries.ReleaseStaleWorktreeRevertClaims(r.Context(),
		pgtype.Interval{Microseconds: worktreeRevertClaimTimeout.Microseconds(), Valid: true}); err != nil {
		slog.Warn("release stale worktree revert claims failed", "error", err)
	}

	if existing, err := h.Queries.GetPendingWorktreeRevertForConversation(r.Context(),
		db.GetPendingWorktreeRevertForConversationParams{IssueID: issue.ID}); err == nil {
		writeJSON(w, http.StatusConflict, worktreeRevertToResponse(existing))
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not check for a revert already in progress")
		return
	}

	row, err := h.Queries.CreateWorktreeRevertRequest(r.Context(), db.CreateWorktreeRevertRequestParams{
		ID:           pgtype.UUID{Bytes: uuid.New(), Valid: true},
		WorkspaceID:  issue.WorkspaceID,
		RuntimeID:    task.RuntimeID,
		TargetTaskID: task.ID,
		IssueID:      issue.ID,
		RequestedBy:  parseUUID(userID),
	})
	if err != nil {
		// The unique partial index is the real guard: the read above and this
		// insert are not one atomic step, and two clients racing on the same
		// run is exactly the case that must not produce two reverts.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "a revert is already in progress for this run")
			return
		}
		slog.Error("create worktree revert request failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "could not request the revert")
		return
	}

	// "member", not "user": the audit table's actor_type CHECK allows only
	// member / agent / system, and a rejected write would lose the record of
	// who asked for a destructive action.
	h.audit(r.Context(), issue.WorkspaceID, "member", userID, AuditWorktreeReverted, "task", task.ID, map[string]any{
		"request_id":     uuidToString(row.ID),
		"issue_id":       uuidToString(issue.ID),
		"branch":         task.BranchName.String,
		"checkpoint_sha": task.CheckpointSha.String,
		"turn_seq":       task.TurnSeq.Int32,
	}, nil)

	h.requestDaemonPendingWork(uuidToString(task.RuntimeID), protocol.PendingWorkKindWorktreeRevert)
	writeJSON(w, http.StatusAccepted, worktreeRevertToResponse(row))
}

// GetWorktreeRevertRequest lets the UI follow a revert it started.
//
// GET /api/issues/{id}/runs/{taskId}/revert/{requestId}
func (h *Handler) GetWorktreeRevertRequest(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "requestId"), "requestId")
	if !ok {
		return
	}
	row, err := h.Queries.GetWorktreeRevertRequest(r.Context(), requestID)
	if err != nil || uuidToString(row.IssueID) != uuidToString(issue.ID) {
		writeError(w, http.StatusNotFound, "revert request not found")
		return
	}
	writeJSON(w, http.StatusOK, worktreeRevertToResponse(row))
}

// claimWorktreeRevertForHeartbeat hands the runtime's oldest pending revert to
// the daemon, with everything it needs to do the work in one payload.
//
// Returns nil when there is nothing to do, which is the overwhelmingly common
// case — the probe before it is a single index lookup.
func (h *Handler) claimWorktreeRevertForHeartbeat(ctx context.Context, runtimeID pgtype.UUID) *protocol.DaemonHeartbeatPendingWorktreeRevert {
	row, err := h.Queries.ClaimWorktreeRevertRequest(ctx, runtimeID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("claim worktree revert failed", "runtime_id", uuidToString(runtimeID), "error", err)
		}
		return nil
	}
	pending, err := h.buildWorktreeRevertPayload(ctx, row)
	if err != nil {
		// The claim already happened, so settle it here rather than leaving the
		// user waiting for a sweeper: the request is unworkable, not delayed.
		h.settleWorktreeRevert(ctx, row.ID, worktreeRevertStatusFailed, err.Error())
		return nil
	}
	return pending
}

// buildWorktreeRevertPayload resolves everything the daemon needs: the
// repository, the branch, the checkpoint and the turns that go with it.
func (h *Handler) buildWorktreeRevertPayload(ctx context.Context, row db.WorktreeRevertRequest) (*protocol.DaemonHeartbeatPendingWorktreeRevert, error) {
	task, err := h.Queries.GetAgentTask(ctx, row.TargetTaskID)
	if err != nil {
		return nil, fmt.Errorf("the run this revert targets no longer exists")
	}
	branch := strings.TrimSpace(task.BranchName.String)
	checkpoint := strings.TrimSpace(task.CheckpointSha.String)
	if branch == "" || checkpoint == "" {
		return nil, fmt.Errorf("the run this revert targets no longer carries a branch and a checkpoint")
	}
	localPath, err := h.resolveWorktreeRevertLocalPath(ctx, task)
	if err != nil {
		return nil, err
	}
	later, err := h.laterTurnsOf(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("could not list the runs this revert would remove")
	}
	ids := make([]string, 0, len(later))
	for _, t := range later {
		ids = append(ids, uuidToString(t.ID))
	}
	return &protocol.DaemonHeartbeatPendingWorktreeRevert{
		ID:           uuidToString(row.ID),
		LocalPath:    localPath,
		Branch:       branch,
		Checkpoint:   checkpoint,
		LaterTaskIDs: ids,
	}, nil
}

// resolveWorktreeRevertLocalPath finds the repository the run's branch lives
// in: the project's local_directory resource bound to the runtime's own daemon.
//
// Scoped to that daemon on purpose — a project may carry one local_directory
// per machine, and another machine's path names a repository this daemon cannot
// see.
func (h *Handler) resolveWorktreeRevertLocalPath(ctx context.Context, task db.AgentTaskQueue) (string, error) {
	if !task.RuntimeID.Valid {
		return "", fmt.Errorf("the run is not bound to a runtime")
	}
	runtime, err := h.Queries.GetAgentRuntime(ctx, task.RuntimeID)
	if err != nil || !runtime.DaemonID.Valid {
		return "", fmt.Errorf("the runtime that produced this run is no longer registered")
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil || !issue.ProjectID.Valid {
		return "", fmt.Errorf("this run's issue is no longer attached to a project")
	}
	rows, err := h.Queries.ListProjectResources(ctx, issue.ProjectID)
	if err != nil {
		return "", fmt.Errorf("could not read the project's resources")
	}
	for _, res := range rows {
		if res.ResourceType != "local_directory" {
			continue
		}
		var ref localDirectoryRef
		if err := json.Unmarshal(res.ResourceRef, &ref); err != nil {
			continue
		}
		if ref.DaemonID != runtime.DaemonID.String {
			continue
		}
		if strings.TrimSpace(ref.LocalPath) == "" {
			continue
		}
		return ref.LocalPath, nil
	}
	return "", fmt.Errorf("this run's project no longer has a local directory on that machine")
}

// laterTurnsOf lists the conversation's checkpointed turns after this one —
// the runs a revert removes.
func (h *Handler) laterTurnsOf(ctx context.Context, task db.AgentTaskQueue) ([]db.AgentTaskQueue, error) {
	if !task.TurnSeq.Valid {
		return nil, nil
	}
	params := db.ListTaskTurnsAfterParams{AfterTurnSeq: task.TurnSeq.Int32}
	switch {
	case task.IssueID.Valid:
		params.IssueID = task.IssueID
	case task.ChatSessionID.Valid:
		params.ChatSessionID = task.ChatSessionID
	default:
		return nil, nil
	}
	return h.Queries.ListTaskTurnsAfter(ctx, params)
}

// ReportWorktreeRevertResult receives the daemon's outcome and, on success,
// removes the turns that no longer exist.
//
// POST /api/daemon/runtimes/{runtimeId}/worktree-revert/{requestId}/result
func (h *Handler) ReportWorktreeRevertResult(w http.ResponseWriter, r *http.Request) {
	runtime, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "requestId"), "requestId")
	if !ok {
		return
	}
	row, err := h.Queries.GetWorktreeRevertRequest(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusNotFound, "revert request not found")
		return
	}
	if uuidToString(row.RuntimeID) != uuidToString(runtime.ID) {
		writeError(w, http.StatusNotFound, "revert request not found")
		return
	}
	// A retried report after the first one landed is not an error: the daemon
	// retries terminal reports, and this one is destructive, so the second
	// delivery must change nothing.
	if row.Status == worktreeRevertStatusDone || row.Status == worktreeRevertStatusFailed {
		writeJSON(w, http.StatusOK, worktreeRevertToResponse(row))
		return
	}

	var body struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if body.Status != "completed" {
		msg := strings.TrimSpace(body.Error)
		if msg == "" {
			msg = "the revert did not complete"
		}
		settled := h.settleWorktreeRevert(r.Context(), row.ID, worktreeRevertStatusFailed, msg)
		writeJSON(w, http.StatusOK, settled)
		return
	}

	removed, err := h.applyWorktreeRevert(r.Context(), row)
	if err != nil {
		slog.Error("apply worktree revert failed after the daemon moved the branch",
			"request_id", uuidToString(row.ID), "error", err)
		settled := h.settleWorktreeRevert(r.Context(), row.ID, worktreeRevertStatusFailed,
			"the branch was reverted but the runs after it could not be removed; refresh and try again")
		writeJSON(w, http.StatusOK, settled)
		return
	}

	settled := h.settleWorktreeRevert(r.Context(), row.ID, worktreeRevertStatusDone, "")
	h.publishTask(protocol.EventTaskReverted, uuidToString(row.WorkspaceID), "system", "",
		uuidToString(row.TargetTaskID), map[string]any{
			"issue_id":         uuidToString(row.IssueID),
			"chat_session_id":  uuidToString(row.ChatSessionID),
			"target_task_id":   uuidToString(row.TargetTaskID),
			"removed_task_ids": removed,
		})
	writeJSON(w, http.StatusOK, settled)
}

// applyWorktreeRevert deletes the runs of the turns the daemon just removed
// from the branch, in one transaction.
//
// Their task_message rows follow through the foreign key inherited from
// migration 026 — the one legacy cascade this feature relies on rather than
// adding a new one. Atomic because a half-applied delete leaves a conversation
// whose transcript and run list disagree about what happened.
func (h *Handler) applyWorktreeRevert(ctx context.Context, row db.WorktreeRevertRequest) ([]string, error) {
	task, err := h.Queries.GetAgentTask(ctx, row.TargetTaskID)
	if err != nil {
		return nil, err
	}
	later, err := h.laterTurnsOf(ctx, task)
	if err != nil {
		return nil, err
	}
	if len(later) == 0 {
		return []string{}, nil
	}
	ids := make([]pgtype.UUID, 0, len(later))
	removed := make([]string, 0, len(later))
	for _, t := range later {
		ids = append(ids, t.ID)
		removed = append(removed, uuidToString(t.ID))
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := h.Queries.WithTx(tx).DeleteAgentTasksByID(ctx, ids); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return removed, nil
}

func (h *Handler) settleWorktreeRevert(ctx context.Context, id pgtype.UUID, status, errMsg string) WorktreeRevertResponse {
	row, err := h.Queries.SettleWorktreeRevertRequest(ctx, db.SettleWorktreeRevertRequestParams{
		ID:     id,
		Status: status,
		Error:  pgtype.Text{String: errMsg, Valid: errMsg != ""},
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("settle worktree revert failed", "request_id", uuidToString(id), "error", err)
		}
		return WorktreeRevertResponse{RequestID: uuidToString(id), Status: status, Error: errMsg}
	}
	return worktreeRevertToResponse(row)
}
