package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Plan Gate (K11): approving a plan version creates its steps as staged
// sub-issues with blocking dependencies, exactly once.

func putPlanSteps(t *testing.T, issueID string, steps []map[string]any, headers ...string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPut, "/api/issues/"+issueID+"/plan", map[string]any{"content": "# Plan", "steps": steps})
	if len(headers) > 0 {
		req = testutil.WithHeaders(req, headers...)
	}
	return testutil.Call(t, testHandler.SetIssuePlan, testutil.WithURLParams(req, "id", issueID))
}

func materializePlan(t *testing.T, issueID string, version string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/plan/"+version+"/materialize", map[string]any{})
	return testutil.Call(t, testHandler.MaterializeIssuePlan, testutil.WithURLParams(req, "id", issueID, "version", version))
}

// cleanupChildren removes what materialization created under the parent. It
// also aligns the workspace issue counter with the fixtures' MAX(number)+1
// numbering, so the service's allocator does not collide with a fixture row.
func cleanupChildren(t *testing.T, parentID string) {
	t.Helper()
	if _, err := testPool.Exec(t.Context(), `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := t.Context()
		testPool.Exec(ctx, `DELETE FROM issue_dependency WHERE issue_id IN (SELECT id FROM issue WHERE parent_issue_id = $1) OR depends_on_issue_id IN (SELECT id FROM issue WHERE parent_issue_id = $1)`, parentID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE parent_issue_id = $1)`, parentID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE parent_issue_id = $1`, parentID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, parentID)
	})
}

type planGateResult struct {
	Plan   IssuePlanResponse `json:"plan"`
	Issues []IssueResponse   `json:"issues"`
}

func TestPlanGateMaterializesStagedSubIssuesOnce(t *testing.T) {
	agentID := dbfx.Agent(t, "plan gate agent", handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "plan gate parent", testutil.Cols{"priority": "high"})
	cleanupChildren(t, issue)
	putPlanSteps(t, issue, []map[string]any{
		{"id": "s1", "title": "Add the endpoint", "assignee_type": "agent", "assignee_id": agentID},
		{"id": "s2", "title": "Test it", "after": []string{"s1"}},
		{"id": "s3", "title": "Document it", "after": []string{"s1"}, "assignee_type": "agent", "assignee_id": uuid.NewString()},
		{"id": "s4", "title": "Ship it", "after": []string{"s2", "s3"}},
	}).Want(http.StatusOK)

	var out planGateResult
	materializePlan(t, issue, "1").Want(http.StatusOK).JSON(&out)
	if len(out.Issues) != 4 || out.Plan.MaterializedAt == nil {
		t.Fatalf("materialized = %d issues, materialized_at = %v; want 4 and a timestamp", len(out.Issues), out.Plan.MaterializedAt)
	}
	type row struct {
		title, status string
		stage         int32
		assignee      *string
	}
	rows := map[string]row{}
	res, err := testPool.Query(t.Context(), `SELECT title, status, stage, assignee_id::text FROM issue WHERE parent_issue_id = $1`, issue)
	if err != nil {
		t.Fatal(err)
	}
	for res.Next() {
		var r row
		if err := res.Scan(&r.title, &r.status, &r.stage, &r.assignee); err != nil {
			t.Fatal(err)
		}
		rows[r.title] = r
	}
	res.Close()
	want := map[string]struct {
		status string
		stage  int32
	}{"Add the endpoint": {"todo", 1}, "Test it": {"backlog", 2}, "Document it": {"backlog", 2}, "Ship it": {"backlog", 3}}
	for title, w := range want {
		got, ok := rows[title]
		if !ok || got.status != w.status || got.stage != w.stage {
			t.Fatalf("child %q = %+v, want status %s stage %d", title, got, w.status, w.stage)
		}
	}
	if rows["Add the endpoint"].assignee == nil || *rows["Add the endpoint"].assignee != agentID {
		t.Fatalf("s1 assignee = %v, want the agent", rows["Add the endpoint"].assignee)
	}
	if rows["Document it"].assignee != nil {
		t.Fatalf("s3 assignee = %v, want the unknown suggestion dropped", *rows["Document it"].assignee)
	}
	// Predecessor blocks successor: four edges, stored from the predecessor.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue_dependency d JOIN issue a ON a.id = d.issue_id JOIN issue b ON b.id = d.depends_on_issue_id
		WHERE a.parent_issue_id = $1 AND d.type = 'blocks' AND a.stage < b.stage`, issue); n != 4 {
		t.Fatalf("blocking edges = %d, want 4", n)
	}
	// The steps now name their sub-issues.
	plan := getPlan(t, issue).Plan
	steps := parsePlanSteps(plan.Steps)
	var shipID string
	dbfx.QueryRow(t, `SELECT id FROM issue WHERE parent_issue_id = $1 AND title = 'Ship it'`, issue).Scan(&shipID)
	if len(steps) != 4 || steps[0].IssueID == "" || steps[3].IssueID != shipID {
		t.Fatalf("steps after materialization = %+v", steps)
	}

	// Replaying the approval creates nothing more.
	var refused struct{ Code string }
	materializePlan(t, issue, "1").Want(http.StatusConflict).JSON(&refused)
	if refused.Code != ErrCodePlanAlreadyMaterialized {
		t.Fatalf("code = %q, want %s", refused.Code, ErrCodePlanAlreadyMaterialized)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE parent_issue_id = $1`, issue); n != 4 {
		t.Fatalf("children after replay = %d, want 4", n)
	}
}

func TestPlanGateRefusesBadStepsSupersededAndStepless(t *testing.T) {
	issue := dbfx.Issue(t, "plan gate refusals")
	cleanupChildren(t, issue)
	putPlanSteps(t, issue, []map[string]any{{"id": "a", "title": "A", "after": []string{"zzz"}}}).Want(http.StatusBadRequest)
	putPlanSteps(t, issue, []map[string]any{{"id": "a", "title": "A", "after": []string{"b"}}, {"id": "b", "title": "B", "after": []string{"a"}}}).Want(http.StatusBadRequest)
	putPlanSteps(t, issue, []map[string]any{{"id": "a", "title": "A"}, {"id": "a", "title": "A again"}}).Want(http.StatusBadRequest)
	putPlanSteps(t, issue, []map[string]any{{"title": "A", "assignee_type": "agent"}}).Want(http.StatusBadRequest)

	putPlanSteps(t, issue, []map[string]any{{"title": "Only step"}}).Want(http.StatusOK) // v1
	putPlanSteps(t, issue, nil).Want(http.StatusOK)                                      // v2 supersedes v1, no steps
	var refused struct{ Code string }
	materializePlan(t, issue, "1").Want(http.StatusConflict).JSON(&refused)
	if refused.Code != ErrCodePlanSuperseded {
		t.Fatalf("code = %q, want %s", refused.Code, ErrCodePlanSuperseded)
	}
	materializePlan(t, issue, "2").Want(http.StatusBadRequest).JSON(&refused)
	if refused.Code != ErrCodePlanHasNoSteps {
		t.Fatalf("code = %q, want %s", refused.Code, ErrCodePlanHasNoSteps)
	}
	materializePlan(t, issue, "9").Want(http.StatusNotFound)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE parent_issue_id = $1`, issue); n != 0 {
		t.Fatalf("children = %d, want none", n)
	}
}

