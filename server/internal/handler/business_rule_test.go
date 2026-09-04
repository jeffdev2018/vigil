package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Business rules (K53): draft → preview → dry-run → activate, deterministic
// enforcement at project creation and review submission, violations kept
// after disabling.

func ruleCall(t *testing.T, h http.HandlerFunc, method, path string, body any, params ...string) *testutil.Response {
	t.Helper()
	req := testutil.WithHeaders(newRequest(method, path, body), "X-Workspace-ID", testWorkspaceID)
	if len(params) == 2 {
		req = testutil.WithURLParams(req, params[0], params[1])
	}
	return testutil.Call(t, inboxWorkspaceHandler(h), req)
}

func createProjectCall(t *testing.T, title string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.CreateProject, newRequest(http.MethodPost, "/api/projects?workspace_id="+testWorkspaceID, map[string]any{"title": title}))
}

func TestBusinessRulesLifecycleAndProjectGate(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM business_rule_violation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM business_rule WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM project WHERE workspace_id = $1 AND title LIKE 'rule gate %'`, testWorkspaceID)
	})
	prev := testHandler.LLM
	testHandler.LLM = nil
	t.Cleanup(func() { testHandler.LLM = prev })

	projects := dbfx.Count(t, `SELECT COUNT(*) FROM project WHERE workspace_id = $1`, testWorkspaceID)
	predicate := map[string]any{"all": []map[string]any{{"field": "workspace.project_count", "op": "lte", "value": projects}}}

	// Validation.
	ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{"natural_language": "x", "attach_point": "nope", "predicate": predicate}).Want(http.StatusBadRequest)
	ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{"natural_language": "x", "attach_point": "project_create", "predicate": map[string]any{"all": []map[string]any{{"field": "issue.priority", "op": "eq", "value": "high"}}}}).Want(http.StatusUnprocessableEntity)
	res := ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{"natural_language": "no more projects", "attach_point": "project_create"})
	if res.Code != http.StatusServiceUnavailable || res.Map()["code"] != "llm_unavailable" {
		t.Fatalf("no llm: %d %s", res.Code, res.Text())
	}

	// A draft with a preview, compiled by hand.
	var created struct{ Rule BusinessRuleResponse }
	ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{"natural_language": "The workspace keeps its current number of projects", "attach_point": "project_create", "predicate": predicate}).Want(http.StatusCreated).JSON(&created)
	rule := created.Rule
	if rule.Status != "draft" || rule.Description == "" || rule.Title == "" {
		t.Fatalf("draft = %+v", rule)
	}

	// Dry-run names the next project as a violation without blocking.
	var dry struct {
		Checked    int             `json:"checked"`
		Violations []DryRunSubject `json:"violations"`
	}
	ruleCall(t, testHandler.DryRunBusinessRule, http.MethodPost, "/api/business-rules/"+rule.ID+"/dry-run", nil, "id", rule.ID).Want(http.StatusOK).JSON(&dry)
	if dry.Checked != 1 || len(dry.Violations) != 1 || dry.Violations[0].SubjectType != "project_create" {
		t.Fatalf("dry-run = %+v", dry)
	}
	createProjectCall(t, "rule gate draft").Want(http.StatusCreated)
	predicate["all"].([]map[string]any)[0]["value"] = projects + 1

	// Activation blocks the next project with the rule's title and detail.
	ruleCall(t, testHandler.ActivateBusinessRule, http.MethodPut, "/api/business-rules/"+rule.ID+"/activate", nil, "id", rule.ID).Want(http.StatusOK)
	ruleCall(t, testHandler.ActivateBusinessRule, http.MethodPut, "/api/business-rules/"+rule.ID+"/activate", nil, "id", rule.ID).Want(http.StatusConflict)
	res = createProjectCall(t, "rule gate blocked")
	if res.Code != http.StatusUnprocessableEntity || res.Map()["code"] != ErrCodeBusinessRuleViolation || res.Map()["rule_id"] != rule.ID || res.Map()["title"] != rule.Title {
		t.Fatalf("blocked create: %d %s", res.Code, res.Text())
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM project WHERE workspace_id = $1 AND title = 'rule gate blocked'`, testWorkspaceID); n != 0 {
		t.Fatal("a blocked project must not be created")
	}
	var violations struct {
		Violations []BusinessRuleViolationResponse `json:"violations"`
	}
	ruleCall(t, testHandler.ListBusinessRuleViolations, http.MethodGet, "/api/business-rules/"+rule.ID+"/violations", nil, "id", rule.ID).Want(http.StatusOK).JSON(&violations)
	if len(violations.Violations) != 1 || violations.Violations[0].Detail == nil {
		t.Fatalf("violations = %+v", violations.Violations)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND details->>'rule_id' = $2`, AuditBusinessRuleViolated, rule.ID); n != 1 {
		t.Fatalf("audit = %d, want 1", n)
	}

	// Disabling stops the rule at once and keeps the violation; delete needs disabled.
	ruleCall(t, testHandler.DeleteBusinessRule, http.MethodDelete, "/api/business-rules/"+rule.ID, nil, "id", rule.ID).Want(http.StatusConflict)
	ruleCall(t, testHandler.DisableBusinessRule, http.MethodPut, "/api/business-rules/"+rule.ID+"/disable", nil, "id", rule.ID).Want(http.StatusOK)
	createProjectCall(t, "rule gate after disable").Want(http.StatusCreated)
	ruleCall(t, testHandler.ListBusinessRuleViolations, http.MethodGet, "/api/business-rules/"+rule.ID+"/violations", nil, "id", rule.ID).Want(http.StatusOK).JSON(&violations)
	if len(violations.Violations) != 1 {
		t.Fatalf("violations after disable = %d, want kept", len(violations.Violations))
	}
	var list struct {
		Rules        []BusinessRuleResponse `json:"rules"`
		AttachPoints []string               `json:"attach_points"`
	}
	ruleCall(t, testHandler.ListBusinessRules, http.MethodGet, "/api/business-rules", nil).Want(http.StatusOK).JSON(&list)
	if len(list.Rules) != 1 || list.Rules[0].Status != "disabled" || len(list.AttachPoints) != 3 {
		t.Fatalf("list = %+v", list)
	}
	ruleCall(t, testHandler.DeleteBusinessRule, http.MethodDelete, "/api/business-rules/"+rule.ID, nil, "id", rule.ID).Want(http.StatusNoContent)
	ruleCall(t, testHandler.DryRunBusinessRule, http.MethodPost, "/api/business-rules/"+rule.ID+"/dry-run", nil, "id", rule.ID).Want(http.StatusNotFound)
}

func TestBusinessRulesCompileWithLLMAndGateReview(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM business_rule_violation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM business_rule WHERE workspace_id = $1`, testWorkspaceID)
	})
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"title":"Described before review","predicate":{"all":[{"field":"issue.has_description","op":"eq","value":true}]}}`))
	var created struct{ Rule BusinessRuleResponse }
	ruleCall(t, testHandler.CreateBusinessRule, http.MethodPost, "/api/business-rules", map[string]any{"natural_language": "An issue needs a description before review", "attach_point": "issue_submit_review"}).Want(http.StatusCreated).JSON(&created)
	rule := created.Rule
	if rule.Title != "Described before review" || rule.Description != "whether the issue has a description must be true" {
		t.Fatalf("compiled = %+v", rule)
	}

	bare := dbfx.Issue(t, "rule review bare", testutil.Cols{"status": "in_progress"})
	described := dbfx.Issue(t, "rule review described", testutil.Cols{"status": "in_progress", "description": "It does this."})
	already := dbfx.Issue(t, "rule review already", testutil.Cols{"status": "in_review"})

	// Dry-run scans the issues already in review.
	var dry struct {
		Checked    int             `json:"checked"`
		Violations []DryRunSubject `json:"violations"`
	}
	ruleCall(t, testHandler.DryRunBusinessRule, http.MethodPost, "/api/business-rules/"+rule.ID+"/dry-run", nil, "id", rule.ID).Want(http.StatusOK).JSON(&dry)
	found := false
	for _, v := range dry.Violations {
		found = found || v.SubjectID == already
	}
	if dry.Checked == 0 || !found {
		t.Fatalf("dry-run = %+v, want the undescribed issue in review", dry)
	}

	// Draft: nothing blocks. Active: the bare issue is refused, the described one passes.
	moveIssue(t, bare, "in_review").Want(http.StatusOK)
	moveIssue(t, bare, "in_progress").Want(http.StatusOK)
	ruleCall(t, testHandler.ActivateBusinessRule, http.MethodPut, "/api/business-rules/"+rule.ID+"/activate", nil, "id", rule.ID).Want(http.StatusOK)
	res := moveIssue(t, bare, "in_review")
	if res.Code != http.StatusUnprocessableEntity || res.Map()["code"] != ErrCodeBusinessRuleViolation {
		t.Fatalf("bare issue: %d %s", res.Code, res.Text())
	}
	moveIssue(t, described, "in_review").Want(http.StatusOK)
	// Leaving review and a non-review move are never checked.
	moveIssue(t, already, "done").Want(http.StatusOK)
	moveIssue(t, bare, "done").Want(http.StatusOK)
}
