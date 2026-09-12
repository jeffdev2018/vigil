package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/integrations/ghdiff"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Narrative pull request walkthrough (F05 / JEF-16). The settings shape and
// the ingest normalisation (kind vocabulary, ordering, caps) are covered
// canonically in service/pr_walkthrough_test.go; this file covers the
// endpoints, the head fence, the completion hooks and authorization.

const walkthroughFixtureDiff = "diff --git a/api/client.go b/api/client.go\n" +
	"--- a/api/client.go\n+++ b/api/client.go\n@@ -12,7 +12,9 @@\n-old\n+new\n"

func prWalkthroughCleanup(t *testing.T) {
	t.Helper()
	syncIssueCounter(t)
	prev := testHandler.DiffFetcher
	testHandler.DiffFetcher = fakeDiffFetcher{diff: walkthroughFixtureDiff}
	t.Cleanup(func() {
		testHandler.DiffFetcher = prev
		// NOT context.Background(): Go cancels it just before cleanups run.
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM pr_walkthrough WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `UPDATE workspace SET settings = settings - 'pr_walkthrough' WHERE id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE workspace_id = $1 AND action LIKE 'pr_walkthrough.%'`, testWorkspaceID)
	})
}

// walkthroughVCSPR creates a VCS pull request linked to a fresh issue and
// returns both ids plus the head it is currently on.
func walkthroughVCSPR(t *testing.T, headSHA string) (issueID, prID string) {
	t.Helper()
	connID := dbfx.Insert(t, "vcs_connection", testutil.Cols{
		"workspace_id":             testWorkspaceID,
		"provider":                 "gitlab",
		"instance_url":             "https://gitlab-" + uuid.NewString()[:8] + ".example.test",
		"account_login":            "bot",
		"access_token_encrypted":   "x",
		"webhook_secret_encrypted": "y",
	})
	issueID = dbfx.Issue(t, "walkthrough issue "+uuid.NewString()[:8])
	prID = dbfx.Insert(t, "vcs_pull_request", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"connection_id": connID,
		"provider":      "gitlab",
		"repo_owner":    "org",
		"repo_name":     "repo",
		"pr_number":     7,
		"title":         "MR " + uuid.NewString()[:8],
		"state":         "open",
		"html_url":      "https://gitlab.example.test/org/repo/-/merge_requests/" + uuid.NewString()[:8],
		"head_sha":      headSHA,
		"pr_created_at": testutil.Raw("now()"),
		"pr_updated_at": testutil.Raw("now()"),
	})
	dbfx.InsertNoID(t, "issue_vcs_pull_request", testutil.Cols{"issue_id": issueID, "pull_request_id": prID},
		"issue_id = $1 AND pull_request_id = $2", issueID, prID)
	return issueID, prID
}

func enablePrWalkthrough(t *testing.T) string {
	t.Helper()
	agentID := dbfx.Agent(t, "walkthrough agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	testutil.Call(t, testHandler.PutPrWalkthroughSettings,
		newRequest(http.MethodPut, "/api/pr-walkthrough/settings",
			map[string]any{"enabled": true, "agent_id": agentID})).Want(http.StatusOK)
	return agentID
}

func getWalkthrough(t *testing.T, issueID, prID string) PrWalkthroughResponse {
	t.Helper()
	var out PrWalkthroughResponse
	testutil.Call(t, testHandler.GetIssuePrWalkthrough, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/pull-requests/"+prID+"/walkthrough", nil),
		"id", issueID, "prId", prID)).Want(http.StatusOK).JSON(&out)
	return out
}

func refreshWalkthrough(t *testing.T, issueID, prID string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.RefreshIssuePrWalkthrough, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/pull-requests/"+prID+"/walkthrough/refresh", nil),
		"id", issueID, "prId", prID))
}

// completeWalkthroughTask drives the daemon completion callback, which is
// where the settle hook hangs.
func completeWalkthroughTask(t *testing.T, taskID, output string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID)
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": output}, testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("complete task %s: %d %s", taskID, w.Code, w.Body.String())
	}
}

func failWalkthroughTask(t *testing.T, taskID, reason string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID)
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/fail",
		map[string]any{"error": reason}, testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.FailTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("fail task %s: %d %s", taskID, w.Code, w.Body.String())
	}
}

func walkthroughFence(t *testing.T, groups []service.PrWalkthroughGroup) string {
	t.Helper()
	raw, err := json.Marshal(service.PrWalkthroughReport{Groups: groups})
	if err != nil {
		t.Fatal(err)
	}
	return "Read the diff.\n\n```pr_walkthrough\n" + string(raw) + "\n```\n"
}

