package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"errors"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Cross-provider self-review (K15): a completed code run with a diff gets
// a review run by the least recently used agent of ANOTHER provider, briefed
// with the diff only; review runs are not reviewed; no diff, no review; the
// author alone in the workspace means no review at all; the reviewer's report
// is stored as a review_report message and listed; a failed review can be
// retried once, not while one is pending. (JEF-238 widened "another provider"
// to "not the author's (runtime, model) pair, another provider first" — see
// cross_review_reviewer_test.go.)

func providerRuntime(t *testing.T, provider string) string {
	t.Helper()
	return dbfx.Insert(t, "agent_runtime", testutil.Cols{
		"workspace_id": testWorkspaceID, "daemon_id": "xr-" + uuid.NewString()[:8], "name": provider + " runtime", "runtime_mode": "local",
		"provider": provider, "status": "online", "owner_id": testUserID, "last_seen_at": testutil.Raw("now()"),
	})
}

// quietOtherAgents archives every other agent of the test workspace for the
// duration of the test, so the reviewer choice is about our fixtures only.
func quietOtherAgents(t *testing.T, keep ...string) {
	t.Helper()
	rows, err := testPool.Query(context.Background(), `SELECT id FROM agent WHERE workspace_id = $1 AND archived_at IS NULL AND NOT (id = ANY($2::uuid[]))`, testWorkspaceID, keep)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = ANY($1::uuid[])`, ids)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE agent SET archived_at = NULL WHERE id = ANY($1::uuid[])`, ids)
	})
}

func TestCrossReviewParsesReports(t *testing.T) {
	r := parseCrossReviewReport("Some notes.\n\n```review_report\n{\"verdict\":\"request_changes\",\"risks\":[\"no test on the retry path\"],\"questions\":[],\"suggestions\":[\"extract the helper\"]}\n```\n")
	if r.Verdict != "request_changes" || len(r.Risks) != 1 || len(r.Suggestions) != 1 || r.Questions == nil {
		t.Fatalf("parsed = %+v", r)
	}
	r = parseCrossReviewReport("First paragraph.\n\nLooks fine overall, one nit.")
	if r.Verdict != "comment" || r.Summary != "Looks fine overall, one nit." || len(r.Risks) != 0 {
		t.Fatalf("fallback = %+v", r)
	}
	if parseCrossReviewReport("```review_report\n{\"verdict\":\"weird\"}\n```").Verdict != "comment" {
		t.Fatal("unknown verdict must fall back to comment")
	}
	if diffReference("", "", nil) != "" || diffReference("", "feat/x", []string{"a"}) != "branch feat/x" || diffReference("https://x/pull/1", "b", nil) != "pull request https://x/pull/1" {
		t.Fatal("diff reference precedence")
	}
}

