package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Eval Lab (K24): a proved issue becomes a case, a suite of cases replays
// against one pinned agent version in throwaway sandboxed issues, and the run
// is scored on the criteria the agent managed to prove again.

type evalCaseEnvelope struct {
	Case EvalCaseResponse `json:"case"`
}

type evalSuiteEnvelope struct {
	Suite EvalSuiteResponse `json:"suite"`
}

type evalRunEnvelope struct {
	Run EvalRunResponse `json:"run"`
}

func evalWorkspaceCall(t *testing.T, h http.HandlerFunc, method, path string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, h, withURLParam(newRequest(method, path, body), "id", testWorkspaceID))
}

// evalCleanup removes the rows the eval flow creates outside the fixture:
// the throwaway issues and their runs, plus the four eval tables.
func evalCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		// NOT t.Context(): Go cancels it just before cleanups run, which made
		// every delete below a silent no-op and leaked eval rows into the next
		// test in this package.
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM eval_run_case WHERE run_id IN (SELECT id FROM eval_run WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM eval_run WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM eval_suite WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM eval_case WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1 AND origin_type = 'eval_run')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND origin_type = 'eval_run'`, testWorkspaceID)
	})
}

// promoteToEvalCase proves nothing by itself; the caller asserts the status.
func promoteToEvalCase(t *testing.T, issueID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.PromoteIssueToEvalCase,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issueID+"/promote-to-eval-case", nil), "id", issueID))
}

// evalProvedCase makes an issue with `texts` criteria, proves them all and
// promotes it, returning the new case id.
func evalProvedCase(t *testing.T, title string, texts ...string) string {
	t.Helper()
	issue := dbfx.Issue(t, title, testutil.Cols{"status": "in_review", "description": "Reference statement for " + title})
	// dbfx.Issue picks its number from MAX(number)+1 without moving the
	// workspace counter, which IssueService.Create reads: without this the
	// replay issue below collides on uq_issue_workspace_number.
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID)
	body := make([]map[string]any, 0, len(texts))
	for _, text := range texts {
		body = append(body, map[string]any{"text": text})
	}
	criteria := criteriaOf(t, setCriteria(t, issue, body))
	for _, c := range criteria {
		proveCriterion(t, issue, c.ID, map[string]any{"proof_type": "test", "proof_ref": "go test ./..."}).Want(http.StatusOK)
	}
	var out evalCaseEnvelope
	promoteToEvalCase(t, issue).Want(http.StatusCreated).JSON(&out)
	return out.Case.ID
}

// evalAgentWithVersion returns a runtime, an agent bound to it, and a pinned
// version of that agent carrying its own instructions and model.
func evalAgentWithVersion(t *testing.T, name, instructions, model string) (runtimeID, agentID, versionID string) {
	t.Helper()
	runtimeID = dbfx.Runtime(t, name+" runtime", testutil.Cols{
		"sandbox_mode": "none", "sandbox_image": "ghcr.io/acme/eval:1", "device_info": "eval fixture",
	})
	agentID = dbfx.Agent(t, name, runtimeID, testutil.Cols{"instructions": "current instructions", "model": "current-model"})
	versionID = dbfx.Insert(t, "agent_version", testutil.Cols{
		"workspace_id": testWorkspaceID, "agent_id": agentID, "version_number": 1,
		"instructions": instructions, "model": model,
		"skill_ids": testutil.Raw("'[]'::jsonb"), "tool_config": testutil.Raw("'{}'::jsonb"),
		"note": "", "created_by_type": "member", "created_by_id": testUserID,
	})
	return runtimeID, agentID, versionID
}

func evalRunSuite(t *testing.T, suiteID, agentID, versionID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.RunEvalSuite, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/eval-suites/"+suiteID+"/run", map[string]any{"agent_id": agentID, "agent_version_id": versionID}),
		"id", suiteID))
}

func TestEvalCasePromotionNeedsProvedCriteria(t *testing.T) {
	evalCleanup(t)
	issue := dbfx.Issue(t, "eval promotion", testutil.Cols{"status": "in_review", "description": "The statement"})

	// No criteria at all: nothing to measure a replay against.
	promoteToEvalCase(t, issue).Want(http.StatusConflict)

	criteria := criteriaOf(t, setCriteria(t, issue, []map[string]any{{"text": "Tests pass"}, {"text": "Docs updated"}}))
	var refused struct {
		Code     string                `json:"code"`
		Criteria []AcceptanceCriterion `json:"criteria"`
	}
	promoteToEvalCase(t, issue).Want(http.StatusConflict).JSON(&refused)
	if refused.Code != ErrCodeEvalCaseNeedsProofs || len(refused.Criteria) != 2 {
		t.Fatalf("refusal names the unproved criteria: %+v", refused)
	}

	// One proof is not enough; the second one opens the promotion.
	proveCriterion(t, issue, criteria[0].ID, map[string]any{"proof_type": "test", "proof_ref": "go test ./..."}).Want(http.StatusOK)
	promoteToEvalCase(t, issue).Want(http.StatusConflict).JSON(&refused)
	if len(refused.Criteria) != 1 {
		t.Fatalf("one criterion still lacks proof: %+v", refused.Criteria)
	}
	proveCriterion(t, issue, criteria[1].ID, map[string]any{"proof_type": "url", "proof_ref": "https://example.com/docs"}).Want(http.StatusOK)

	var out evalCaseEnvelope
	promoteToEvalCase(t, issue).Want(http.StatusCreated).JSON(&out)
	if out.Case.Title != "eval promotion" || out.Case.Description != "The statement" || out.Case.SourceIssueID != issue {
		t.Fatalf("snapshot = %+v", out.Case)
	}
	if len(out.Case.Criteria) != 2 || out.Case.Criteria[0].ProofState != ProofStateSatisfied || out.Case.Criteria[1].ProofRef != "https://example.com/docs" {
		t.Fatalf("the snapshot keeps the reference proofs: %+v", out.Case.Criteria)
	}

	var listed struct {
		Cases []EvalCaseResponse `json:"cases"`
	}
	evalWorkspaceCall(t, testHandler.ListEvalCases, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/eval-cases", nil).Want(http.StatusOK).JSON(&listed)
	found := false
	for _, c := range listed.Cases {
		if c.ID == out.Case.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("the new case is listed: %+v", listed.Cases)
	}
}

func TestEvalSuiteCreateValidatesCases(t *testing.T) {
	evalCleanup(t)
	caseID := evalProvedCase(t, "suite case", "Tests pass")

	create := func(body map[string]any) *testutil.Response {
		return evalWorkspaceCall(t, testHandler.CreateEvalSuite, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/eval-suites", body)
	}
	create(map[string]any{"name": "  ", "case_ids": []string{caseID}}).Want(http.StatusBadRequest)
	create(map[string]any{"name": "empty", "case_ids": []string{}}).Want(http.StatusBadRequest)
	create(map[string]any{"name": "unknown", "case_ids": []string{uuid.NewString()}}).Want(http.StatusBadRequest)
	create(map[string]any{"name": "mixed", "case_ids": []string{caseID, uuid.NewString()}}).Want(http.StatusBadRequest)

	var out evalSuiteEnvelope
	create(map[string]any{"name": "Regression suite", "case_ids": []string{caseID}}).Want(http.StatusCreated).JSON(&out)
	if out.Suite.Name != "Regression suite" || out.Suite.CaseCount != 1 || out.Suite.CaseIDs[0] != caseID {
		t.Fatalf("suite = %+v", out.Suite)
	}

	var listed struct {
		Suites []EvalSuiteResponse `json:"suites"`
	}
	evalWorkspaceCall(t, testHandler.ListEvalSuites, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/eval-suites", nil).Want(http.StatusOK).JSON(&listed)
	if len(listed.Suites) == 0 || listed.Suites[0].ID != out.Suite.ID {
		t.Fatalf("suites = %+v", listed.Suites)
	}
}

func TestEvalRunCreatesOneReplayIssuePerCase(t *testing.T) {
	evalCleanup(t)
	caseA := evalProvedCase(t, "replay case A", "Tests pass", "Docs updated")
	caseB := evalProvedCase(t, "replay case B", "Endpoint answers 200")

	var suite evalSuiteEnvelope
	evalWorkspaceCall(t, testHandler.CreateEvalSuite, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/eval-suites",
		map[string]any{"name": "two cases", "case_ids": []string{caseA, caseB}}).Want(http.StatusCreated).JSON(&suite)

	_, agentID, versionID := evalAgentWithVersion(t, "eval runner", "pinned instructions", "pinned-model")
	_, otherAgent, otherVersion := evalAgentWithVersion(t, "other runner", "other instructions", "other-model")

	// A version of another agent is not a version of this one.
	evalRunSuite(t, suite.Suite.ID, agentID, otherVersion).Want(http.StatusUnprocessableEntity)
	evalRunSuite(t, suite.Suite.ID, uuid.NewString(), versionID).Want(http.StatusUnprocessableEntity)
	_ = otherAgent

	var run evalRunEnvelope
	evalRunSuite(t, suite.Suite.ID, agentID, versionID).Want(http.StatusAccepted).JSON(&run)
	if run.Run.Status != "running" || len(run.Run.Cases) != 2 || run.Run.SuiteName != "two cases" || run.Run.AgentVersionNumber != 1 {
		t.Fatalf("run = %+v", run.Run)
	}
	// The suite's own order is kept and every case got a queued run.
	for _, c := range run.Run.Cases {
		if c.Status != "pending" || c.IssueID == "" || c.TaskID == "" {
			t.Fatalf("case = %+v", c)
		}
	}

	// One throwaway issue per case, assigned to the agent, carrying the
	// case's criteria WITHOUT their proofs.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE workspace_id = $1 AND origin_type = 'eval_run' AND origin_id = $2 AND assignee_type = 'agent' AND assignee_id = $3`, testWorkspaceID, run.Run.ID, agentID); n != 2 {
		t.Fatalf("replay issues = %d, want 2", n)
	}
	replay := listCriteria(t, run.Run.Cases[0].IssueID)
	if len(replay) != 2 {
		t.Fatalf("first case replays both criteria: %+v", replay)
	}
	for _, c := range replay {
		if c.ProofState != ProofStateMissing || c.ProofType != "" {
			t.Fatalf("the replay starts without proofs: %+v", c)
		}
	}
	var title, description string
	dbfx.QueryRow(t, `SELECT title, description FROM issue WHERE id = $1`, run.Run.Cases[0].IssueID).Scan(&title, &description)
	if title != "[Eval] replay case A" || !strings.Contains(description, evalBrief) {
		t.Fatalf("replay issue = %q / %q", title, description)
	}

	// A second run of the same suite is refused while the first is running.
	var conflict struct {
		Code string `json:"code"`
	}
	evalRunSuite(t, suite.Suite.ID, agentID, versionID).Want(http.StatusConflict).JSON(&conflict)
	if conflict.Code != ErrCodeEvalRunActive {
		t.Fatalf("conflict code = %q", conflict.Code)
	}

	var listed struct {
		Runs []EvalRunResponse `json:"runs"`
	}
	evalWorkspaceCall(t, testHandler.ListEvalRuns, http.MethodGet, "/api/workspaces/"+testWorkspaceID+"/eval-runs", nil).Want(http.StatusOK).JSON(&listed)
	if len(listed.Runs) == 0 || listed.Runs[0].ID != run.Run.ID || len(listed.Runs[0].Cases) != 2 {
		t.Fatalf("runs = %+v", listed.Runs)
	}

	var fetched evalRunEnvelope
	testutil.Call(t, testHandler.GetEvalRun, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/eval-runs/"+run.Run.ID, nil), "id", run.Run.ID)).Want(http.StatusOK).JSON(&fetched)
	if fetched.Run.ID != run.Run.ID || len(fetched.Run.Cases) != 2 {
		t.Fatalf("fetched run = %+v", fetched.Run)
	}
}

