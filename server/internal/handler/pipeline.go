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
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Pipelines (K37): an ordered chain of stages (agent or squad, optional
// human gate) an issue moves through. A stage starts by assigning the issue
// to its executor and queuing a run; the executor's completed run advances
// the cursor; a gated stage first asks a Decision Card (K01) and waits for
// the approval (K05 path); a missing executor pauses the run with an
// explicit error. One open run per issue.

const (
	AuditPipelineChanged     = "pipeline.changed"
	AuditPipelineRun         = "pipeline.run"
	ErrCodePipelineRunOpen   = "pipeline_run_open"
	ErrCodeStageExecutorGone = "pipeline_stage_executor_unavailable"
	pipelineApproveOption    = "advance"
	pipelineHoldOption       = "hold"
)

type PipelineStageRequest struct {
	Name              string `json:"name"`
	ExecutorType      string `json:"executor_type"`
	ExecutorID        string `json:"executor_id"`
	RequiresHumanGate bool   `json:"requires_human_gate"`
}

type PipelineRequest struct {
	Name   string                 `json:"name"`
	Stages []PipelineStageRequest `json:"stages"`
}

type PipelineStageResponse struct {
	ID                string `json:"id"`
	Position          int32  `json:"position"`
	Name              string `json:"name"`
	ExecutorType      string `json:"executor_type"`
	ExecutorID        string `json:"executor_id"`
	RequiresHumanGate bool   `json:"requires_human_gate"`
}

type PipelineResponse struct {
	ID        string                  `json:"id"`
	Name      string                  `json:"name"`
	Stages    []PipelineStageResponse `json:"stages"`
	OpenRuns  int64                   `json:"open_runs"`
	CreatedAt string                  `json:"created_at"`
}

type PipelineRunResponse struct {
	ID             string                  `json:"id"`
	PipelineID     string                  `json:"pipeline_id"`
	PipelineName   string                  `json:"pipeline_name"`
	IssueID        string                  `json:"issue_id"`
	Status         string                  `json:"status"`
	CurrentStageID *string                 `json:"current_stage_id"`
	CurrentIndex   int                     `json:"current_index"`
	GateDecisionID *string                 `json:"gate_decision_id"`
	LastError      *string                 `json:"last_error"`
	Stages         []PipelineStageResponse `json:"stages"`
	StartedAt      string                  `json:"started_at"`
	CompletedAt    *string                 `json:"completed_at"`
}

func stageToResponse(s db.PipelineStage) PipelineStageResponse {
	return PipelineStageResponse{ID: uuidToString(s.ID), Position: s.Position, Name: s.Name, ExecutorType: s.ExecutorType, ExecutorID: uuidToString(s.ExecutorID), RequiresHumanGate: s.RequiresHumanGate}
}

func (h *Handler) pipelineToResponse(ctx context.Context, p db.Pipeline) PipelineResponse {
	stages, _ := h.Queries.ListPipelineStages(ctx, p.ID)
	out := PipelineResponse{ID: uuidToString(p.ID), Name: p.Name, Stages: []PipelineStageResponse{}, CreatedAt: timestampToString(p.CreatedAt)}
	for _, s := range stages {
		out.Stages = append(out.Stages, stageToResponse(s))
	}
	out.OpenRuns, _ = h.Queries.CountOpenPipelineRunsForPipeline(ctx, p.ID)
	return out
}

func (h *Handler) pipelineRunToResponse(ctx context.Context, run db.PipelineRun) PipelineRunResponse {
	out := PipelineRunResponse{
		ID: uuidToString(run.ID), PipelineID: uuidToString(run.PipelineID), IssueID: uuidToString(run.IssueID), Status: run.Status,
		CurrentStageID: uuidToPtr(run.CurrentStageID), GateDecisionID: uuidToPtr(run.GateDecisionID), LastError: textToPtr(run.LastError),
		Stages: []PipelineStageResponse{}, StartedAt: timestampToString(run.StartedAt), CompletedAt: timestampToPtr(run.CompletedAt), CurrentIndex: -1,
	}
	if p, err := h.Queries.GetPipeline(ctx, run.PipelineID); err == nil {
		out.PipelineName = p.Name
	}
	stages, _ := h.Queries.ListPipelineStages(ctx, run.PipelineID)
	for i, s := range stages {
		out.Stages = append(out.Stages, stageToResponse(s))
		if run.CurrentStageID.Valid && s.ID == run.CurrentStageID {
			out.CurrentIndex = i
		}
	}
	if run.Status == "completed" {
		out.CurrentIndex = len(stages)
	}
	return out
}

// validateStages checks every executor belongs to the workspace.
func (h *Handler) validateStages(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID, stages []PipelineStageRequest) ([]db.CreatePipelineStageParams, bool) {
	if len(stages) == 0 {
		writeError(w, http.StatusBadRequest, "a pipeline needs at least one stage")
		return nil, false
	}
	out := make([]db.CreatePipelineStageParams, 0, len(stages))
	for i, st := range stages {
		name := strings.TrimSpace(st.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("stage %d needs a name", i+1))
			return nil, false
		}
		id, ok := parseUUIDOrBadRequest(w, st.ExecutorID, "executor_id")
		if !ok {
			return nil, false
		}
		switch st.ExecutorType {
		case "agent":
			if _, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: id, WorkspaceID: wsUUID}); err != nil {
				writeErrorCode(w, http.StatusUnprocessableEntity, ErrCodeStageExecutorGone, "stage "+name+": agent not found in this workspace")
				return nil, false
			}
		case "squad":
			if sq, err := h.Queries.GetSquad(r.Context(), id); err != nil || sq.WorkspaceID != wsUUID {
				writeErrorCode(w, http.StatusUnprocessableEntity, ErrCodeStageExecutorGone, "stage "+name+": squad not found in this workspace")
				return nil, false
			}
		default:
			writeError(w, http.StatusBadRequest, "executor_type must be agent or squad")
			return nil, false
		}
		out = append(out, db.CreatePipelineStageParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Position: int32(i), Name: name, ExecutorType: st.ExecutorType, ExecutorID: id, RequiresHumanGate: st.RequiresHumanGate})
	}
	return out, true
}

