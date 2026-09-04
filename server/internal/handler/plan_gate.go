package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Plan Gate (K11): approving a plan is what turns its steps into sub-issues.
// Each step becomes a child issue of the planned issue; `after` edges become
// blocking dependencies and decide the child's stage, so the existing staged
// barrier (issue_child_done.go) sequences the work: stage 1 starts in todo,
// later stages wait in backlog until the parent's assignee promotes them.

const (
	ErrCodePlanAlreadyMaterialized = "already_materialized"
	ErrCodePlanSuperseded          = "plan_superseded"
	ErrCodePlanHasNoSteps          = "plan_has_no_steps"

	planApproveOptionID = "approve"
	planReviseOptionID  = "revise"
	planMaxSteps        = 50
)

// planStages assigns each step 1 + the deepest stage among the steps it comes
// after. It rejects an unknown or cyclic `after` reference with the offending
// step id, so a plan is refused at publish time rather than at approval.
func planStages(steps []IssuePlanStep) (map[string]int32, error) {
	byID := map[string]IssuePlanStep{}
	for _, s := range steps {
		byID[s.ID] = s
	}
	stages := map[string]int32{}
	var visiting map[string]bool
	var stageOf func(id string, depth int) (int32, error)
	stageOf = func(id string, depth int) (int32, error) {
		if st, ok := stages[id]; ok {
			return st, nil
		}
		if visiting[id] || depth > len(steps) {
			return 0, fmt.Errorf("step %q is part of a cycle", id)
		}
		visiting[id] = true
		var st int32 = 1
		for _, dep := range byID[id].After {
			if _, ok := byID[dep]; !ok {
				return 0, fmt.Errorf("step %q comes after unknown step %q", id, dep)
			}
			if dep == id {
				return 0, fmt.Errorf("step %q cannot come after itself", id)
			}
			ds, err := stageOf(dep, depth+1)
			if err != nil {
				return 0, err
			}
			if ds+1 > st {
				st = ds + 1
			}
		}
		delete(visiting, id)
		stages[id] = st
		return st, nil
	}
	visiting = map[string]bool{}
	for _, s := range steps {
		if _, err := stageOf(s.ID, 0); err != nil {
			return nil, err
		}
	}
	return stages, nil
}

// normalizePlanSteps trims, fills missing ids positionally, checks uniqueness
// and the `after` graph. Assignee pairs are only shape-checked here; whether
// they exist is settled at materialization, where an unknown one is dropped.
func normalizePlanSteps(steps []IssuePlanStep) ([]IssuePlanStep, error) {
	if len(steps) > planMaxSteps {
		return nil, fmt.Errorf("at most %d steps", planMaxSteps)
	}
	seen := map[string]bool{}
	out := make([]IssuePlanStep, 0, len(steps))
	for i, s := range steps {
		s.ID = strings.TrimSpace(s.ID)
		s.Title = strings.TrimSpace(s.Title)
		s.AssigneeType = strings.TrimSpace(s.AssigneeType)
		s.AssigneeID = strings.TrimSpace(s.AssigneeID)
		s.IssueID = ""
		if s.Title == "" {
			return nil, fmt.Errorf("steps[%d].title is required", i)
		}
		if s.ID == "" {
			s.ID = fmt.Sprintf("s%d", i+1)
		}
		if seen[s.ID] {
			return nil, fmt.Errorf("step id %q is used twice", s.ID)
		}
		seen[s.ID] = true
		if (s.AssigneeType == "") != (s.AssigneeID == "") {
			return nil, fmt.Errorf("steps[%d]: assignee_type and assignee_id go together", i)
		}
		out = append(out, s)
	}
	if _, err := planStages(out); err != nil {
		return nil, err
	}
	return out, nil
}

func parsePlanSteps(raw []byte) []IssuePlanStep {
	var steps []IssuePlanStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		return nil
	}
	return steps
}

// planMaterializationError is a refusal the client can key on.
type planMaterializationError struct {
	status int
	code   string
	msg    string
}

func (e *planMaterializationError) Error() string { return e.msg }

