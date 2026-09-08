package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Reviewer choice (JEF-238): the worker and its (runtime, model) twins are
// out; another provider still wins; a pinned reviewer overrides the pair
// rule; an archived pinned reviewer falls back to the automatic choice.

// completedWorkerRun seeds the completed code run a review is triggered for.
func completedWorkerRun(t *testing.T, worker, runtimeID, issue string) string {
	t.Helper()
	return dbfx.Task(t, worker, testutil.Cols{"runtime_id": runtimeID, "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()"), "touched_paths": `["server/x.go"]`})
}

func reviewAgentFor(t *testing.T, workerTaskID string) string {
	t.Helper()
	var agentID string
	dbfx.QueryRow(t, `SELECT agent_id FROM agent_task_queue WHERE review_of_task_id = $1`, workerTaskID).Scan(&agentID)
	return agentID
}

func TestCrossReviewExcludesWorkerAndSamePair(t *testing.T) {
	claude := providerRuntime(t, "claude")
	codex := providerRuntime(t, "codex")
	worker := dbfx.Agent(t, "sel worker", claude, testutil.Cols{"model": "opus"})
	twin := dbfx.Agent(t, "sel twin", claude, testutil.Cols{"model": "opus"})
	sameProvider := dbfx.Agent(t, "sel same provider", claude, testutil.Cols{"model": "haiku"})
	otherProvider := dbfx.Agent(t, "sel other provider", codex)
	issue := dbfx.Issue(t, "Reviewer selection issue")
	quietOtherAgents(t, worker, twin, sameProvider, otherProvider)
	prevFetcher := testHandler.DiffFetcher
	testHandler.DiffFetcher = fakeDiffFetcher{}
	t.Cleanup(func() {
		testHandler.DiffFetcher = prevFetcher
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2, $3, $4)`, worker, twin, sameProvider, otherProvider)
	})
	ctx := context.Background()

	// Another provider beats the same-provider fallback.
	code := completedWorkerRun(t, worker, claude, issue)
	testHandler.triggerCrossReview(ctx, mustTask(t, code), "", "feat/select")
	if got := reviewAgentFor(t, code); got != otherProvider {
		t.Fatalf("reviewer = %s, want the other-provider agent %s", got, otherProvider)
	}

	// Without another provider the same (runtime, model) twin stays excluded,
	// but a same-provider agent on another model is eligible.
	code2 := completedWorkerRun(t, worker, claude, issue)
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, otherProvider)
	testHandler.triggerCrossReview(ctx, mustTask(t, code2), "", "feat/select-2")
	if got := reviewAgentFor(t, code2); got != sameProvider {
		t.Fatalf("fallback reviewer = %s, want the same-provider other-model agent %s", got, sameProvider)
	}

	// Alone with its twin, there is no reviewer at all.
	code3 := completedWorkerRun(t, worker, claude, issue)
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, sameProvider)
	testHandler.triggerCrossReview(ctx, mustTask(t, code3), "", "feat/select-3")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE review_of_task_id = $1`, code3); n != 0 {
		t.Fatal("the worker and its (runtime, model) twin must both be excluded")
	}
}

func TestCrossReviewPinnedReviewer(t *testing.T) {
	claude := providerRuntime(t, "claude")
	codex := providerRuntime(t, "codex")
	worker := dbfx.Agent(t, "pin worker", claude)
	pinned := dbfx.Agent(t, "pin reviewer", claude) // same provider AND pair: the pin overrides
	otherProvider := dbfx.Agent(t, "pin other", codex)
	project := dbfx.Project(t, "pinned review project")
	reviewConfigCleanup(t, project)
	issue := dbfx.Issue(t, "Pinned reviewer issue", testutil.Cols{"project_id": project})
	quietOtherAgents(t, worker, pinned, otherProvider)
	prevFetcher := testHandler.DiffFetcher
	testHandler.DiffFetcher = fakeDiffFetcher{}
	t.Cleanup(func() {
		testHandler.DiffFetcher = prevFetcher
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2, $3)`, worker, pinned, otherProvider)
	})
	putReviewConfig(t, project, map[string]any{"reviewer_agent_id": pinned}).Want(http.StatusOK)
	ctx := context.Background()

	code := completedWorkerRun(t, worker, claude, issue)
	testHandler.triggerCrossReview(ctx, mustTask(t, code), "", "feat/pinned")
	if got := reviewAgentFor(t, code); got != pinned {
		t.Fatalf("reviewer = %s, want the pinned agent %s", got, pinned)
	}

	// Pinned but archived: the automatic choice takes over.
	code2 := completedWorkerRun(t, worker, claude, issue)
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, pinned)
	testHandler.triggerCrossReview(ctx, mustTask(t, code2), "", "feat/pinned-2")
	if got := reviewAgentFor(t, code2); got != otherProvider {
		t.Fatalf("fallback reviewer = %s, want %s", got, otherProvider)
	}
}

// The review brief gains a Review checklist section and the per-item report
// contract when the project configures one; parse tolerates its absence.
func TestCrossReviewBriefChecklist(t *testing.T) {
	brief := buildCrossReviewBrief("claude", "branch feat/x", "", []string{"tests pass", "no stray logs"})
	for _, want := range []string{"Review checklist", "- tests pass", "- no stray logs", "checklist_results", "request_changes"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief missing %q:\n%s", want, brief)
		}
	}
	plain := buildCrossReviewBrief("claude", "branch feat/x", "", nil)
	if strings.Contains(plain, "checklist") {
		t.Fatalf("brief without checklist must stay the K15 one:\n%s", plain)
	}

	r := parseCrossReviewReport("```review_report\n{\"verdict\":\"request_changes\",\"risks\":[],\"questions\":[],\"suggestions\":[],\"checklist_results\":[{\"item\":\"tests pass\",\"pass\":false,\"note\":\"no test for the retry\"}]}\n```")
	if len(r.ChecklistResults) != 1 || r.ChecklistResults[0].Item != "tests pass" || r.ChecklistResults[0].Pass || r.ChecklistResults[0].Note == "" {
		t.Fatalf("checklist results = %+v", r.ChecklistResults)
	}
	r = parseCrossReviewReport("```review_report\n{\"verdict\":\"approve\"}\n```")
	if r.Verdict != "approve" || len(r.ChecklistResults) != 0 {
		t.Fatalf("absent checklist_results must be tolerated: %+v", r)
	}
}
