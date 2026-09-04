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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Fan-out / fan-in (K38). The leader (or a member) splits an issue into
// sub-tasks, one child issue each, assigned to specialist agents and run in
// parallel. The barrier watches their runs: once every member completed or
// failed for good, the leader gets a synthesis run on the parent issue whose
// handoff lists each child's outcome and latest handoff packet. Never a
// second implementation of running: children are ordinary runs.

const (
	AuditFanout         = "fanout"
	ErrCodeFanoutActive = "fanout_batch_already_active"
	fanoutMaxSubTasks   = 20
	fanoutSynthesisLead = "Fan-in synthesis: every specialist run of this fan-out finished. Read each child issue below, merge their results into one coherent outcome on this issue, and call out the sub-tasks that failed."
)

type FanoutSubTaskRequest struct {
	Description string `json:"description"`
	AssigneeID  string `json:"assignee_id"`
}

type FanoutRequest struct {
	LeaderAgentID string                 `json:"leader_agent_id"`
	SubTasks      []FanoutSubTaskRequest `json:"sub_tasks"`
}

type FanoutMemberResponse struct {
	ID              string  `json:"id"`
	ChildIssueID    string  `json:"child_issue_id"`
	TaskID          string  `json:"task_id"`
	TaskStatus      string  `json:"task_status"`
	AssigneeAgentID string  `json:"assignee_agent_id"`
	Description     string  `json:"description"`
	Outcome         *string `json:"outcome"`
	SettledAt       *string `json:"settled_at"`
}

type FanoutBatchResponse struct {
	ID              string                 `json:"id"`
	ParentIssueID   string                 `json:"parent_issue_id"`
	LeaderAgentID   string                 `json:"leader_agent_id"`
	Status          string                 `json:"status"`
	ExpectedCount   int32                  `json:"expected_count"`
	CompletedCount  int32                  `json:"completed_count"`
	FailedCount     int32                  `json:"failed_count"`
	SynthesisTaskID *string                `json:"synthesis_task_id"`
	Members         []FanoutMemberResponse `json:"members"`
	CreatedAt       string                 `json:"created_at"`
	CompletedAt     *string                `json:"completed_at"`
}

func (h *Handler) fanoutToResponse(ctx context.Context, b db.FanoutBatch) FanoutBatchResponse {
	out := FanoutBatchResponse{
		ID: uuidToString(b.ID), ParentIssueID: uuidToString(b.ParentIssueID), LeaderAgentID: uuidToString(b.LeaderAgentID), Status: b.Status,
		ExpectedCount: b.ExpectedCount, CompletedCount: b.CompletedCount, FailedCount: b.FailedCount, SynthesisTaskID: uuidToPtr(b.SynthesisTaskID),
		Members: []FanoutMemberResponse{}, CreatedAt: timestampToString(b.CreatedAt), CompletedAt: timestampToPtr(b.CompletedAt),
	}
	rows, _ := h.Queries.ListFanoutBatchMembers(ctx, b.ID)
	for _, m := range rows {
		out.Members = append(out.Members, FanoutMemberResponse{
			ID: uuidToString(m.ID), ChildIssueID: uuidToString(m.ChildIssueID), TaskID: uuidToString(m.TaskID), TaskStatus: m.TaskStatus, AssigneeAgentID: uuidToString(m.AssigneeAgentID),
			Description: m.SubTaskDescription, Outcome: textToPtr(m.Outcome), SettledAt: timestampToPtr(m.SettledAt),
		})
	}
	return out
}

