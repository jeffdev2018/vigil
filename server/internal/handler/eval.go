package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Eval Lab (K24). An issue whose acceptance criteria are all proved (K12)
// becomes a reusable evaluation case: its statement and its reference proofs
// are snapshotted. A suite groups cases; running a suite replays every case
// against one pinned agent version (K23) in a throwaway issue confined to a
// container sandbox (K10), and scores the version on how many criteria the
// agent managed to prove again. The source issue is never touched, and the
// throwaway issues are cancelled once settled so they leave the board.

const (
	ErrCodeEvalCaseNeedsProofs = "eval_case_needs_proofs"
	ErrCodeEvalRunActive       = "eval_run_active"

	AuditEvalCasePromoted = "eval.case_promoted"
	AuditEvalRunStarted   = "eval.run_started"
	AuditEvalRunFinished  = "eval.run_finished"

	evalMaxSuiteName  = 200
	evalMaxSuiteCases = 100
	evalMaxDetailLen  = 200

	// evalBrief rides on every throwaway issue. The agent is told the run is
	// an evaluation replay and that proof is the only thing measured: a
	// criterion left unproved counts as failed even when the work was done.
	evalBrief = "EVALUATION REPLAY. This is a throwaway issue in a disposable sandbox, created to evaluate you against a reference case. Do the work described above. Then prove EVERY acceptance criterion of this issue with the criteria proof endpoint (PATCH /api/issues/{id}/acceptance-criteria/{criterionId}/proof, or the `multica` CLI as documented in the working-on-issues skill). Do not touch any other issue. A criterion left without proof counts as failed, however good the work was."
)

type EvalCaseResponse struct {
	ID                string                `json:"id"`
	WorkspaceID       string                `json:"workspace_id"`
	SourceIssueID     string                `json:"source_issue_id"`
	SourceIssueNumber int32                 `json:"source_issue_number"`
	Title             string                `json:"title"`
	Description       string                `json:"description"`
	Criteria          []AcceptanceCriterion `json:"criteria"`
	CreatedBy         *string               `json:"created_by"`
	CreatedAt         string                `json:"created_at"`
}