func TestEvalRunForcesContainerSandboxAndPinsTheVersion(t *testing.T) {
	evalCleanup(t)
	caseID := evalProvedCase(t, "sandbox case", "Tests pass")
	var suite evalSuiteEnvelope
	evalWorkspaceCall(t, testHandler.CreateEvalSuite, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/eval-suites",
		map[string]any{"name": "sandbox suite", "case_ids": []string{caseID}}).Want(http.StatusCreated).JSON(&suite)

	runtimeID, agentID, versionID := evalAgentWithVersion(t, "sandbox eval agent", "pinned instructions", "pinned-model")
	var run evalRunEnvelope
	evalRunSuite(t, suite.Suite.ID, agentID, versionID).Want(http.StatusAccepted).JSON(&run)
	taskID := run.Run.Cases[0].TaskID

	// The runtime asks for no confinement; an eval replay gets one anyway,
	// and runs the pinned version rather than the agent's current config.
	var claim struct {
		Task *struct {
			ID      string       `json:"id"`
			Sandbox *SandboxSpec `json:"sandbox"`
			Agent   *struct {
				Instructions string `json:"instructions"`
				Model        string `json:"model"`
			} `json:"agent"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "eval-daemon"), "runtimeId", runtimeID)).
		Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != taskID {
		t.Fatalf("claim = %+v", claim.Task)
	}
	if claim.Task.Sandbox == nil || claim.Task.Sandbox.Mode != "container" || claim.Task.Sandbox.Image != "ghcr.io/acme/eval:1" {
		t.Fatalf("an eval replay is always confined: %+v", claim.Task.Sandbox)
	}
	if claim.Task.Agent == nil || !strings.Contains(claim.Task.Agent.Instructions, "pinned instructions") || claim.Task.Agent.Model != "pinned-model" {
		t.Fatalf("the claim runs the pinned version: %+v", claim.Task.Agent)
	}

	// A daemon that could not confine the run does not get to start it.
	testutil.Call(t, testHandler.StartTask, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/start",
		map[string]any{"sandbox_requested": "container", "sandbox_mode": "none", "sandbox_reason": "docker is not available"}, testWorkspaceID, "eval-daemon"), "taskId", taskID)).
		Want(http.StatusConflict)

	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("the unconfined run is cancelled, got %q", status)
	}
	var fetched evalRunEnvelope
	testutil.Call(t, testHandler.GetEvalRun, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/eval-runs/"+run.Run.ID, nil), "id", run.Run.ID)).Want(http.StatusOK).JSON(&fetched)
	if len(fetched.Run.Cases) != 1 || fetched.Run.Cases[0].Status != "infra_failed" || fetched.Run.Cases[0].Score != nil {
		t.Fatalf("the case is an infrastructure failure, not a bad score: %+v", fetched.Run.Cases)
	}
	if !strings.Contains(fetched.Run.Cases[0].Detail, "sandbox unavailable") {
		t.Fatalf("detail = %q", fetched.Run.Cases[0].Detail)
	}
	if fetched.Run.Status != "failed" || fetched.Run.Score != nil {
		t.Fatalf("a run measured on nothing has no score: status=%q score=%v", fetched.Run.Status, fetched.Run.Score)
	}
}

func TestEvalRunScoresTheCriteriaTheAgentProved(t *testing.T) {
	evalCleanup(t)
	caseID := evalProvedCase(t, "scored case", "Tests pass", "Docs updated")
	var suite evalSuiteEnvelope
	evalWorkspaceCall(t, testHandler.CreateEvalSuite, http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/eval-suites",
		map[string]any{"name": "scored suite", "case_ids": []string{caseID}}).Want(http.StatusCreated).JSON(&suite)

	runtimeID, agentID, versionID := evalAgentWithVersion(t, "scored eval agent", "pinned instructions", "pinned-model")
	var run evalRunEnvelope
	evalRunSuite(t, suite.Suite.ID, agentID, versionID).Want(http.StatusAccepted).JSON(&run)
	taskID, replayIssue := run.Run.Cases[0].TaskID, run.Run.Cases[0].IssueID

	testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(
		newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "eval-daemon"), "runtimeId", runtimeID)).Want(http.StatusOK)
	testutil.Call(t, testHandler.StartTask, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/start",
		map[string]any{"sandbox_requested": "container", "sandbox_mode": "container"}, testWorkspaceID, "eval-daemon"), "taskId", taskID)).Want(http.StatusOK)

	// The agent proves every criterion of the replay issue, then finishes.
	for _, c := range listCriteria(t, replayIssue) {
		proveCriterion(t, replayIssue, c.ID, map[string]any{"proof_type": "test", "proof_ref": "go test ./..."}).Want(http.StatusOK)
	}
	testutil.Call(t, testHandler.CompleteTask, withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": "done"}, testWorkspaceID, "eval-daemon"), "taskId", taskID)).Want(http.StatusOK)

	var fetched evalRunEnvelope
	testutil.Call(t, testHandler.GetEvalRun, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/eval-runs/"+run.Run.ID, nil), "id", run.Run.ID)).Want(http.StatusOK).JSON(&fetched)
	if len(fetched.Run.Cases) != 1 {
		t.Fatalf("cases = %+v", fetched.Run.Cases)
	}
	got := fetched.Run.Cases[0]
	if got.Status != "passed" || got.Score == nil || *got.Score != 100 || got.Detail != "2/2 criteria proved" {
		t.Fatalf("case = %+v", got)
	}
	if fetched.Run.Status != "completed" || fetched.Run.Score == nil || *fetched.Run.Score != 100 {
		t.Fatalf("run = %+v", fetched.Run)
	}

	// The throwaway issue leaves the board once its verdict is in.
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, replayIssue).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("replay issue status = %q, want cancelled", status)
	}
}
