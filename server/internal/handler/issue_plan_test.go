package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// F17: the plan artifact and the verification report endpoints.

func putPlan(t *testing.T, issueID, content string) IssuePlanEnvelope {
	t.Helper()
	var out IssuePlanEnvelope
	testutil.Call(t, testHandler.SetIssuePlan, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issueID+"/plan", map[string]any{
			"content": content,
			"steps":   []map[string]any{{"id": "s1", "title": "Do the thing"}},
		}), "id", issueID,
	)).Want(http.StatusOK).JSON(&out)
	return out
}

func getPlan(t *testing.T, issueID string) IssuePlanEnvelope {
	t.Helper()
	var out IssuePlanEnvelope
	testutil.Call(t, testHandler.GetIssuePlan, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/plan", nil), "id", issueID,
	)).Want(http.StatusOK).JSON(&out)
	return out
}

// seedVerification creates a plan and a queued verification row bound to a
// fresh task on the issue, returning (planID, taskID).
func seedVerification(t *testing.T, issueID string) (string, string) {
	t.Helper()
	plan := putPlan(t, issueID, "1. build\n2. test")
	taskID := seedBatchTask(t, "plan verification "+uuid.NewString()[:8])
	dbfx.Insert(t, "plan_verification", testutil.Cols{
		"workspace_id":   testWorkspaceID,
		"issue_id":       issueID,
		"plan_id":        plan.Plan.ID,
		"plan_version":   plan.Plan.Version,
		"task_id":        taskID,
		"source_task_id": uuid.NewString(),
	})
	return plan.Plan.ID, taskID
}

func reportVerification(t *testing.T, issueID, taskID string, findings []map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.ReportPlanVerification, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/plan/verifications/"+taskID, map[string]any{
			"summary":  "checked",
			"findings": findings,
		}), "id", issueID, "runId", taskID,
	))
}

func TestIssuePlanVersionsSupersede(t *testing.T) {
	issue := dbfx.Issue(t, "plan versions")
	if got := getPlan(t, issue); got.Plan != nil || len(got.Versions) != 0 {
		t.Fatalf("fresh issue plan = %+v, want none", got)
	}
	first := putPlan(t, issue, "v1 plan")
	if first.Plan == nil || first.Plan.Version != 1 || first.Plan.AuthorType != "member" {
		t.Fatalf("first publish = %+v, want version 1 by a member", first.Plan)
	}
	second := putPlan(t, issue, "v2 plan")
	if second.Plan == nil || second.Plan.Version != 2 {
		t.Fatalf("second publish = %+v, want version 2", second.Plan)
	}

	got := getPlan(t, issue)
	if got.Plan == nil || got.Plan.Version != 2 || got.Plan.Content != "v2 plan" {
		t.Fatalf("active plan = %+v, want version 2", got.Plan)
	}
	if len(got.Versions) != 2 || got.Versions[0].Version != 2 || got.Versions[1].SupersededAt == nil {
		t.Fatalf("versions = %+v, want v2 then a superseded v1", got.Versions)
	}
	if string(got.Plan.Steps) != `[{"id":"s1","title":"Do the thing"}]` {
		t.Errorf("steps = %s, want the published steps", got.Plan.Steps)
	}
}

func TestIssuePlanRejectsEmptyContentAndForeignIssue(t *testing.T) {
	issue := dbfx.Issue(t, "plan validation")
	testutil.Call(t, testHandler.SetIssuePlan, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issue+"/plan", map[string]any{"content": "   "}), "id", issue,
	)).Want(http.StatusBadRequest)

	foreign := dbfx.Workspace(t, "Plan foreign", "plan-foreign-"+uuid.NewString())
	foreignIssue := dbfx.Issue(t, "plan foreign issue", testutil.Cols{"workspace_id": foreign})
	testutil.Call(t, testHandler.SetIssuePlan, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+foreignIssue+"/plan", map[string]any{"content": "x"}), "id", foreignIssue,
	)).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.GetIssuePlan, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+foreignIssue+"/plan", nil), "id", foreignIssue,
	)).Want(http.StatusNotFound)
}

func TestReportPlanVerificationCountsAndIsIdempotent(t *testing.T) {
	issue := dbfx.Issue(t, "plan report")
	_, taskID := seedVerification(t, issue)

	findings := []map[string]any{
		{"severity": "Critical", "title": "Endpoint missing", "files": []string{"a.go"}, "plan_step_id": "s1"},
		{"severity": "minor", "title": "Naming"},
		{"severity": "weird", "title": "Unknown severity still stored"},
	}
	var first struct {
		Verification PlanVerificationResponse `json:"verification"`
		Replayed     bool                     `json:"replayed"`
	}
	reportVerification(t, issue, taskID, findings).Want(http.StatusOK).JSON(&first)
	v := first.Verification
	if v.State != "reported" || v.CriticalCount != 1 || v.MinorCount != 1 || v.MajorCount != 0 || v.OutdatedCount != 0 {
		t.Fatalf("report = %+v, want reported with 1 critical and 1 minor", v)
	}
	if v.Summary == nil || *v.Summary != "checked" || v.ReportedAt == nil {
		t.Fatalf("report = %+v, want summary and reported_at", v)
	}

	// A system comment doubled the report.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND type = 'system' AND content LIKE 'Plan verification%'`, issue); n != 1 {
		t.Fatalf("system comments = %d, want 1", n)
	}

	var second struct {
		Verification PlanVerificationResponse `json:"verification"`
		Replayed     bool                     `json:"replayed"`
	}
	reportVerification(t, issue, taskID, []map[string]any{}).Want(http.StatusOK).JSON(&second)
	if !second.Replayed || second.Verification.CriticalCount != 1 {
		t.Fatalf("second report = %+v, want the first report replayed untouched", second)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND type = 'system'`, issue); n != 1 {
		t.Fatalf("system comments after replay = %d, want still 1", n)
	}

	// Listed on the issue.
	var listed struct {
		Verifications []PlanVerificationResponse `json:"verifications"`
	}
	testutil.Call(t, testHandler.ListPlanVerifications, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issue+"/plan/verifications", nil), "id", issue,
	)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Verifications) != 1 || listed.Verifications[0].TaskID != taskID {
		t.Fatalf("verifications = %+v, want the one report", listed.Verifications)
	}
}

func TestReportPlanVerificationUnknownRunIs404(t *testing.T) {
	issue := dbfx.Issue(t, "plan report unknown run")
	reportVerification(t, issue, uuid.NewString(), []map[string]any{}).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.ReportPlanVerification, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issues/"+issue+"/plan/verifications/not-a-uuid", map[string]any{"findings": []any{}}),
		"id", issue, "runId", "not-a-uuid",
	)).Want(http.StatusBadRequest)
}