func (h *Handler) loadPipeline(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.Pipeline, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "pipeline id")
	if !ok {
		return db.Pipeline{}, false
	}
	p, err := h.Queries.GetPipeline(r.Context(), id)
	if err != nil || p.WorkspaceID != wsUUID || p.ArchivedAt.Valid {
		writeError(w, http.StatusNotFound, "pipeline not found")
		return db.Pipeline{}, false
	}
	return p, true
}

func (h *Handler) ListPipelines(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPipelines(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list pipelines")
		return
	}
	out := make([]PipelineResponse, 0, len(rows))
	for _, p := range rows {
		out = append(out, h.pipelineToResponse(r.Context(), p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"pipelines": out})
}

func (h *Handler) CreatePipeline(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req PipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	stages, ok := h.validateStages(w, r, wsUUID, req.Stages)
	if !ok {
		return
	}
	p, err := h.Queries.CreatePipeline(r.Context(), db.CreatePipelineParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Name: strings.TrimSpace(req.Name), CreatedBy: parseUUID(requestUserID(r))})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create pipeline")
		return
	}
	for _, st := range stages {
		st.PipelineID = p.ID
		if _, err := h.Queries.CreatePipelineStage(r.Context(), st); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create pipeline stage")
			return
		}
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditPipelineChanged, "pipeline", p.ID, map[string]any{"name": p.Name, "stages": len(stages), "created": true}, nil)
	writeJSON(w, http.StatusCreated, h.pipelineToResponse(r.Context(), p))
}

