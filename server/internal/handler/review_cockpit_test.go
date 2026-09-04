package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Review cockpit (K16): one read gathering run, PRs, cost, open questions,
// criteria and plan verification; empty sources read as empty, not as errors.

func getCockpit(t *testing.T, issueID, query string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodGet, "/api/issues/"+issueID+"/review-cockpit"+query, nil)
	return testutil.Call(t, testHandler.GetReviewCockpit, testutil.WithURLParams(req, "id", issueID))
}

func TestReviewCockpitAggregatesEverySource(t *testing.T) {
	issue, task := completedAgentRun(t, "cockpit")
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(t.Context(), `DELETE FROM inbox_item WHERE issue_id = $1`, issue)
	})
	conn := vcsConnection(t)
	vcsPR(t, conn, issue, 41, "success", "pending")
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": task, "provider": "openai", "model": "m1", "input_tokens": 1000, "output_tokens": 200, "cost_usd_ticks": 3_000_000_000})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": task, "provider": "openai", "model": "m2", "input_tokens": 10, "output_tokens": 1})
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated)
	var answered decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&answered)
	respondDecision(t, issue, answered.Decision.ID, map[string]any{"option_id": "keep"}).Want(http.StatusOK)
	setCriteria(t, issue, []map[string]any{{"text": "Ships"}}).Want(http.StatusOK)

	// Answering the card queued a resume run: the newest run leads, the
	// completed one is selectable.
	var out ReviewCockpitResponse
	getCockpit(t, issue, "").Want(http.StatusOK).JSON(&out)
	if out.Run == nil || out.Run.Status != "queued" || len(out.Runs) != 2 || out.Runs[1].ID != task {
		t.Fatalf("run = %+v runs = %+v, want the queued resume run first and the completed one second", out.Run, out.Runs)
	}
	getCockpit(t, issue, "?run_id="+task).Want(http.StatusOK).JSON(&out)
	if out.Run == nil || out.Run.ID != task || out.Run.Status != "completed" || out.Run.CompletedAt == nil {
		t.Fatalf("selected run = %+v, want the completed run", out.Run)
	}
	if out.Usage == nil || out.Usage.InputTokens != 1010 || out.Usage.CostUsdTicks == nil || *out.Usage.CostUsdTicks != 3_000_000_000 || !out.Usage.Uncosted {
		t.Fatalf("usage = %+v, want summed tokens, the priced cost and the uncosted flag", out.Usage)
	}
	if out.MergeReadiness == nil || len(out.MergeReadiness.PRs) != 1 || out.MergeReadiness.Ready {
		t.Fatalf("merge readiness = %+v, want one PR with a pending check", out.MergeReadiness)
	}
	if len(out.OpenQuestions) != 1 || out.OpenQuestions[0].Response != nil {
		t.Fatalf("open questions = %+v, want only the unanswered card", out.OpenQuestions)
	}
	if len(out.Criteria) != 1 || out.Criteria[0].ProofState != ProofStateMissing {
		t.Fatalf("criteria = %+v", out.Criteria)
	}
	if out.PlanVerification != nil || out.SelfReview != nil || len(out.FailedSections) != 0 {
		t.Fatalf("plan/self-review/failed = %+v %+v %+v, want none", out.PlanVerification, out.SelfReview, out.FailedSections)
	}

	// An unknown run is a 404.
	getCockpit(t, issue, "?run_id=00000000-0000-0000-0000-000000000000").Want(http.StatusNotFound)
}

func TestReviewCockpitOnABareIssue(t *testing.T) {
	issue := dbfx.Issue(t, "cockpit bare")
	var out ReviewCockpitResponse
	getCockpit(t, issue, "").Want(http.StatusOK).JSON(&out)
	if out.Run != nil || out.Usage != nil || len(out.Runs) != 0 || len(out.OpenQuestions) != 0 || len(out.Criteria) != 0 {
		t.Fatalf("bare issue = %+v, want empty sections", out)
	}
	if out.MergeReadiness == nil || len(out.MergeReadiness.Blockers) == 0 {
		t.Fatalf("merge readiness = %+v, want the no-PR blocker", out.MergeReadiness)
	}
	if out.Issue.ID != issue || len(out.FailedSections) != 0 {
		t.Fatalf("issue = %s failed = %v", out.Issue.ID, out.FailedSections)
	}
}