func refreshTaskID(t *testing.T, resp *testutil.Response) string {
	t.Helper()
	var out prWalkthroughRefreshResponse
	resp.Want(http.StatusAccepted).JSON(&out)
	if out.TaskID == "" {
		t.Fatal("refresh returned no task id")
	}
	return out.TaskID
}

func TestPrWalkthroughSettingsAreAdminOnlyAndValidated(t *testing.T) {
	prWalkthroughCleanup(t)
	agentID := dbfx.Agent(t, "wt agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))

	var out service.PrWalkthroughSettings
	testutil.Call(t, testHandler.GetPrWalkthroughSettings,
		newRequest(http.MethodGet, "/api/pr-walkthrough/settings", nil)).Want(http.StatusOK).JSON(&out)
	if out.Enabled || out.AgentID != "" {
		t.Fatalf("defaults: %+v — this spends agent budget per push and must be opt-in", out)
	}

	member := dbfx.User(t, "wt member", "wt-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "member")
	testutil.Call(t, testHandler.GetPrWalkthroughSettings,
		newRequestAs(member, http.MethodGet, "/api/pr-walkthrough/settings", nil)).Want(http.StatusOK)
	testutil.Call(t, testHandler.PutPrWalkthroughSettings,
		newRequestAs(member, http.MethodPut, "/api/pr-walkthrough/settings",
			map[string]any{"enabled": true, "agent_id": agentID})).Want(http.StatusForbidden)

	// Enabling needs an agent, and it has to be one of this workspace's.
	testutil.Call(t, testHandler.PutPrWalkthroughSettings,
		newRequest(http.MethodPut, "/api/pr-walkthrough/settings",
			map[string]any{"enabled": true})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutPrWalkthroughSettings,
		newRequest(http.MethodPut, "/api/pr-walkthrough/settings",
			map[string]any{"enabled": true, "agent_id": uuid.NewString()})).Want(http.StatusBadRequest)

	testutil.Call(t, testHandler.PutPrWalkthroughSettings,
		newRequest(http.MethodPut, "/api/pr-walkthrough/settings",
			map[string]any{"enabled": true, "agent_id": agentID})).Want(http.StatusOK).JSON(&out)
	if !out.Enabled || out.AgentID != agentID {
		t.Fatalf("saved settings: %+v", out)
	}
}

func TestPrWalkthroughReadsPendingUntilItsHeadSettles(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)
	issueID, prID := walkthroughVCSPR(t, "head-1")

	// No row yet: pending is a real answer, not a 404.
	if got := getWalkthrough(t, issueID, prID); got.State != "pending" || got.HeadSha != "head-1" || len(got.Groups) != 0 {
		t.Fatalf("before any run: %+v", got)
	}

	taskID := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))
	if got := getWalkthrough(t, issueID, prID); got.State != "pending" {
		t.Fatalf("while the run is out: %+v", got)
	}

	completeWalkthroughTask(t, taskID, walkthroughFence(t, []service.PrWalkthroughGroup{
		{Title: "generated client", Kind: "generated", Rationale: "regenerated"},
		{Title: "the change", Kind: "core", Rationale: "the behaviour moved", Files: []service.PrWalkthroughFile{{
			Path: "api/client.go", Hunks: []service.PrWalkthroughHunk{{OldStart: 12, NewStart: 12, Lines: "@@", Explanation: "returns the error"}},
		}}},
	}))

	got := getWalkthrough(t, issueID, prID)
	if got.State != "ready" || len(got.Groups) != 2 {
		t.Fatalf("after completion: %+v", got)
	}
	// Fixed render order: core before generated, whatever order the run used.
	if got.Groups[0].Kind != "core" || got.Groups[1].Kind != "generated" {
		t.Errorf("groups = %s/%s, want core then generated", got.Groups[0].Kind, got.Groups[1].Kind)
	}
	if len(got.Groups[0].Files) != 1 || got.Groups[0].Files[0].Hunks[0].Explanation != "returns the error" {
		t.Errorf("hunk explanation lost: %+v", got.Groups[0].Files)
	}
	if got.GeneratedAt == "" {
		t.Error("ready walkthrough carries no generated_at")
	}
}