func TestCrossReviewTriggerReportAndRetry(t *testing.T) {
	claude := providerRuntime(t, "claude")
	codex := providerRuntime(t, "codex")
	author := dbfx.Agent(t, "xr author", claude)
	sibling := dbfx.Agent(t, "xr sibling", claude)
	reviewer := dbfx.Agent(t, "xr reviewer", codex)
	issue := dbfx.Issue(t, "Cross review issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": author})
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM task_message WHERE task_id IN (SELECT id FROM agent_task_queue WHERE agent_id IN ($1, $2, $3))`, author, sibling, reviewer)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2, $3)`, author, sibling, reviewer)
	})
	quietOtherAgents(t, author, sibling, reviewer)
	prevFetcher := testHandler.DiffFetcher
	testHandler.DiffFetcher = fakeDiffFetcher{}
	t.Cleanup(func() { testHandler.DiffFetcher = prevFetcher })
	ctx := context.Background()
	// No diff: nothing to review.
	bare := dbfx.Task(t, author, testutil.Cols{"runtime_id": claude, "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()")})
	testHandler.triggerCrossReview(ctx, mustTask(t, bare), "", "")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE review_of_task_id = $1`, bare); n != 0 {
		t.Fatal("a run without a diff must not be reviewed")
	}
	// A completed run with a PR: the codex agent reviews it, briefed with the PR only.
	code := dbfx.Task(t, author, testutil.Cols{"runtime_id": claude, "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()"), "touched_paths": `["server/x.go"]`})
	testHandler.triggerCrossReview(ctx, mustTask(t, code), "https://github.com/org/repo/pull/7", "")
	var out struct{ Reviews []CrossReviewResponse }
	list := func() []CrossReviewResponse {
		testutil.Call(t, testHandler.ListCrossReviews, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/cross-reviews", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
		return out.Reviews
	}
	reviews := list()
	if len(reviews) != 1 || reviews[0].ReviewerAgentID != reviewer || reviews[0].ReviewerProvider != "codex" || reviews[0].ReviewOfTaskID != code || reviews[0].Status != "queued" || reviews[0].Report != nil {
		t.Fatalf("reviews = %+v", reviews)
	}
	review := mustTask(t, reviews[0].TaskID)
	if !strings.Contains(review.HandoffNote.String, "pull request https://github.com/org/repo/pull/7") || !strings.Contains(review.HandoffNote.String, "running on claude") || strings.Contains(review.HandoffNote.String, "server/x.go") {
		t.Fatalf("review brief = %q", review.HandoffNote.String)
	}
	if !review.ForceFreshSession {
		t.Fatal("the reviewer must start a fresh session, never the author's")
	}
	// Triggering again for the same run, or for the review run itself, adds nothing.
	testHandler.triggerCrossReview(ctx, mustTask(t, code), "https://github.com/org/repo/pull/7", "")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, reviews[0].TaskID)
	testHandler.triggerCrossReview(ctx, mustTask(t, reviews[0].TaskID), "https://github.com/org/repo/pull/8", "")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE review_of_task_id IS NOT NULL AND issue_id = $1`, issue); n != 1 {
		t.Fatalf("review runs = %d, want exactly one", n)
	}
	// The finished review leaves its structured report, parsed from its final output.
	testHandler.storeCrossReviewReport(ctx, mustTask(t, reviews[0].TaskID), "Read it.\n\n```review_report\n{\"verdict\":\"request_changes\",\"risks\":[\"retry path untested\"],\"questions\":[\"why not reuse mustTask?\"],\"suggestions\":[]}\n```")
	testHandler.storeCrossReviewReport(ctx, mustTask(t, reviews[0].TaskID), "again")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM task_message WHERE task_id = $1 AND type = 'review_report'`, reviews[0].TaskID); n != 1 {
		t.Fatalf("report messages = %d, want one", n)
	}
	reviews = list()
	if reviews[0].Report == nil || reviews[0].Report.Verdict != "request_changes" || len(reviews[0].Report.Risks) != 1 || len(reviews[0].Report.Questions) != 1 {
		t.Fatalf("report = %+v", reviews[0].Report)
	}
	// Retry: refused while the latest review is not failed; after a failure a new review run starts, once.
	testutil.Call(t, testHandler.RetryCrossReview, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issue+"/cross-reviews/retry", nil), "id", issue)).Want(http.StatusConflict)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', failure_reason = 'agent_error' WHERE id = $1`, reviews[0].TaskID)
	testutil.Call(t, testHandler.RetryCrossReview, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issue+"/cross-reviews/retry", nil), "id", issue)).Want(http.StatusCreated).JSON(&out)
	if len(out.Reviews) != 2 || out.Reviews[0].Status != "queued" || out.Reviews[0].ReviewOfTaskID != code || out.Reviews[1].Status != "failed" {
		t.Fatalf("after retry = %+v", out.Reviews)
	}
	testutil.Call(t, testHandler.RetryCrossReview, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issue+"/cross-reviews/retry", nil), "id", issue)).Want(http.StatusConflict)
}

type fakeDiffFetcher struct{ diff string }

func (f fakeDiffFetcher) FetchIssueDiff(context.Context, db.Issue, string) (string, error) {
	if f.diff == "" {
		return "", errors.New("no diff")
	}
	return f.diff, nil
}