// StartFanout: POST /api/issues/{id}/fanout.
func (h *Handler) StartFanout(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req FanoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.SubTasks) == 0 || len(req.SubTasks) > fanoutMaxSubTasks {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("between 1 and %d sub-tasks", fanoutMaxSubTasks))
		return
	}
	leaderID, ok := parseUUIDOrBadRequest(w, req.LeaderAgentID, "leader_agent_id")
	if !ok {
		return
	}
	leader, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: leaderID, WorkspaceID: issue.WorkspaceID})
	if err != nil || leader.ArchivedAt.Valid {
		writeError(w, http.StatusUnprocessableEntity, "leader agent not found in this workspace")
		return
	}
	type subTask struct {
		desc  string
		agent db.Agent
	}
	subs := make([]subTask, 0, len(req.SubTasks))
	for i, st := range req.SubTasks {
		desc := strings.TrimSpace(st.Description)
		if desc == "" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("sub-task %d needs a description", i+1))
			return
		}
		aid, ok := parseUUIDOrBadRequest(w, st.AssigneeID, "assignee_id")
		if !ok {
			return
		}
		agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: aid, WorkspaceID: issue.WorkspaceID})
		if err != nil || agent.ArchivedAt.Valid {
			writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("sub-task %d: agent not found in this workspace", i+1))
			return
		}
		subs = append(subs, subTask{desc: desc, agent: agent})
	}
	actorType, actorID := h.resolveActor(r, requestUserID(r), uuidToString(issue.WorkspaceID))
	batch, err := h.Queries.CreateFanoutBatch(r.Context(), db.CreateFanoutBatchParams{ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, ParentIssueID: issue.ID, LeaderAgentID: leader.ID, ExpectedCount: int32(len(subs)), StartedBy: parseUUID(requestUserID(r))})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErrorCode(w, http.StatusConflict, ErrCodeFanoutActive, "a fan-out is already running on this issue")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create the fan-out")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	for i, st := range subs {
		title := st.desc
		if nl := strings.IndexByte(title, '\n'); nl > 0 {
			title = title[:nl]
		}
		if len(title) > 120 {
			title = title[:117] + "..."
		}
		res, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
			WorkspaceID: issue.WorkspaceID, Title: title,
			Description: pgtype.Text{String: fmt.Sprintf("Sub-task %d of the fan-out on %s-%d.\n\n%s", i+1, prefix, issue.Number, st.desc), Valid: true},
			Status:      "todo", Priority: issue.Priority, AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: st.agent.ID,
			CreatorType: actorType, CreatorID: issue.CreatorID, ParentIssueID: issue.ID, ProjectID: issue.ProjectID, AllowDuplicate: true,
		}, service.IssueCreateOpts{ActorID: actorID})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create sub-task issue: "+err.Error())
			return
		}
		// Creating an agent-assigned issue already queues its run; queue one only when it did not.
		task, err := h.Queries.GetLatestTaskForIssue(r.Context(), res.Issue.ID)
		if err != nil {
			if task, err = h.TaskService.EnqueueTaskForIssueByActor(r.Context(), res.Issue, parseUUID(requestUserID(r))); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to queue the specialist run: "+err.Error())
				return
			}
		}
		if _, err := h.Queries.AddFanoutBatchMember(r.Context(), db.AddFanoutBatchMemberParams{ID: dbid.NewV7(), FanoutBatchID: batch.ID, WorkspaceID: issue.WorkspaceID, ChildIssueID: res.Issue.ID, TaskID: task.ID, AssigneeAgentID: st.agent.ID, SubTaskDescription: st.desc}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record the sub-task")
			return
		}
	}
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditFanout, "issue", issue.ID, map[string]any{"batch_id": uuidToString(batch.ID), "leader_agent_id": uuidToString(leader.ID), "sub_tasks": len(subs), "started": true}, nil)
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	writeJSON(w, http.StatusCreated, map[string]any{"batch": h.fanoutToResponse(r.Context(), batch)})
}

// GetIssueFanout: GET /api/issues/{id}/fanout — the latest batch.
func (h *Handler) GetIssueFanout(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	b, err := h.Queries.GetLatestFanoutBatchForIssue(r.Context(), issue.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"batch": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batch": h.fanoutToResponse(r.Context(), b)})
}

// GetFanoutBatch: GET /api/fanout-batches/{id}.
func (h *Handler) GetFanoutBatch(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "batch id")
	if !ok {
		return
	}
	b, err := h.Queries.GetFanoutBatch(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "fan-out batch not found")
		return
	}
	if _, ok := h.loadIssueForUser(w, r, uuidToString(b.ParentIssueID)); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"batch": h.fanoutToResponse(r.Context(), b)})
}