func TestPrWalkthroughEnqueuesAtMostOneRunPerHead(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)
	issueID, prID := walkthroughVCSPR(t, "head-1")

	first := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))
	// A second request for the same head is refused at the unique index rather
	// than silently enqueued again.
	refreshWalkthrough(t, issueID, prID).Want(http.StatusConflict)
	if n := dbfx.Count(t, `SELECT count(*) FROM pr_walkthrough WHERE pr_id = $1`, prID); n != 1 {
		t.Fatalf("rows for one head = %d, want 1", n)
	}
	completeWalkthroughTask(t, first, walkthroughFence(t, []service.PrWalkthroughGroup{{Kind: "core", Title: "one"}}))

	// A new head is a new row and a new run.
	dbfx.Exec(t, `UPDATE vcs_pull_request SET head_sha = 'head-2' WHERE id = $1`, prID)
	second := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))
	if second == first {
		t.Fatal("moving the head reused the old run")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM pr_walkthrough WHERE pr_id = $1`, prID); n != 2 {
		t.Fatalf("rows after the head moved = %d, want 2", n)
	}
	if got := getWalkthrough(t, issueID, prID); got.HeadSha != "head-2" || got.State != "pending" {
		t.Fatalf("reader followed the wrong head: %+v", got)
	}
}

// A pull request that moves WHILE its walkthrough is queued hits the task
// queue's own one-pending-run-per-(issue, agent) rule, not our head fence. The
// new head must not be stranded by that: settling the first run picks it up.
func TestPrWalkthroughChasesAHeadThatMovedDuringTheRun(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)
	issueID, prID := walkthroughVCSPR(t, "head-1")
	first := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))

	dbfx.Exec(t, `UPDATE vcs_pull_request SET head_sha = 'head-2' WHERE id = $1`, prID)
	// While the first run is queued the second head cannot get its own run yet.
	refreshWalkthrough(t, issueID, prID).Want(http.StatusConflict)

	completeWalkthroughTask(t, first, walkthroughFence(t, []service.PrWalkthroughGroup{{Kind: "core", Title: "one"}}))

	// Settling the first run started the second head's own run.
	var state string
	var taskID *string
	dbfx.QueryRow(t, `SELECT state, task_id::text FROM pr_walkthrough WHERE pr_id = $1 AND head_sha = 'head-2'`, prID).
		Scan(&state, &taskID)
	if state != "pending" || taskID == nil || *taskID == "" {
		t.Fatalf("head-2 state=%q task=%v — a head that moved mid-run was stranded", state, taskID)
	}
	completeWalkthroughTask(t, *taskID, walkthroughFence(t, []service.PrWalkthroughGroup{{Kind: "core", Title: "two"}}))
	if got := getWalkthrough(t, issueID, prID); got.State != "ready" || got.Groups[0].Title != "two" {
		t.Fatalf("chased head: %+v", got)
	}
}

func TestPrWalkthroughStaleCompletionNeverOverwritesTheCurrentHead(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)
	issueID, prID := walkthroughVCSPR(t, "head-1")

	stale := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))
	// The head moves and gets its own run once the first one is out of the
	// queue's way; the stale run is still in flight the whole time.
	dbfx.Exec(t, `UPDATE vcs_pull_request SET head_sha = 'head-2' WHERE id = $1`, prID)

	// The run for the OLD head lands. It settles ITS row and never touches the
	// current head's.
	completeWalkthroughTask(t, stale, walkthroughFence(t, []service.PrWalkthroughGroup{
		{Title: "old story", Kind: "core"},
	}))
	if got := getWalkthrough(t, issueID, prID); got.HeadSha != "head-2" || got.State != "pending" || len(got.Groups) != 0 {
		t.Fatalf("a stale completion leaked into the current head: %+v", got)
	}
	var staleState, staleTitle string
	dbfx.QueryRow(t, `SELECT state, groups->0->>'title' FROM pr_walkthrough WHERE pr_id = $1 AND head_sha = 'head-1'`, prID).
		Scan(&staleState, &staleTitle)
	if staleState != "ready" || staleTitle != "old story" {
		t.Errorf("stale row = %q/%q, want its own narrative kept as a record", staleState, staleTitle)
	}

	// The current head's own run is what fills it.
	var currentTask string
	dbfx.QueryRow(t, `SELECT task_id::text FROM pr_walkthrough WHERE pr_id = $1 AND head_sha = 'head-2'`, prID).Scan(&currentTask)
	completeWalkthroughTask(t, currentTask, walkthroughFence(t, []service.PrWalkthroughGroup{
		{Title: "new story", Kind: "core"},
	}))
	got := getWalkthrough(t, issueID, prID)
	if got.State != "ready" || got.HeadSha != "head-2" || got.Groups[0].Title != "new story" {
		t.Fatalf("current head after its own run: %+v", got)
	}
}

func TestPrWalkthroughMalformedAndCrashedRunsSettleFailed(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)

	// An answer with no readable fence is a failure the reviewer can retry,
	// not a walkthrough stuck pending forever.
	issueID, prID := walkthroughVCSPR(t, "head-malformed")
	taskID := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))
	completeWalkthroughTask(t, taskID, "I had a look and it seems fine to me.")
	if got := getWalkthrough(t, issueID, prID); got.State != "failed" || got.Error == "" {
		t.Fatalf("malformed answer: %+v", got)
	}

	// A fence carrying invalid JSON is the same failure.
	issueID2, prID2 := walkthroughVCSPR(t, "head-badjson")
	taskID2 := refreshTaskID(t, refreshWalkthrough(t, issueID2, prID2))
	completeWalkthroughTask(t, taskID2, "```pr_walkthrough\n{not json}\n```")
	if got := getWalkthrough(t, issueID2, prID2); got.State != "failed" {
		t.Fatalf("malformed fence json: %+v", got)
	}

	// A crashed run releases its head too.
	issueID3, prID3 := walkthroughVCSPR(t, "head-crash")
	taskID3 := refreshTaskID(t, refreshWalkthrough(t, issueID3, prID3))
	failWalkthroughTask(t, taskID3, "the runtime died")
	got := getWalkthrough(t, issueID3, prID3)
	if got.State != "failed" || got.Error == "" {
		t.Fatalf("crashed run: %+v", got)
	}
}

// Acceptance 3: an over-cap diff produces a partial walkthrough with the count
// of what was left out — never an error. A reviewer who can see 400 of 900
// files, and is told so, is better served than one who sees a red box.
func TestPrWalkthroughOverCapDiffTruncatesInsteadOfFailing(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)

	var big strings.Builder
	for i := 0; i < ghdiff.MaxFiles+25; i++ {
		big.WriteString("diff --git a/f" + strconv.Itoa(i) + ".go b/f" + strconv.Itoa(i) + ".go\n")
		big.WriteString("@@ -1 +1 @@\n-old\n+new\n")
	}
	prev := testHandler.DiffFetcher
	testHandler.DiffFetcher = fakeDiffFetcher{diff: big.String()}
	t.Cleanup(func() { testHandler.DiffFetcher = prev })

	issueID, prID := walkthroughVCSPR(t, "head-huge")
	taskID := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))

	// The cap is recorded when the brief is built, so it survives even if the
	// run never comes back.
	pending := getWalkthrough(t, issueID, prID)
	if !pending.Truncated || pending.OmittedFiles != 25 {
		t.Fatalf("pending: truncated=%v omitted=%d, want true/25", pending.Truncated, pending.OmittedFiles)
	}

	completeWalkthroughTask(t, taskID, walkthroughFence(t, []service.PrWalkthroughGroup{
		{Title: "what I could read", Kind: "core"},
	}))
	got := getWalkthrough(t, issueID, prID)
	if got.State != "ready" {
		t.Fatalf("an over-cap diff must still produce a walkthrough: %+v", got)
	}
	if !got.Truncated || got.OmittedFiles != 25 {
		t.Errorf("after completion: truncated=%v omitted=%d, want true/25", got.Truncated, got.OmittedFiles)
	}
}

func TestPrWalkthroughRefreshIsRefusedWhenDisabled(t *testing.T) {
	prWalkthroughCleanup(t)
	issueID, prID := walkthroughVCSPR(t, "head-1")

	// Never configured: nothing is enqueued and the caller is told why.
	resp := refreshWalkthrough(t, issueID, prID).Want(http.StatusConflict)
	if body := resp.Body.String(); !strings.Contains(body, "walkthrough_disabled") {
		t.Fatalf("409 body = %q, want the walkthrough_disabled code", body)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM pr_walkthrough WHERE pr_id = $1`, prID); n != 0 {
		t.Fatalf("a disabled workspace claimed %d heads", n)
	}
	// The read still answers, so the panel renders an empty state rather than
	// an error.
	if got := getWalkthrough(t, issueID, prID); got.State != "pending" {
		t.Fatalf("read while disabled: %+v", got)
	}
}