// The brief embeds the diff when it can be read; the policy can switch the
// review off or exclude a project.
func TestCrossReviewEmbedsDiffAndHonoursPolicy(t *testing.T) {
	claude := providerRuntime(t, "claude")
	codex := providerRuntime(t, "codex")
	author := dbfx.Agent(t, "xr diff author", claude)
	reviewer := dbfx.Agent(t, "xr diff reviewer", codex)
	project := dbfx.Project(t, "xr project")
	issue := dbfx.Issue(t, "Cross review diff issue", testutil.Cols{"project_id": project})
	rememberSettings(t)
	quietOtherAgents(t, author, reviewer)
	prev := testHandler.DiffFetcher
	testHandler.DiffFetcher = fakeDiffFetcher{diff: "diff --git a/x.go b/x.go\n+fmt.Println(1)\n"}
	t.Cleanup(func() {
		testHandler.DiffFetcher = prev
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, author, reviewer)
	})
	ctx := context.Background()
	if cfg := service.CrossReviewSettings([]byte(`{"cross_review":{"enabled":false}}`)); cfg.Allows("") {
		t.Fatal("disabled policy must not allow")
	}
	if cfg := service.CrossReviewSettings([]byte(`{"cross_review":{"opt_out_project_ids":["p1",""]}}`)); !cfg.Enabled || cfg.Allows("p1") || !cfg.Allows("p2") {
		t.Fatalf("opt-out policy = %+v", cfg)
	}
	// Project opted out: no review.
	testutil.Call(t, testHandler.PutCrossReviewSettings, newRequest(http.MethodPut, "/api/cross-review-settings", map[string]any{"enabled": true, "opt_out_project_ids": []string{project}})).Want(http.StatusOK)
	code := dbfx.Task(t, author, testutil.Cols{"runtime_id": claude, "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()")})
	testHandler.triggerCrossReview(ctx, mustTask(t, code), "https://github.com/org/repo/pull/9", "")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE review_of_task_id = $1`, code); n != 0 {
		t.Fatal("an opted-out project must not be reviewed")
	}
	var got map[string]any
	testutil.Call(t, testHandler.GetCrossReviewSettings, newRequest(http.MethodGet, "/api/cross-review-settings", nil)).Want(http.StatusOK).JSON(&got)
	if ids, _ := got["opt_out_project_ids"].([]any); len(ids) != 1 || ids[0] != project {
		t.Fatalf("settings = %v", got)
	}
	testutil.Call(t, testHandler.PutCrossReviewSettings, newRequest(http.MethodPut, "/api/cross-review-settings", map[string]any{"enabled": true, "opt_out_project_ids": []string{"00000000-0000-0000-0000-000000000001"}})).Want(http.StatusUnprocessableEntity)
	// Back in: the review starts with the diff embedded in its brief.
	testutil.Call(t, testHandler.PutCrossReviewSettings, newRequest(http.MethodPut, "/api/cross-review-settings", map[string]any{"enabled": true, "opt_out_project_ids": []string{}})).Want(http.StatusOK)
	testHandler.triggerCrossReview(ctx, mustTask(t, code), "https://github.com/org/repo/pull/9", "")
	var reviewID string
	dbfx.QueryRow(t, `SELECT id FROM agent_task_queue WHERE review_of_task_id = $1`, code).Scan(&reviewID)
	review := mustTask(t, reviewID)
	if !strings.Contains(review.HandoffNote.String, "```diff\ndiff --git a/x.go b/x.go\n+fmt.Println(1)") || strings.Contains(review.HandoffNote.String, "Read the diff yourself") {
		t.Fatalf("brief = %q", review.HandoffNote.String)
	}
	// Disabled workspace-wide: nothing, even for another issue.
	testutil.Call(t, testHandler.PutCrossReviewSettings, newRequest(http.MethodPut, "/api/cross-review-settings", map[string]any{"enabled": false, "opt_out_project_ids": []string{}})).Want(http.StatusOK)
	other := dbfx.Issue(t, "Cross review disabled issue")
	code2 := dbfx.Task(t, author, testutil.Cols{"runtime_id": claude, "issue_id": other, "status": "completed", "completed_at": testutil.Raw("now()")})
	testHandler.triggerCrossReview(ctx, mustTask(t, code2), "", "feat/x")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE review_of_task_id = $1`, code2); n != 0 {
		t.Fatal("disabled policy must not review")
	}
}

func TestCrossReviewNeedsASecondProvider(t *testing.T) {
	claude := providerRuntime(t, "claude")
	author := dbfx.Agent(t, "xr solo author", claude)
	issue := dbfx.Issue(t, "Cross review solo issue")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id = $1`, author)
	})
	// The author is the only live agent: the feature is simply inactive.
	quietOtherAgents(t, author)
	code := dbfx.Task(t, author, testutil.Cols{"runtime_id": claude, "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()")})
	testHandler.triggerCrossReview(context.Background(), mustTask(t, code), "", "feat/solo")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE review_of_task_id = $1`, code); n != 0 {
		t.Fatal("one provider must mean no cross review")
	}
	var out struct{ Reviews []CrossReviewResponse }
	testutil.Call(t, testHandler.ListCrossReviews, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/cross-reviews", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
	if len(out.Reviews) != 0 {
		t.Fatalf("reviews = %+v", out.Reviews)
	}
}