// materializePlan claims the plan version, creates one child issue per step
// (stage from its `after` edges; stage 1 in todo, later stages parked in
// backlog), links blocking dependencies, and writes each child's id back on
// its step. A failure after the claim deletes what was created and releases
// the claim, so the plan reads as never approved.
// ponytail: compensation instead of one transaction — IssueService.Create
// commits and enqueues per issue; a shared tx would leak half-made runs.
func (h *Handler) materializePlan(ctx context.Context, r *http.Request, issue db.Issue, plan db.IssuePlan, actorType, actorID string) ([]db.Issue, db.IssuePlan, error) {
	steps := parsePlanSteps(plan.Steps)
	if len(steps) == 0 {
		return nil, plan, &planMaterializationError{http.StatusBadRequest, ErrCodePlanHasNoSteps, "this plan has no structured steps to create sub-issues from"}
	}
	stages, err := planStages(steps)
	if err != nil {
		return nil, plan, &planMaterializationError{http.StatusBadRequest, "invalid_plan_steps", err.Error()}
	}
	if plan.SupersededAt.Valid {
		return nil, plan, &planMaterializationError{http.StatusConflict, ErrCodePlanSuperseded, "a newer plan version supersedes this one; approve the active version"}
	}
	claimed, err := h.Queries.ClaimIssuePlanMaterialization(ctx, plan.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		if plan.MaterializedAt.Valid {
			return nil, plan, &planMaterializationError{http.StatusConflict, ErrCodePlanAlreadyMaterialized, "this plan version already created its sub-issues"}
		}
		return nil, plan, &planMaterializationError{http.StatusConflict, ErrCodePlanSuperseded, "a newer plan version supersedes this one; approve the active version"}
	}
	if err != nil {
		return nil, plan, fmt.Errorf("claim plan: %w", err)
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	fill := h.newStatusCategoryFiller(ctx, issue.WorkspaceID)
	platform, _, _ := middleware.ClientMetadataFromContext(ctx)
	created := make([]db.Issue, 0, len(steps))
	byStep := map[string]db.Issue{}
	rollback := func() {
		for _, c := range created {
			if err := h.Queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: c.ID, WorkspaceID: c.WorkspaceID}); err != nil {
				slog.Warn("plan gate: rollback delete failed", "issue_id", uuidToString(c.ID), "error", err)
			}
		}
		if err := h.Queries.ReleaseIssuePlanMaterialization(ctx, plan.ID); err != nil {
			slog.Warn("plan gate: release claim failed", "plan_id", uuidToString(plan.ID), "error", err)
		}
	}
	for i, step := range steps {
		assigneeType, assigneeID := pgtype.Text{}, pgtype.UUID{}
		if step.AssigneeType != "" {
			id, err := util.ParseUUID(step.AssigneeID)
			candidateType := pgtype.Text{String: step.AssigneeType, Valid: true}
			if err == nil {
				if status, _ := h.validateAssigneePair(ctx, r, workspaceID, candidateType, id); status == 0 {
					assigneeType, assigneeID = candidateType, id
				}
			}
			if !assigneeID.Valid {
				// Criterion 5: an unknown suggested assignee is dropped, not fatal.
				slog.Info("plan gate: suggested assignee ignored", "step", step.ID, "assignee_type", step.AssigneeType, "assignee_id", step.AssigneeID)
			}
		}
		status := "todo"
		if stages[step.ID] > 1 {
			status = "backlog"
		}
		res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
			WorkspaceID:    issue.WorkspaceID,
			Title:          step.Title,
			Description:    pgtype.Text{String: fmt.Sprintf("Step %d of plan v%d for %s-%d.", i+1, plan.Version, prefix, issue.Number), Valid: true},
			Status:         status,
			Priority:       issue.Priority,
			AssigneeType:   assigneeType,
			AssigneeID:     assigneeID,
			CreatorType:    actorType,
			CreatorID:      issue.CreatorID,
			ParentIssueID:  issue.ID,
			Stage:          pgtype.Int4{Int32: stages[step.ID], Valid: true},
			AllowDuplicate: true,
		}, service.IssueCreateOpts{
			ActorID:  actorID,
			Platform: platform,
			BroadcastPayload: func(child db.Issue, _ []db.Attachment, _ []db.IssueLabel) map[string]any {
				payload := issueToResponse(child, prefix)
				fill(&payload)
				return map[string]any{"issue": payload}
			},
		})
		if err != nil {
			rollback()
			return nil, plan, fmt.Errorf("create sub-issue for step %q: %w", step.ID, err)
		}
		created = append(created, res.Issue)
		byStep[step.ID] = res.Issue
		steps[i].IssueID = uuidToString(res.Issue.ID)
	}
	// Blocking edges: a step's predecessor blocks it.
	var touched []pgtype.UUID
	for _, step := range steps {
		for _, dep := range step.After {
			if _, err := h.Queries.CreateIssueDependency(ctx, db.CreateIssueDependencyParams{
				IssueID:          byStep[dep].ID,
				DependsOnIssueID: byStep[step.ID].ID,
				Type:             dependencyBlocks,
			}); err != nil {
				rollback()
				return nil, plan, fmt.Errorf("link step %q after %q: %w", step.ID, dep, err)
			}
			touched = append(touched, byStep[dep].ID, byStep[step.ID].ID)
		}
	}
	if len(touched) > 0 {
		h.publishIssueDependencyChange(r, actorType, actorID, issue.WorkspaceID, touched...)
	}
	stepsJSON, _ := json.Marshal(steps)
	updated, err := h.Queries.SetIssuePlanSteps(ctx, db.SetIssuePlanStepsParams{ID: plan.ID, Steps: stepsJSON})
	if err != nil {
		slog.Warn("plan gate: write step issue ids failed", "plan_id", uuidToString(plan.ID), "error", err)
		updated = claimed
	}
	return created, updated, nil
}