// An unreadable diff is the provider's problem, not a server crash: the refresh
// says so with a 502 and the row is settled failed with the reason, so the
// reviewer can retry once the provider is back.
func TestPrWalkthroughRefreshAnswers502WhenTheDiffCannotBeRead(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)
	issueID, prID := walkthroughVCSPR(t, "head-502")
	prev := testHandler.DiffFetcher
	testHandler.DiffFetcher = fakeDiffFetcher{}
	t.Cleanup(func() { testHandler.DiffFetcher = prev })

	resp := refreshWalkthrough(t, issueID, prID).Want(http.StatusBadGateway)
	if body := resp.Body.String(); !strings.Contains(body, "diff_unavailable") {
		t.Fatalf("502 body = %q, want the diff_unavailable code", body)
	}
	if got := getWalkthrough(t, issueID, prID); got.State != "failed" || !strings.Contains(got.Error, "no diff") {
		t.Fatalf("row after unreadable diff: %+v", got)
	}
}

// Both endpoints answer only from the issue's OWN link list, so a pull request
// that exists but is not linked here is indistinguishable from one that does
// not exist. That is the authorization these handlers own; workspace
// membership is gated once, by RequireWorkspaceMember on the whole /api/issues
// group in cmd/server/router.go.
func TestPrWalkthroughAnswersOnlyForPullRequestsLinkedToTheIssue(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)
	issueID, prID := walkthroughVCSPR(t, "head-1")
	otherIssue := dbfx.Issue(t, "unrelated "+uuid.NewString()[:8])

	for _, tc := range []struct {
		name        string
		issue, pull string
	}{
		{"pr linked to another issue", otherIssue, prID},
		{"pr that does not exist", issueID, uuid.NewString()},
		{"pr id that is not a uuid", issueID, "not-a-uuid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Call(t, testHandler.GetIssuePrWalkthrough, testutil.WithURLParams(
				newRequest(http.MethodGet, "/x", nil), "id", tc.issue, "prId", tc.pull)).Want(http.StatusNotFound)
			testutil.Call(t, testHandler.RefreshIssuePrWalkthrough, testutil.WithURLParams(
				newRequest(http.MethodPost, "/x", nil), "id", tc.issue, "prId", tc.pull)).Want(http.StatusNotFound)
		})
	}
	// The linked one still works, so the 404s above are about the link and not
	// about the fixture being broken.
	if got := getWalkthrough(t, issueID, prID); got.State != "pending" {
		t.Fatalf("linked pull request: %+v", got)
	}
}