// UpdatePipeline replaces name and stages. Stages are replaced wholesale;
// an open run keeps pointing at its stage id, so only stages with no open
// run may be rewritten.
func (h *Handler) UpdatePipeline(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	p, ok := h.loadPipeline(w, r, wsUUID)
	if !ok {
		return
	}
	var req PipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Name) != "" && strings.TrimSpace(req.Name) != p.Name {
		if _, err := h.Queries.UpdatePipelineName(r.Context(), db.UpdatePipelineNameParams{ID: p.ID, Name: strings.TrimSpace(req.Name)}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to rename pipeline")
			return
		}
	}
	if req.Stages != nil {
		if n, _ := h.Queries.CountOpenPipelineRunsForPipeline(r.Context(), p.ID); n > 0 {
			writeErrorCode(w, http.StatusConflict, ErrCodePipelineRunOpen, "issues are still moving through this pipeline; finish or cancel their runs before changing its stages")
			return
		}
		stages, ok := h.validateStages(w, r, wsUUID, req.Stages)
		if !ok {
			return
		}
		if err := h.Queries.DeletePipelineStages(r.Context(), p.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to replace stages")
			return
		}
		for _, st := range stages {
			st.PipelineID = p.ID
			if _, err := h.Queries.CreatePipelineStage(r.Context(), st); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to create pipeline stage")
				return
			}
		}
	}
	updated, _ := h.Queries.GetPipeline(r.Context(), p.ID)
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditPipelineChanged, "pipeline", p.ID, map[string]any{"name": updated.Name, "stages": len(req.Stages)}, nil)
	writeJSON(w, http.StatusOK, h.pipelineToResponse(r.Context(), updated))
}