func (h *Handler) writePlanMaterializationError(w http.ResponseWriter, r *http.Request, err error) {
	var pe *planMaterializationError
	if errors.As(err, &pe) {
		writeErrorCode(w, pe.status, pe.code, pe.msg)
		return
	}
	slog.Warn("plan materialization failed", append(logger.RequestAttrs(r), "error", err)...)
	writeError(w, http.StatusInternalServerError, "failed to create sub-issues from the plan")
}

// MaterializeIssuePlan — POST /api/issues/{id}/plan/{version}/materialize.
// Human-only (router): the gate exists so a human approves before work fans out.
func (h *Handler) MaterializeIssuePlan(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	version, err := strconv.Atoi(chi.URLParam(r, "version"))
	if err != nil || version <= 0 {
		writeError(w, http.StatusBadRequest, "invalid plan version")
		return
	}
	plan, err := h.Queries.GetIssuePlanVersion(r.Context(), db.GetIssuePlanVersionParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, Version: int32(version)})
	if err != nil {
		writeError(w, http.StatusNotFound, "plan version not found")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	children, updated, err := h.materializePlan(r.Context(), r, issue, plan, actorType, actorID)
	if err != nil {
		h.writePlanMaterializationError(w, r, err)
		return
	}
	h.publishIssueAuxChanged(r, issue, actorType, actorID)
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), actorType, actorID, map[string]any{
		"issue": issueToResponse(issue, h.getIssuePrefix(r.Context(), issue.WorkspaceID)), "children_changed": true,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"plan":   issuePlanToResponse(updated),
		"issues": h.issuesToResponses(r.Context(), issue.WorkspaceID, children),
	})
}

func (h *Handler) issuesToResponses(ctx context.Context, wsID pgtype.UUID, issues []db.Issue) []IssueResponse {
	prefix := h.getIssuePrefix(ctx, wsID)
	fill := h.newStatusCategoryFiller(ctx, wsID)
	out := make([]IssueResponse, 0, len(issues))
	for _, i := range issues {
		resp := issueToResponse(i, prefix)
		fill(&resp)
		out = append(out, resp)
	}
	return out
}

// askPlanApproval files the Decision Card a human answers to approve a plan
// an agent published (K01 → K11). Best effort: the plan exists either way.
func (h *Handler) askPlanApproval(ctx context.Context, r *http.Request, issue db.Issue, plan db.IssuePlan, stepCount int, actorType, actorID string) {
	options, _ := json.Marshal([]DecisionOption{
		{ID: planApproveOptionID, Label: fmt.Sprintf("Approve and create %d sub-issues", stepCount)},
		{ID: planReviseOptionID, Label: "Ask for changes", Impact: "the agent revises the plan before anything is created"},
	})
	var taskID pgtype.UUID
	if task, ok := h.taskFromRequestHeader(r); ok && task.IssueID == issue.ID {
		taskID = task.ID
	}
	decision, err := h.Queries.CreateIssueDecision(ctx, db.CreateIssueDecisionParams{
		WorkspaceID:         issue.WorkspaceID,
		IssueID:             issue.ID,
		TaskID:              taskID,
		AskedByType:         actorType,
		AskedByID:           parseUUID(actorID),
		Question:            fmt.Sprintf("Approve plan v%d (%d steps) for «%s»?", plan.Version, stepCount, issue.Title),
		Options:             options,
		RecommendedOptionID: pgtype.Text{String: planApproveOptionID, Valid: true},
		Urgency:             "normal",
		PlanVersion:         pgtype.Int4{Int32: plan.Version, Valid: true},
		SlaDeadlineAt:       h.decisionDeadline(ctx, issue.WorkspaceID),
	})
	if err != nil {
		slog.Warn("plan gate: file approval card failed", "error", err, "issue_id", uuidToString(issue.ID))
		return
	}
	h.notifyDecisionRequested(ctx, issue, decision, actorType, actorID)
}

// planMaterializationNote tells the resumed run what approval produced.
func planMaterializationNote(prefix string, plan db.IssuePlan, children []db.Issue) string {
	refs := make([]string, 0, len(children))
	for _, c := range children {
		refs = append(refs, fmt.Sprintf("%s-%d", prefix, c.Number))
	}
	return fmt.Sprintf("Plan v%d was approved and its %d steps now exist as sub-issues (%s). Stage 1 is in todo; later stages wait in backlog and are promoted as each stage closes. Do not recreate them; coordinate through them.",
		plan.Version, len(children), strings.Join(refs, ", "))
}