// updateFanoutBarrier (K38) is called when a run reaches a terminal
// status: a completed child settles its member, a failed child settles it
// only when no retry is coming; the last settlement starts the synthesis.
func (h *Handler) updateFanoutBarrier(ctx context.Context, task db.AgentTaskQueue) {
	root := task.ID
	if task.RetryOfTaskID.Valid {
		root = task.RetryOfTaskID
	} else if task.ParentTaskID.Valid {
		root = task.ParentTaskID
	}
	member, err := h.Queries.GetFanoutMemberForTask(ctx, db.GetFanoutMemberForTaskParams{TaskID: task.ID, RootTaskID: root})
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
	if _, err := h.Queries.SettleFanoutMember(ctx, db.SettleFanoutMemberParams{ID: member.ID, Outcome: pgtype.Text{String: outcome, Valid: true}, SettledTaskID: task.ID}); err != nil {
		return
	}
	batch, err := h.Queries.GetFanoutBatch(ctx, member.FanoutBatchID)
	if err != nil || batch.Status != "pending" {
		return
	}
	counts, err := h.Queries.CountFanoutOutcomes(ctx, batch.ID)
	if err != nil {
		return
	}
	if counts.Completed+counts.Failed < batch.ExpectedCount {
		_ = h.Queries.UpdateFanoutCounts(ctx, db.UpdateFanoutCountsParams{ID: batch.ID, CompletedCount: counts.Completed, FailedCount: counts.Failed})
		h.publishFanoutProgress(ctx, batch)
		return
	}
	status := "complete"
	if counts.Failed > 0 {
		status = "partial_failure"
	}
	synthesis := h.startFanoutSynthesis(ctx, batch, counts.Failed > 0)
	if _, err := h.Queries.SettleFanoutBatch(ctx, db.SettleFanoutBatchParams{ID: batch.ID, CompletedCount: counts.Completed, FailedCount: counts.Failed, Status: status, SynthesisTaskID: synthesis}); err != nil {
		slog.Warn("fanout: settle batch failed", "batch_id", uuidToString(batch.ID), "error", err)
		return
	}
	h.audit(ctx, batch.WorkspaceID, "system", "", AuditFanout, "issue", batch.ParentIssueID, map[string]any{"batch_id": uuidToString(batch.ID), "status": status, "completed": counts.Completed, "failed": counts.Failed, "synthesis_task_id": uuidToPtr(synthesis)}, nil)
	h.publishFanoutProgress(ctx, batch)
}

// startFanoutSynthesis assigns the parent issue to the leader and queues its
// run with every child's outcome in the handoff. Returns the run id.
func (h *Handler) startFanoutSynthesis(ctx context.Context, batch db.FanoutBatch, partial bool) pgtype.UUID {
	issue, err := h.Queries.GetIssue(ctx, batch.ParentIssueID)
	if err != nil {
		return pgtype.UUID{}
	}
	members, _ := h.Queries.ListFanoutBatchMembers(ctx, batch.ID)
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	var note strings.Builder
	note.WriteString(fanoutSynthesisLead)
	if partial {
		note.WriteString("\nWARNING: some sub-tasks failed for good; their results are missing and must be said so.")
	}
	for _, m := range members {
		child, err := h.Queries.GetIssue(ctx, m.ChildIssueID)
		if err != nil {
			continue
		}
		outcome := m.Outcome.String
		if outcome == "" {
			outcome = "unsettled"
		}
		fmt.Fprintf(&note, "\n- %s-%d [%s] %s", prefix, child.Number, outcome, child.Title)
		if p, err := h.Queries.GetLatestHandoffPacket(ctx, child.ID); err == nil {
			if p.NextAction.Valid && p.NextAction.String != "" {
				note.WriteString(" — next action: " + p.NextAction.String)
			}
			if len(jsonStrings(p.FailedAttempts)) > 0 {
				note.WriteString(" — failed attempts: " + strings.Join(jsonStrings(p.FailedAttempts), "; "))
			}
		}
	}
	updated, err := h.Queries.SetIssueAssigneeForPipeline(ctx, db.SetIssueAssigneeForPipelineParams{ID: issue.ID, AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: batch.LeaderAgentID})
	if err != nil {
		return pgtype.UUID{}
	}
	task, err := h.TaskService.EnqueueTaskForIssueWithHandoff(ctx, updated, note.String(), batch.StartedBy)
	if err != nil {
		slog.Warn("fanout: synthesis enqueue failed", "batch_id", uuidToString(batch.ID), "error", err)
		return pgtype.UUID{}
	}
	return task.ID
}

func (h *Handler) publishFanoutProgress(ctx context.Context, batch db.FanoutBatch) {
	if issue, err := h.Queries.GetIssue(ctx, batch.ParentIssueID); err == nil {
		h.publish("fanout:progress", uuidToString(issue.WorkspaceID), "system", "", map[string]any{"issue_id": uuidToString(issue.ID), "batch_id": uuidToString(batch.ID)})
	}
}
