package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Blast radius (K07): rules per project, most specific first, conflicts
// refused at creation, and the approval gates (K05) obey them: read_only
// refuses before any card, autonomous passes without one, dual_approval
// needs two different humans.

func blastCall(t *testing.T, h http.HandlerFunc, method, path string, body any, params ...string) *testutil.Response {
	t.Helper()
	req := testutil.WithHeaders(newRequest(method, path, body), "X-Workspace-ID", testWorkspaceID)
	return testutil.Call(t, inboxWorkspaceHandler(h), testutil.WithURLParams(req, params...))
}

func TestBlastRadiusRulesCRUDConflictAndPreview(t *testing.T) {
	project := dbfx.Project(t, "blast radius project")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_blast_radius_rule WHERE project_id = $1`, project)
	})
	create := func(pattern, level string) *testutil.Response {
		return blastCall(t, testHandler.CreateBlastRadiusRule, http.MethodPost, "/api/projects/"+project+"/blast-radius-rules", map[string]any{"path_pattern": pattern, "autonomy_level": level}, "id", project)
	}
	create("src/[ab]", "autonomous").Want(http.StatusBadRequest)
	create("apps/**", "god").Want(http.StatusBadRequest)
	create("apps/mobile/**", "autonomous").Want(http.StatusCreated)
	create("server/migrations/**", "read_only").Want(http.StatusCreated)
	create("**", "dual_approval").Want(http.StatusCreated)
	create("apps/mobile/**", "read_only").Want(http.StatusConflict)
	if res := create("apps/mobile/*", "read_only"); res.Code != http.StatusConflict || res.Map()["code"] != "blast_radius_conflict" {
		t.Fatalf("same specificity, different level: %d %s", res.Code, res.Text())
	}
	var list struct {
		Rules  []BlastRadiusRuleResponse `json:"rules"`
		Levels []string                  `json:"levels"`
	}
	blastCall(t, testHandler.ListBlastRadiusRules, http.MethodGet, "/api/projects/"+project+"/blast-radius-rules", nil, "id", project).Want(http.StatusOK).JSON(&list)
	if len(list.Rules) != 3 || list.Rules[0].PathPattern != "server/migrations/**" || list.Rules[2].PathPattern != "**" || len(list.Levels) != 3 {
		t.Fatalf("list = %+v", list)
	}
	preview := func(path string) map[string]any {
		return blastCall(t, testHandler.PreviewBlastRadius, http.MethodGet, "/api/projects/"+project+"/blast-radius-preview?path="+path, nil, "id", project).Want(http.StatusOK).Map()
	}
	if p := preview("server/migrations/1.sql"); p["level"] != "read_only" {
		t.Fatalf("preview = %v", p)
	}
	if p := preview("apps/mobile/x.tsx"); p["level"] != "autonomous" {
		t.Fatalf("preview = %v", p)
	}
	if p := preview("README.md"); p["level"] != "dual_approval" {
		t.Fatalf("preview = %v", p)
	}
	ruleID := list.Rules[2].ID
	blastCall(t, testHandler.DeleteBlastRadiusRule, http.MethodDelete, "/api/projects/"+project+"/blast-radius-rules/"+ruleID, nil, "id", project, "ruleId", ruleID).Want(http.StatusNoContent)
	if p := preview("README.md"); p["level"] != "inherit" {
		t.Fatalf("after delete = %v", p)
	}
}

func TestBlastRadiusDrivesApprovalGates(t *testing.T) {
	project := dbfx.Project(t, "blast radius gates")
	issue, task, agent := runningAgentRun(t, "gate blast")
	dbfx.Exec(t, `UPDATE issue SET project_id = $2 WHERE id = $1`, issue, project)
	second := dbfx.User(t, "Second approver", "second-"+uuid.NewString()[:8]+"@multica.ai")
	dbfx.Member(t, testWorkspaceID, second, "admin")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_blast_radius_rule WHERE project_id = $1`, project)
		testPool.Exec(context.Background(), `DELETE FROM approval_gate_event WHERE task_id = $1`, task)
		testPool.Exec(context.Background(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	for pattern, level := range map[string]string{"apps/mobile/**": "autonomous", "server/migrations/**": "read_only", "billing/**": "dual_approval"} {
		blastCall(t, testHandler.CreateBlastRadiusRule, http.MethodPost, "/api/projects/"+project+"/blast-radius-rules", map[string]any{"path_pattern": pattern, "autonomy_level": level}, "id", project).Want(http.StatusCreated)
	}
	hdr := gateHeaders(task, agent)
	open := func(paths []string) ApprovalGateResponse {
		var g ApprovalGateResponse
		gateCall(t, testHandler.CreateApprovalGate, http.MethodPost, "/api/tasks/"+task+"/gates", map[string]any{"gate_type": "git_push", "summary": "push", "details": map[string]any{"paths": paths}}, hdr, "taskId", task).Want(http.StatusCreated).JSON(&g)
		return g
	}
	cards := func() int { return dbfx.Count(t, `SELECT COUNT(*) FROM issue_decision WHERE issue_id = $1`, issue) }

	// read_only: refused before any card, even mixed with autonomous paths.
	if g := open([]string{"apps/mobile/a.ts", "server/migrations/9.sql"}); g.Status != "denied" || g.DecisionID != nil {
		t.Fatalf("read_only gate = %+v", g)
	}
	// autonomous only: approved at once, no card.
	if g := open([]string{"apps/mobile/a.ts", "apps/mobile/b.ts"}); g.Status != "approved" || g.DecisionID != nil {
		t.Fatalf("autonomous gate = %+v", g)
	}
	if cards() != 0 {
		t.Fatal("no card must exist yet")
	}
	// No rule: one card, one approval.
	g := open([]string{"docs/readme.md"})
	if g.Status != "pending" || g.DecisionID == nil {
		t.Fatalf("inherit gate = %+v", g)
	}
	respondDecision(t, issue, *g.DecisionID, map[string]any{"option_id": "approve"}).Want(http.StatusOK)
	var polled ApprovalGateResponse
	gateCall(t, testHandler.GetApprovalGate, http.MethodGet, "/api/tasks/"+task+"/gates/"+g.ID, nil, hdr, "taskId", task, "gateId", g.ID).Want(http.StatusOK).JSON(&polled)
	if polled.Status != "approved" {
		t.Fatalf("inherit gate after approval = %+v", polled)
	}
	// dual_approval: the first approval files a second card; the same person
	// cannot count twice; a different admin settles it.
	g = open([]string{"billing/invoice.go"})
	if g.Status != "pending" {
		t.Fatalf("dual gate = %+v", g)
	}
	before := cards()
	respondDecision(t, issue, *g.DecisionID, map[string]any{"option_id": "approve"}).Want(http.StatusOK)
	gateCall(t, testHandler.GetApprovalGate, http.MethodGet, "/api/tasks/"+task+"/gates/"+g.ID, nil, hdr, "taskId", task, "gateId", g.ID).Want(http.StatusOK).JSON(&polled)
	if polled.Status != "pending" || cards() != before+1 {
		t.Fatalf("after first approval = %+v, cards %d -> %d", polled, before, cards())
	}
	var secondCard string
	dbfx.QueryRow(t, `SELECT details->>'pending_decision_id' FROM approval_gate_event WHERE id = $1`, g.ID).Scan(&secondCard)
	// Same approver again: still pending, yet another card.
	respondDecision(t, issue, secondCard, map[string]any{"option_id": "approve"}).Want(http.StatusOK)
	gateCall(t, testHandler.GetApprovalGate, http.MethodGet, "/api/tasks/"+task+"/gates/"+g.ID, nil, hdr, "taskId", task, "gateId", g.ID).Want(http.StatusOK).JSON(&polled)
	if polled.Status != "pending" {
		t.Fatalf("same approver twice must not settle: %+v", polled)
	}
	dbfx.QueryRow(t, `SELECT details->>'pending_decision_id' FROM approval_gate_event WHERE id = $1`, g.ID).Scan(&secondCard)
	// A different admin answers the pending card.
	req := testutil.WithHeaders(newRequest(http.MethodPost, "/api/issues/"+issue+"/decisions/"+secondCard+"/respond", map[string]any{"option_id": "approve"}), "X-User-ID", second)
	testutil.Call(t, testHandler.RespondIssueDecision, testutil.WithURLParams(req, "id", issue, "decisionId", secondCard)).Want(http.StatusOK)
	gateCall(t, testHandler.GetApprovalGate, http.MethodGet, "/api/tasks/"+task+"/gates/"+g.ID, nil, hdr, "taskId", task, "gateId", g.ID).Want(http.StatusOK).JSON(&polled)
	if polled.Status != "approved" {
		t.Fatalf("after second approver = %+v", polled)
	}
}