func TestPlanGateApprovalCardFromARun(t *testing.T) {
	issue := dbfx.Issue(t, "plan gate card")
	cleanupChildren(t, issue)
	t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue) })
	// What the auth middleware stamps on a task-token request.
	agent := []string{"X-Actor-Source", "task_token", "X-Agent-ID", dbfx.Agent(t, "plan gate run agent", handlerTestRuntimeID(t))}

	// A human's plan files no card; a run's plan with steps does.
	putPlanSteps(t, issue, []map[string]any{{"title": "Human step"}}).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue_decision WHERE issue_id = $1`, issue); n != 0 {
		t.Fatalf("decisions after a human plan = %d, want 0", n)
	}
	putPlanSteps(t, issue, []map[string]any{{"id": "s1", "title": "First"}, {"id": "s2", "title": "Second", "after": []string{"s1"}}}, agent...).Want(http.StatusOK) // v2
	var decisionID string
	var planVersion int32
	dbfx.QueryRow(t, `SELECT id, plan_version FROM issue_decision WHERE issue_id = $1 AND response IS NULL`, issue).Scan(&decisionID, &planVersion)
	if planVersion != 2 {
		t.Fatalf("card plan_version = %d, want 2", planVersion)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'decision_request'`, issue); n < 1 {
		t.Fatal("the approval card must reach the inbox")
	}

	// Approving from the card creates the sub-issues and records the answer.
	var answered decisionEnvelope
	respondDecision(t, issue, decisionID, map[string]any{"option_id": "approve"}).Want(http.StatusOK).JSON(&answered)
	if answered.Decision.Response == nil || answered.Decision.PlanVersion != 2 {
		t.Fatalf("answered card = %+v", answered.Decision)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE parent_issue_id = $1`, issue); n != 2 {
		t.Fatalf("children after approval = %d, want 2", n)
	}
	respondDecision(t, issue, decisionID, map[string]any{"option_id": "approve"}).Want(http.StatusConflict)

	// A newer plan gets its own card; once the human approved it directly, the
	// card cannot create the sub-issues twice and stays unanswered.
	putPlanSteps(t, issue, []map[string]any{{"title": "Third"}}, agent...).Want(http.StatusOK) // v3
	var cardV3 string
	dbfx.QueryRow(t, `SELECT id FROM issue_decision WHERE issue_id = $1 AND plan_version = 3`, issue).Scan(&cardV3)
	materializePlan(t, issue, "3").Want(http.StatusOK)
	var refused struct{ Code string }
	respondDecision(t, issue, cardV3, map[string]any{"option_id": "approve"}).Want(http.StatusConflict).JSON(&refused)
	if refused.Code != ErrCodePlanAlreadyMaterialized {
		t.Fatalf("code = %q, want %s", refused.Code, ErrCodePlanAlreadyMaterialized)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue_decision WHERE id = $1 AND response IS NULL`, cardV3); n != 1 {
		t.Fatal("a refused approval must leave the card unanswered")
	}
	// Asking for changes creates nothing.
	respondDecision(t, issue, cardV3, map[string]any{"option_id": "revise"}).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE parent_issue_id = $1`, issue); n != 3 {
		t.Fatalf("children = %d, want 3", n)
	}
}