// A settled head is re-claimable: retry after a failure and regenerate on a
// ready walkthrough are the same button, and neither may be blocked by the
// fence that exists to stop DUPLICATE runs.
func TestPrWalkthroughSettledHeadCanBeRegenerated(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)
	issueID, prID := walkthroughVCSPR(t, "head-1")

	first := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))
	completeWalkthroughTask(t, first, walkthroughFence(t, []service.PrWalkthroughGroup{
		{Title: "first pass", Kind: "core"},
	}))

	second := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))
	if second == first {
		t.Fatal("regenerate reused the settled run")
	}
	// Still one row for the head, and the previous narrative survives until the
	// new run replaces it.
	if n := dbfx.Count(t, `SELECT count(*) FROM pr_walkthrough WHERE pr_id = $1`, prID); n != 1 {
		t.Fatalf("rows = %d, want the head re-claimed in place", n)
	}
	if got := getWalkthrough(t, issueID, prID); got.State != "pending" || len(got.Groups) != 1 {
		t.Fatalf("during regeneration: %+v — the old narrative must survive", got)
	}
	completeWalkthroughTask(t, second, walkthroughFence(t, []service.PrWalkthroughGroup{
		{Title: "second pass", Kind: "core"},
	}))
	if got := getWalkthrough(t, issueID, prID); got.State != "ready" || got.Groups[0].Title != "second pass" {
		t.Fatalf("after regeneration: %+v", got)
	}
}

func TestPrWalkthroughRunIsReviewLikeSoRoutingStatsIgnoreIt(t *testing.T) {
	prWalkthroughCleanup(t)
	enablePrWalkthrough(t)
	issueID, prID := walkthroughVCSPR(t, "head-1")
	taskID := refreshTaskID(t, refreshWalkthrough(t, issueID, prID))

	var legRole string
	dbfx.QueryRow(t, `SELECT leg_role FROM agent_task_queue WHERE id = $1`, taskID).Scan(&legRole)
	if legRole != service.LegRolePrWalkthrough {
		t.Fatalf("leg_role = %q, want %q — an unstamped run counts as a sample of the worker's task class",
			legRole, service.LegRolePrWalkthrough)
	}
}