type EvalSuiteResponse struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	Name        string   `json:"name"`
	CaseIDs     []string `json:"case_ids"`
	CaseCount   int      `json:"case_count"`
	CreatedBy   *string  `json:"created_by"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

type EvalRunCaseResponse struct {
	CaseID    string  `json:"case_id"`
	CaseTitle string  `json:"case_title"`
	IssueID   string  `json:"issue_id"`
	TaskID    string  `json:"task_id"`
	Status    string  `json:"status"`
	Score     *int32  `json:"score"`
	Detail    string  `json:"detail"`
	SettledAt *string `json:"settled_at"`
}

type EvalRunResponse struct {
	ID                 string                `json:"id"`
	WorkspaceID        string                `json:"workspace_id"`
	SuiteID            string                `json:"suite_id"`
	SuiteName          string                `json:"suite_name"`
	AgentID            string                `json:"agent_id"`
	AgentVersionID     *string               `json:"agent_version_id"`
	AgentVersionNumber int32                 `json:"agent_version_number"`
	Status             string                `json:"status"`
	Score              *int32                `json:"score"`
	StartedBy          *string               `json:"started_by"`
	StartedAt          string                `json:"started_at"`
	CompletedAt        *string               `json:"completed_at"`
	Cases              []EvalRunCaseResponse `json:"cases"`
}

func evalCaseToResponse(c db.EvalCase) EvalCaseResponse {
	return EvalCaseResponse{
		ID: uuidToString(c.ID), WorkspaceID: uuidToString(c.WorkspaceID), SourceIssueID: uuidToString(c.SourceIssueID),
		SourceIssueNumber: c.SourceIssueNumber, Title: c.Title, Description: c.Description,
		Criteria: parseAcceptanceCriteria(c.Criteria), CreatedBy: uuidToPtr(c.CreatedBy), CreatedAt: timestampToString(c.CreatedAt),
	}
}

// evalCaseIDs reads the suite's stored order defensively: a malformed column
// reads as an empty suite, never as an arbitrary set of cases.
func evalCaseIDs(raw []byte) []string {
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		slog.Warn("eval: unreadable suite case_ids, treating as empty", "error", err)
		return []string{}
	}
	if ids == nil {
		ids = []string{}
	}
	return ids
}

func evalSuiteToResponse(s db.EvalSuite) EvalSuiteResponse {
	ids := evalCaseIDs(s.CaseIds)
	return EvalSuiteResponse{
		ID: uuidToString(s.ID), WorkspaceID: uuidToString(s.WorkspaceID), Name: s.Name, CaseIDs: ids, CaseCount: len(ids),
		CreatedBy: uuidToPtr(s.CreatedBy), CreatedAt: timestampToString(s.CreatedAt), UpdatedAt: timestampToString(s.UpdatedAt),
	}
}

// evalRunToResponse fills the suite name and version number with one read
// each; both are display-only, so a missing row degrades to empty rather than
// failing the response.
func (h *Handler) evalRunToResponse(ctx context.Context, run db.EvalRun) EvalRunResponse {
	out := EvalRunResponse{
		ID: uuidToString(run.ID), WorkspaceID: uuidToString(run.WorkspaceID), SuiteID: uuidToString(run.SuiteID),
		AgentID: uuidToString(run.AgentID), AgentVersionID: uuidToPtr(run.AgentVersionID), Status: run.Status,
		Score: int4ToPtr(run.Score), StartedBy: uuidToPtr(run.StartedBy), StartedAt: timestampToString(run.StartedAt),
		CompletedAt: timestampToPtr(run.CompletedAt), Cases: []EvalRunCaseResponse{},
	}
	if suite, err := h.Queries.GetEvalSuite(ctx, run.SuiteID); err == nil {
		out.SuiteName = suite.Name
	}
	if run.AgentVersionID.Valid {
		if version, err := h.Queries.GetAgentVersion(ctx, db.GetAgentVersionParams{ID: run.AgentVersionID, AgentID: run.AgentID}); err == nil {
			out.AgentVersionNumber = version.VersionNumber
		}
	}
	rows, err := h.Queries.ListEvalRunCases(ctx, run.ID)
	if err != nil {
		slog.Warn("eval: list run cases failed", "run_id", uuidToString(run.ID), "error", err)
		return out
	}
	for _, rc := range rows {
		out.Cases = append(out.Cases, EvalRunCaseResponse{
			CaseID: uuidToString(rc.CaseID), CaseTitle: rc.CaseTitle.String, IssueID: uuidToString(rc.IssueID), TaskID: uuidToString(rc.TaskID),
			Status: rc.Status, Score: int4ToPtr(rc.Score), Detail: rc.Detail, SettledAt: timestampToPtr(rc.SettledAt),
		})
	}
	return out
}

// PromoteIssueToEvalCase — POST /api/issues/{id}/promote-to-eval-case.
// The proofs are what makes a case reusable: an issue with an unproved
// criterion has no reference answer to measure a replay against.
func (h *Handler) PromoteIssueToEvalCase(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	criteria := parseAcceptanceCriteria(issue.AcceptanceCriteria)
	unsatisfied := unsatisfiedAcceptanceCriteria(issue.AcceptanceCriteria)
	if len(criteria) == 0 || len(unsatisfied) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"code":     ErrCodeEvalCaseNeedsProofs,
			"error":    "an eval case needs acceptance criteria with a satisfied proof on every one of them",
			"criteria": unsatisfied,
		})
		return
	}
	// The snapshot is the stored column, proofs included: it is the reference
	// answer a replay is measured against, and it must not drift when the
	// source issue is later edited.
	created, err := h.Queries.CreateEvalCase(r.Context(), db.CreateEvalCaseParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, SourceIssueID: issue.ID, SourceIssueNumber: issue.Number,
		Title: issue.Title, Description: issue.Description.String, Criteria: issue.AcceptanceCriteria, CreatedBy: parseUUID(userID),
	})
	if err != nil {
		slog.Warn("eval: create case failed", "issue_id", uuidToString(issue.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create the eval case")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	h.audit(r.Context(), issue.WorkspaceID, actorType, actorID, AuditEvalCasePromoted, "eval_case", created.ID, map[string]any{
		"issue_id": uuidToString(issue.ID), "issue_number": issue.Number, "criteria": len(criteria),
	}, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"case": evalCaseToResponse(created)})
}

// ListEvalCases — GET /api/workspaces/{id}/eval-cases.
func (h *Handler) ListEvalCases(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.evalWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListEvalCases(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list eval cases")
		return
	}
	cases := make([]EvalCaseResponse, 0, len(rows))
	for _, c := range rows {
		cases = append(cases, evalCaseToResponse(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": cases})
}

// ListEvalSuites — GET /api/workspaces/{id}/eval-suites.
func (h *Handler) ListEvalSuites(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.evalWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListEvalSuites(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list eval suites")
		return
	}
	suites := make([]EvalSuiteResponse, 0, len(rows))
	for _, s := range rows {
		suites = append(suites, evalSuiteToResponse(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"suites": suites})
}

// CreateEvalSuite — POST /api/workspaces/{id}/eval-suites.
func (h *Handler) CreateEvalSuite(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.evalWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name    string   `json:"name"`
		CaseIDs []string `json:"case_ids"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > evalMaxSuiteName {
		writeError(w, http.StatusBadRequest, "a suite needs a name of at most 200 characters")
		return
	}
	if len(req.CaseIDs) == 0 || len(req.CaseIDs) > evalMaxSuiteCases {
		writeError(w, http.StatusBadRequest, "a suite needs between 1 and 100 cases")
		return
	}
	ids := make([]pgtype.UUID, 0, len(req.CaseIDs))
	for _, raw := range req.CaseIDs {
		id, ok := parseUUIDOrBadRequest(w, raw, "case id")
		if !ok {
			return
		}
		ids = append(ids, id)
	}
	found, err := h.Queries.GetEvalCasesByIDs(r.Context(), db.GetEvalCasesByIDsParams{WorkspaceID: wsUUID, Ids: ids})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the eval cases")
		return
	}
	// Count against the DISTINCT requested ids: a duplicate in the body must
	// not make an unknown id look present.
	distinct := map[string]bool{}
	for _, raw := range req.CaseIDs {
		distinct[strings.TrimSpace(raw)] = true
	}
	if len(found) != len(distinct) {
		writeError(w, http.StatusBadRequest, "one of the case ids does not exist in this workspace")
		return
	}
	raw, _ := json.Marshal(req.CaseIDs)
	suite, err := h.Queries.CreateEvalSuite(r.Context(), db.CreateEvalSuiteParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, Name: name, CaseIds: raw, CreatedBy: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create the eval suite")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"suite": evalSuiteToResponse(suite)})
}