func (h *Handler) DeletePipeline(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	p, ok := h.loadPipeline(w, r, wsUUID)
	if !ok {
		return
	}
	if n, _ := h.Queries.CountOpenPipelineRunsForPipeline(r.Context(), p.ID); n > 0 {
		writeErrorCode(w, http.StatusConflict, ErrCodePipelineRunOpen, "issues are still moving through this pipeline")
		return
	}
	if err := h.Queries.ArchivePipeline(r.Context(), p.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to archive pipeline")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditPipelineChanged, "pipeline", p.ID, map[string]any{"name": p.Name, "archived": true}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// StartPipelineRun: POST /api/issues/{id}/pipeline-run {pipeline_id}.
func (h *Handler) StartPipelineRun(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req struct {
		PipelineID string `json:"pipeline_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pid, ok := parseUUIDOrBadRequest(w, req.PipelineID, "pipeline_id")
	if !ok {
		return
	}
	p, err := h.Queries.GetPipeline(r.Context(), pid)
	if err != nil || p.WorkspaceID != issue.WorkspaceID || p.ArchivedAt.Valid {
		writeError(w, http.StatusNotFound, "pipeline not found")
		return
	}
	stages, err := h.Queries.ListPipelineStages(r.Context(), p.ID)
	if err != nil || len(stages) == 0 {
		writeError(w, http.StatusBadRequest, "this pipeline has no stage")
		return
	}
	run, err := h.Queries.CreatePipelineRun(r.Context(), db.CreatePipelineRunParams{ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, PipelineID: p.ID, CurrentStageID: stages[0].ID, StartedBy: parseUUID(requestUserID(r))})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeErrorCode(w, http.StatusConflict, ErrCodePipelineRunOpen, "this issue already moves through a pipeline")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to start pipeline run")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", requestUserID(r), AuditPipelineRun, "issue", issue.ID, map[string]any{"run_id": uuidToString(run.ID), "pipeline_id": uuidToString(p.ID), "started": true}, nil)
	run = h.startStage(r.Context(), run, stages[0], issue, parseUUID(requestUserID(r)))
	h.publishIssueAuxChanged(r, issue, "member", requestUserID(r))
	writeJSON(w, http.StatusCreated, map[string]any{"run": h.pipelineRunToResponse(r.Context(), run)})
}

// startStage assigns the issue to the stage's executor and queues a run;
// a missing executor pauses the pipeline run with an explicit error.
func (h *Handler) startStage(ctx context.Context, run db.PipelineRun, stage db.PipelineStage, issue db.Issue, actor pgtype.UUID) db.PipelineRun {
	fail := func(msg string) db.PipelineRun {
		paused, err := h.Queries.SetPipelineRunError(ctx, db.SetPipelineRunErrorParams{ID: run.ID, LastError: pgtype.Text{String: msg, Valid: true}})
		if err != nil {
			return run
		}
		h.notifyPipeline(ctx, issue, "Pipeline stopped at stage "+stage.Name+": "+msg, "action_required", run)
		return paused
	}
	switch stage.ExecutorType {
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: stage.ExecutorID, WorkspaceID: issue.WorkspaceID})
		if err != nil || agent.ArchivedAt.Valid {
			return fail("its agent no longer exists; reassign the stage")
		}
		updated, err := h.Queries.SetIssueAssigneeForPipeline(ctx, db.SetIssueAssigneeForPipelineParams{ID: issue.ID, AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: agent.ID})
		if err != nil {
			return fail("could not assign the issue: " + err.Error())
		}
		if _, err := h.TaskService.EnqueueTaskForIssueByActor(ctx, updated, actor); err != nil {
			return fail("could not queue the run: " + err.Error())
		}
	case "squad":
		sq, err := h.Queries.GetSquad(ctx, stage.ExecutorID)
		if err != nil || sq.WorkspaceID != issue.WorkspaceID || sq.ArchivedAt.Valid {
			return fail("its squad no longer exists; reassign the stage")
		}
		updated, err := h.Queries.SetIssueAssigneeForPipeline(ctx, db.SetIssueAssigneeForPipelineParams{ID: issue.ID, AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: sq.LeaderID})
		if err != nil {
			return fail("could not assign the issue: " + err.Error())
		}
		if _, err := h.TaskService.EnqueueTaskForSquadLeaderByActor(ctx, updated, sq.LeaderID, sq.ID, actor); err != nil {
			return fail("could not queue the squad run: " + err.Error())
		}
	}
	slog.Info("pipeline: stage started", "run_id", uuidToString(run.ID), "stage", stage.Name)
	return run
}

// advancePipelineAfterTask (K37) is called when a run completes: if the
// issue moves through a pipeline and the completed run belongs to the
// current stage's executor, the cursor advances.
func (h *Handler) advancePipelineAfterTask(ctx context.Context, task db.AgentTaskQueue) {
	if !task.IssueID.Valid {
		return
	}
	run, err := h.Queries.GetOpenPipelineRunForIssue(ctx, task.IssueID)
	if err != nil || run.Status != "active" || !run.CurrentStageID.Valid {
		return
	}
	stage, err := h.Queries.GetPipelineStage(ctx, run.CurrentStageID)
	if err != nil {
		return
	}
	executor := stage.ExecutorID
	if stage.ExecutorType == "squad" {
		if sq, err := h.Queries.GetSquad(ctx, stage.ExecutorID); err == nil {
			executor = sq.LeaderID
		}
	}
	if task.AgentID != executor {
		return
	}
	h.advancePipeline(ctx, run, stage, "system", "")
}

// advancePipeline moves to the next stage, gating first when it asks for it.
func (h *Handler) advancePipeline(ctx context.Context, run db.PipelineRun, from db.PipelineStage, actorType, actorID string) {
	issue, err := h.Queries.GetIssue(ctx, run.IssueID)
	if err != nil {
		return
	}
	stages, err := h.Queries.ListPipelineStages(ctx, run.PipelineID)
	if err != nil {
		return
	}
	var next *db.PipelineStage
	for i := range stages {
		if stages[i].Position > from.Position {
			next = &stages[i]
			break
		}
	}
	if next == nil {
		if _, err := h.Queries.FinishPipelineRun(ctx, db.FinishPipelineRunParams{ID: run.ID, Status: "completed"}); err == nil {
			h.audit(ctx, issue.WorkspaceID, actorType, actorID, AuditPipelineRun, "issue", issue.ID, map[string]any{"run_id": uuidToString(run.ID), "completed": true}, nil)
			h.notifyPipeline(ctx, issue, "Pipeline completed: every stage is done", "info", run)
		}
		return
	}
	if next.RequiresHumanGate {
		options, _ := json.Marshal([]DecisionOption{
			{ID: pipelineApproveOption, Label: "Advance to " + next.Name, Impact: "the issue is handed to " + next.ExecutorType + " " + next.Name},
			{ID: pipelineHoldOption, Label: "Stop the pipeline", Impact: "the issue stays where it is"},
		})
		// The card is asked on behalf of whoever started the pipeline.
		decision, err := h.Queries.CreateIssueDecision(ctx, db.CreateIssueDecisionParams{
			WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AskedByType: "member", AskedByID: run.StartedBy, Question: "Pipeline gate · " + from.Name + " is done. Advance to " + next.Name + "?",
			Options: options, RecommendedOptionID: pgtype.Text{String: pipelineApproveOption, Valid: true}, Urgency: "normal", SlaDeadlineAt: h.decisionDeadline(ctx, issue.WorkspaceID),
		})
		if err != nil {
			slog.Warn("pipeline: gate decision failed", "run_id", uuidToString(run.ID), "error", err)
			return
		}
		if _, err := h.Queries.SetPipelineRunGate(ctx, db.SetPipelineRunGateParams{ID: run.ID, GateDecisionID: decision.ID, CurrentStageID: next.ID}); err != nil {
			return
		}
		h.notifyDecisionRequested(ctx, issue, decision, "member", uuidToString(run.StartedBy))
		h.audit(ctx, issue.WorkspaceID, actorType, actorID, AuditPipelineRun, "issue", issue.ID, map[string]any{"run_id": uuidToString(run.ID), "gate_stage": next.Name, "decision_id": uuidToString(decision.ID)}, nil)
		return
	}
	moved, err := h.Queries.SetPipelineRunStage(ctx, db.SetPipelineRunStageParams{ID: run.ID, CurrentStageID: next.ID})
	if err != nil {
		return
	}
	h.audit(ctx, issue.WorkspaceID, actorType, actorID, AuditPipelineRun, "issue", issue.ID, map[string]any{"run_id": uuidToString(run.ID), "stage": next.Name}, nil)
	h.startStage(ctx, moved, *next, issue, pgtype.UUID{})
}

// advancePipelineForDecision (K37) answers the gate card: advance or stop.
func (h *Handler) advancePipelineForDecision(ctx context.Context, decision db.IssueDecision, optionID, actorType, actorID string) bool {
	run, err := h.Queries.GetPipelineRunByGateDecision(ctx, decision.ID)
	if err != nil {
		return false
	}
	issue, err := h.Queries.GetIssue(ctx, run.IssueID)
	if err != nil {
		return true
	}
	if optionID != pipelineApproveOption {
		if _, err := h.Queries.FinishPipelineRun(ctx, db.FinishPipelineRunParams{ID: run.ID, Status: "cancelled"}); err == nil {
			h.audit(ctx, issue.WorkspaceID, actorType, actorID, AuditPipelineRun, "issue", issue.ID, map[string]any{"run_id": uuidToString(run.ID), "cancelled_at_gate": true}, nil)
		}
		return true
	}
	stage, err := h.Queries.GetPipelineStage(ctx, run.CurrentStageID)
	if err != nil {
		return true
	}
	moved, err := h.Queries.SetPipelineRunStage(ctx, db.SetPipelineRunStageParams{ID: run.ID, CurrentStageID: stage.ID})
	if err != nil {
		return true
	}
	h.audit(ctx, issue.WorkspaceID, actorType, actorID, AuditPipelineRun, "issue", issue.ID, map[string]any{"run_id": uuidToString(run.ID), "stage": stage.Name, "gate_approved": true}, nil)
	h.startStage(ctx, moved, stage, issue, parseUUID(actorID))
	return true
}

func (h *Handler) notifyPipeline(ctx context.Context, issue db.Issue, title, severity string, run db.PipelineRun) {
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, issue.WorkspaceID)
	if err != nil {
		return
	}
	details, _ := json.Marshal(map[string]any{"pipeline_run_id": uuidToString(run.ID), "pipeline_id": uuidToString(run.PipelineID)})
	for _, rcpt := range recipients {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, RecipientType: rcpt.Type, RecipientID: rcpt.ID, Type: "pipeline", Severity: severity,
			IssueID: issue.ID, Title: title, Body: pgtype.Text{String: issue.Title, Valid: true}, ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		})
		if err != nil {
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(issue.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
	}
}

// GetIssuePipelineRun: GET /api/issues/{id}/pipeline-run — the open run, else the latest.
func (h *Handler) GetIssuePipelineRun(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	run, err := h.Queries.GetLatestPipelineRunForIssue(r.Context(), issue.ID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"run": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": h.pipelineRunToResponse(r.Context(), run)})
}

func (h *Handler) loadPipelineRun(w http.ResponseWriter, r *http.Request) (db.PipelineRun, db.Issue, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "run id")
	if !ok {
		return db.PipelineRun{}, db.Issue{}, false
	}
	run, err := h.Queries.GetPipelineRun(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "pipeline run not found")
		return db.PipelineRun{}, db.Issue{}, false
	}
	issue, ok := h.loadIssueForUser(w, r, uuidToString(run.IssueID))
	if !ok {
		return db.PipelineRun{}, db.Issue{}, false
	}
	return run, issue, true
}

// AdvancePipelineRun: POST /api/pipeline-runs/{id}/advance — a human moves
// the cursor (after a gate, or to retry a stage whose executor was fixed).
func (h *Handler) AdvancePipelineRun(w http.ResponseWriter, r *http.Request) {
	run, issue, ok := h.loadPipelineRun(w, r)
	if !ok {
		return
	}
	if run.Status == "completed" || run.Status == "cancelled" {
		writeError(w, http.StatusConflict, "this pipeline run is over")
		return
	}
	stage, err := h.Queries.GetPipelineStage(r.Context(), run.CurrentStageID)
	if err != nil {
		writeError(w, http.StatusConflict, "the current stage no longer exists")
		return
	}
	if run.Status == "paused" && run.GateDecisionID.Valid {
		// Approving here answers the gate like the card would.
		h.advancePipelineForDecision(r.Context(), db.IssueDecision{ID: run.GateDecisionID}, pipelineApproveOption, "member", requestUserID(r))
	} else if run.Status == "paused" {
		// Retry the stage after fixing its executor.
		moved, err := h.Queries.SetPipelineRunStage(r.Context(), db.SetPipelineRunStageParams{ID: run.ID, CurrentStageID: stage.ID})
		if err == nil {
			h.startStage(r.Context(), moved, stage, issue, parseUUID(requestUserID(r)))
		}
	} else {
		h.advancePipeline(r.Context(), run, stage, "member", requestUserID(r))
	}
	latest, _ := h.Queries.GetPipelineRun(r.Context(), run.ID)
	h.publishIssueAuxChanged(r, issue, "member", requestUserID(r))
	writeJSON(w, http.StatusOK, map[string]any{"run": h.pipelineRunToResponse(r.Context(), latest)})
}

// CancelPipelineRun: POST /api/pipeline-runs/{id}/cancel.
func (h *Handler) CancelPipelineRun(w http.ResponseWriter, r *http.Request) {
	run, issue, ok := h.loadPipelineRun(w, r)
	if !ok {
		return
	}
	if run.Status == "completed" || run.Status == "cancelled" {
		writeError(w, http.StatusConflict, "this pipeline run is over")
		return
	}
	done, err := h.Queries.FinishPipelineRun(r.Context(), db.FinishPipelineRunParams{ID: run.ID, Status: "cancelled"})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to cancel pipeline run")
		return
	}
	h.audit(r.Context(), issue.WorkspaceID, "member", requestUserID(r), AuditPipelineRun, "issue", issue.ID, map[string]any{"run_id": uuidToString(run.ID), "cancelled": true}, nil)
	h.publishIssueAuxChanged(r, issue, "member", requestUserID(r))
	writeJSON(w, http.StatusOK, map[string]any{"run": h.pipelineRunToResponse(r.Context(), done)})
}