// ListEvalRuns — GET /api/workspaces/{id}/eval-runs.
func (h *Handler) ListEvalRuns(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.evalWorkspaceFromURL(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListEvalRuns(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list eval runs")
		return
	}
	runs := make([]EvalRunResponse, 0, len(rows))
	for _, run := range rows {
		runs = append(runs, h.evalRunToResponse(r.Context(), run))
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

// evalWorkspaceFromURL resolves the workspace of a workspace-scoped eval
// route and checks the requester's membership, like ListModelKeys does.
func (h *Handler) evalWorkspaceFromURL(w http.ResponseWriter, r *http.Request) (pgtype.UUID, bool) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return pgtype.UUID{}, false
	}
	return wsUUID, true
}

// GetEvalRun — GET /api/eval-runs/{id}.
func (h *Handler) GetEvalRun(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "run id")
	if !ok {
		return
	}
	run, err := h.Queries.GetEvalRun(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "eval run not found")
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(run.WorkspaceID)); !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"run": h.evalRunToResponse(r.Context(), run)})
}

// RunEvalSuite — POST /api/eval-suites/{id}/run. One throwaway issue per
// case, assigned to the agent, carrying the case's criteria WITHOUT their
// proofs: re-proving them is the exam.
func (h *Handler) RunEvalSuite(w http.ResponseWriter, r *http.Request) {
	suiteID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "suite id")
	if !ok {
		return
	}
	suite, err := h.Queries.GetEvalSuite(r.Context(), suiteID)
	if err != nil {
		writeError(w, http.StatusNotFound, "eval suite not found")
		return
	}
	if _, ok := h.workspaceMember(w, r, uuidToString(suite.WorkspaceID)); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		AgentID        string `json:"agent_id"`
		AgentVersionID string `json:"agent_version_id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentUUID, WorkspaceID: suite.WorkspaceID})
	if err != nil || agent.ArchivedAt.Valid {
		writeError(w, http.StatusUnprocessableEntity, "agent not found in this workspace")
		return
	}
	versionUUID, ok := parseUUIDOrBadRequest(w, req.AgentVersionID, "agent version id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetAgentVersion(r.Context(), db.GetAgentVersionParams{ID: versionUUID, AgentID: agent.ID}); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "this version does not belong to that agent")
		return
	}
	if active, err := h.Queries.HasRunningEvalRunForSuite(r.Context(), suite.ID); err == nil && active {
		writeErrorCode(w, http.StatusConflict, ErrCodeEvalRunActive, "a run is already in progress on this suite")
		return
	}
	ids := evalCaseIDs(suite.CaseIds)
	caseUUIDs := make([]pgtype.UUID, 0, len(ids))
	for _, raw := range ids {
		if id, err := util.ParseUUID(raw); err == nil {
			caseUUIDs = append(caseUUIDs, id)
		}
	}
	cases, err := h.Queries.GetEvalCasesByIDs(r.Context(), db.GetEvalCasesByIDsParams{WorkspaceID: suite.WorkspaceID, Ids: caseUUIDs})
	if err != nil || len(cases) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "this suite has no case left to run")
		return
	}
	byID := map[string]db.EvalCase{}
	for _, c := range cases {
		byID[uuidToString(c.ID)] = c
	}
	run, err := h.Queries.CreateEvalRun(r.Context(), db.CreateEvalRunParams{
		ID: dbid.NewV7(), WorkspaceID: suite.WorkspaceID, SuiteID: suite.ID, AgentID: agent.ID, AgentVersionID: versionUUID, StartedBy: parseUUID(userID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the eval run")
		return
	}
	enqueued := 0
	for _, id := range ids { // keep the suite's own order
		c, found := byID[id]
		if !found {
			continue
		}
		issueID, taskID, detail := h.startEvalCase(r, run, c, agent, userID)
		if _, err := h.Queries.CreateEvalRunCase(r.Context(), db.CreateEvalRunCaseParams{RunID: run.ID, CaseID: c.ID, IssueID: issueID, TaskID: taskID}); err != nil {
			slog.Warn("eval: record run case failed", "run_id", uuidToString(run.ID), "case_id", uuidToString(c.ID), "error", err)
			continue
		}
		if detail == "" {
			enqueued++
			continue
		}
		if _, err := h.Queries.SettleEvalRunCase(r.Context(), db.SettleEvalRunCaseParams{
			RunID: run.ID, CaseID: c.ID, Status: "infra_failed", Detail: detail, TaskID: taskID,
		}); err != nil {
			slog.Warn("eval: settle infra failure failed", "run_id", uuidToString(run.ID), "error", err)
		}
	}
	if enqueued == 0 {
		// Nothing runs: the verdict is known now, and leaving the run
		// "running" would block the suite forever.
		if finished, err := h.Queries.FinishEvalRun(r.Context(), db.FinishEvalRunParams{ID: run.ID, Status: "failed"}); err == nil {
			run = finished
		}
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(suite.WorkspaceID))
	h.audit(r.Context(), suite.WorkspaceID, actorType, actorID, AuditEvalRunStarted, "eval_run", run.ID, map[string]any{
		"suite_id": uuidToString(suite.ID), "agent_id": uuidToString(agent.ID), "agent_version_id": uuidToString(versionUUID), "cases": len(ids), "enqueued": enqueued,
	}, nil)
	writeJSON(w, http.StatusAccepted, map[string]any{"run": h.evalRunToResponse(r.Context(), run)})
}

// startEvalCase creates the throwaway issue for one case and returns it with
// the id of the run it enqueued. A non-empty detail means nothing runs for
// this case and the caller settles it as an infrastructure failure.
func (h *Handler) startEvalCase(r *http.Request, run db.EvalRun, c db.EvalCase, agent db.Agent, userID string) (issueID, taskID pgtype.UUID, detail string) {
	status, ok := h.resolveEvalIssueStatus(r.Context(), run.WorkspaceID)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, "no usable issue status in this workspace"
	}
	res, err := h.IssueService.Create(r.Context(), service.IssueCreateParams{
		WorkspaceID:    run.WorkspaceID,
		Title:          "[Eval] " + c.Title,
		Description:    strToText(strings.TrimSpace(c.Description) + "\n\n" + evalBrief),
		Status:         status,
		Priority:       "none",
		AssigneeType:   strToText("agent"),
		AssigneeID:     agent.ID,
		CreatorType:    "member",
		CreatorID:      parseUUID(userID),
		OriginType:     strToText("eval_run"),
		OriginID:       run.ID,
		AllowDuplicate: true,
	}, service.IssueCreateOpts{ActorID: userID})
	if err != nil {
		slog.Warn("eval: create throwaway issue failed", "run_id", uuidToString(run.ID), "case_id", uuidToString(c.ID), "error", err)
		return pgtype.UUID{}, pgtype.UUID{}, "could not create the replay issue"
	}
	// The criteria go on WITHOUT their proofs: re-proving them is the exam.
	stripped := make([]AcceptanceCriterion, 0, 8)
	for _, ref := range parseAcceptanceCriteria(c.Criteria) {
		stripped = append(stripped, AcceptanceCriterion{ID: ref.ID, Text: ref.Text, ProofState: ProofStateMissing})
	}
	raw, err := json.Marshal(stripped)
	if err == nil {
		if _, err := h.Queries.UpdateIssueAcceptanceCriteria(r.Context(), db.UpdateIssueAcceptanceCriteriaParams{ID: res.Issue.ID, AcceptanceCriteria: raw}); err != nil {
			slog.Warn("eval: write replay criteria failed", "issue_id", uuidToString(res.Issue.ID), "error", err)
		}
	}
	if !res.AssignedTaskID.Valid {
		return res.Issue.ID, pgtype.UUID{}, "no run enqueued"
	}
	return res.Issue.ID, res.AssignedTaskID, ""
}

// resolveEvalIssueStatus picks the status a replay issue starts in: todo when
// the workspace still has it, else the first active key.
func (h *Handler) resolveEvalIssueStatus(ctx context.Context, wsID pgtype.UUID) (string, bool) {
	if _, err := issuestatus.Resolve(ctx, h.Queries, wsID, issuestatus.Todo); err == nil {
		return issuestatus.Todo, true
	}
	keys, err := issuestatus.ActiveKeys(ctx, h.Queries, wsID)
	if err != nil || len(keys) == 0 {
		return "", false
	}
	return keys[0], true
}

// evalRunCaseForTask answers whether a run belongs to an eval case, keyed on
// the run itself or the root of its retry chain (same rule as the duel
// barrier).
func (h *Handler) evalRunCaseForTask(ctx context.Context, task db.AgentTaskQueue) (db.GetEvalRunCaseByTaskRow, bool) {
	root := task.ID
	if task.RetryOfTaskID.Valid {
		root = task.RetryOfTaskID
	} else if task.ParentTaskID.Valid {
		root = task.ParentTaskID
	}
	row, err := h.Queries.GetEvalRunCaseByTask(ctx, db.GetEvalRunCaseByTaskParams{TaskID: task.ID, RootTaskID: root})
	if err != nil {
		return db.GetEvalRunCaseByTaskRow{}, false
	}
	return row, true
}

// applyEvalAgentVersion pins the run to the version being evaluated: the
// scores are meaningless if the agent's instructions changed between the run
// and the version under test. Skills are NOT pinned — the version stores skill
// ids, but the claim resolves skills from the agent's current junction rows.
func (h *Handler) applyEvalAgentVersion(ctx context.Context, row db.GetEvalRunCaseByTaskRow, task db.AgentTaskQueue, resp *AgentTaskResponse) {
	if resp.Agent == nil || !row.AgentVersionID.Valid {
		return
	}
	version, err := h.Queries.GetAgentVersion(ctx, db.GetAgentVersionParams{ID: row.AgentVersionID, AgentID: task.AgentID})
	if err != nil {
		slog.Warn("eval: pinned version not found; running the agent as configured",
			"task_id", uuidToString(task.ID), "agent_version_id", uuidToString(row.AgentVersionID), "error", err)
		return
	}
	resp.Agent.Instructions = version.Instructions
	if version.Model != "" {
		resp.Agent.Model = version.Model
	}
}

// evalStartGate refuses to let an eval run start outside a container: an
// evaluation replay writes throwaway work, and a run that escaped its
// sandbox would do it on the real machine. The case is settled as an
// infrastructure failure rather than a bad score — the version was never
// measured.
func (h *Handler) evalStartGate(ctx context.Context, task db.AgentTaskQueue, req StartTaskRequest) bool {
	row, ok := h.evalRunCaseForTask(ctx, task)
	if !ok || nonEmptySandboxMode(req.SandboxMode) == "container" {
		return false
	}
	reason := strings.TrimSpace(req.SandboxReason)
	if reason == "" {
		reason = nonEmptySandboxMode(req.SandboxMode)
	}
	if _, err := h.TaskService.CancelTaskWithReason(ctx, task.ID, "eval run requires a container sandbox", "sandbox_unavailable"); err != nil {
		slog.Warn("eval: cancel unsandboxed run failed", "task_id", uuidToString(task.ID), "error", err)
	}
	h.settleEvalCase(ctx, row, "infra_failed", pgtype.Int4{}, "sandbox unavailable: "+reason, task.ID)
	return true
}

// settleEvalRunCase is called when a run reaches a terminal status: the score
// is how many of the case's criteria the agent managed to prove again.
func (h *Handler) settleEvalRunCase(ctx context.Context, task db.AgentTaskQueue) {
	row, ok := h.evalRunCaseForTask(ctx, task)
	if !ok {
		return
	}
	var (
		status string
		score  pgtype.Int4
		detail string
	)
	switch task.Status {
	case "completed":
		criteria := parseAcceptanceCriteria(h.evalIssueCriteria(ctx, row.IssueID))
		satisfied := 0
		for _, c := range criteria {
			if c.ProofState == ProofStateSatisfied {
				satisfied++
			}
		}
		total := len(criteria)
		if total == 0 {
			// A replay issue with no criteria cannot be measured; scoring it
			// 100 would reward a case whose criteria never landed.
			status, score, detail = "failed", pgtype.Int4{Int32: 0, Valid: true}, "no criteria on the replay issue"
			break
		}
		score = pgtype.Int4{Int32: int32((satisfied*200 + total) / (total * 2)), Valid: true}
		status = "failed"
		if satisfied == total {
			status = "passed"
		}
		detail = strconv.Itoa(satisfied) + "/" + strconv.Itoa(total) + " criteria proved"
	case "failed", "cancelled":
		if more, err := h.Queries.HasRunnableSuccessorForTask(ctx, task.ID); err == nil && more {
			return // a retry is coming; not final yet
		}
		status, score = "failed", pgtype.Int4{Int32: 0, Valid: true}
		detail = truncate("run failed: "+task.Error.String, evalMaxDetailLen)
	default:
		return
	}
	h.settleEvalCase(ctx, row, status, score, detail, task.ID)
}

func (h *Handler) evalIssueCriteria(ctx context.Context, issueID pgtype.UUID) []byte {
	issue, err := h.Queries.GetIssue(ctx, issueID)
	if err != nil {
		slog.Warn("eval: replay issue not found", "issue_id", uuidToString(issueID), "error", err)
		return nil
	}
	return issue.AcceptanceCriteria
}

// settleEvalCase writes one case's verdict, retires its throwaway issue and
// finalizes the run when it was the last pending case.
func (h *Handler) settleEvalCase(ctx context.Context, row db.GetEvalRunCaseByTaskRow, status string, score pgtype.Int4, detail string, taskID pgtype.UUID) {
	settled, err := h.Queries.SettleEvalRunCase(ctx, db.SettleEvalRunCaseParams{
		RunID: row.RunID, CaseID: row.CaseID, Status: status, Score: score, Detail: truncate(detail, evalMaxDetailLen), TaskID: taskID,
	})
	if err != nil {
		return // already settled, or gone
	}
	// The throwaway issue leaves the active board; best effort, the verdict is
	// already recorded.
	if _, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: settled.IssueID, Status: issuestatus.Cancelled, WorkspaceID: row.WorkspaceID}); err != nil {
		slog.Warn("eval: cancel replay issue failed", "issue_id", uuidToString(settled.IssueID), "error", err)
	}
	h.finalizeEvalRun(ctx, row.RunID)
}

// finalizeEvalRun closes a run once its last case settled: the score is the
// mean of the scored cases, and a run where every case failed on
// infrastructure is failed with no score — the version was never measured.
func (h *Handler) finalizeEvalRun(ctx context.Context, runID pgtype.UUID) {
	run, err := h.Queries.GetEvalRun(ctx, runID)
	if err != nil {
		return
	}
	defer h.publishEvalProgress(ctx, runID)
	pending, err := h.Queries.CountPendingEvalRunCases(ctx, runID)
	if err != nil || pending > 0 {
		return
	}
	cases, err := h.Queries.ListEvalRunCases(ctx, runID)
	if err != nil || len(cases) == 0 {
		return
	}
	status, sum, scored := "completed", int32(0), int32(0)
	allInfra := true
	for _, c := range cases {
		if c.Status != "infra_failed" {
			allInfra = false
		}
		if c.Score.Valid {
			sum += c.Score.Int32
			scored++
		}
	}
	score := pgtype.Int4{}
	if allInfra {
		status = "failed"
	} else if scored > 0 {
		score = pgtype.Int4{Int32: sum / scored, Valid: true}
	}
	finished, err := h.Queries.FinishEvalRun(ctx, db.FinishEvalRunParams{ID: runID, Status: status, Score: score})
	if err != nil {
		return // another settlement already closed it
	}
	h.audit(ctx, run.WorkspaceID, "system", "", AuditEvalRunFinished, "eval_run", runID, map[string]any{
		"suite_id": uuidToString(run.SuiteID), "agent_id": uuidToString(run.AgentID), "agent_version_id": uuidToString(run.AgentVersionID),
		"status": finished.Status, "score": int4ToPtr(finished.Score), "cases": len(cases),
	}, nil)
}

func (h *Handler) publishEvalProgress(ctx context.Context, runID pgtype.UUID) {
	run, err := h.Queries.GetEvalRun(ctx, runID)
	if err != nil {
		return
	}
	h.publish("eval:progress", uuidToString(run.WorkspaceID), "system", "", map[string]any{
		"run_id": uuidToString(run.ID), "suite_id": uuidToString(run.SuiteID), "status": run.Status,
	})
}
