package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// DB-backed wiring of merge readiness (F10). The GitHub snapshot pipeline is
// disabled under test, so PR-level states are driven through the VCS provider
// tables, whose commit statuses need no snapshot manager; the GitHub matrix
// itself is covered in merge_readiness_todos_test.go.

func vcsConnection(t *testing.T) string {
	t.Helper()
	return dbfx.Insert(t, "vcs_connection", testutil.Cols{
		"workspace_id":             testWorkspaceID,
		"provider":                 "gitlab",
		"instance_url":             "https://gitlab.example.test",
		"account_login":            "bot",
		"access_token_encrypted":   "x",
		"webhook_secret_encrypted": "y",
	})
}

// vcsPR links an open VCS merge request to issueID with the given commit
// status states on its head.
func vcsPR(t *testing.T, connID, issueID string, number int, states ...string) string {
	t.Helper()
	sha := "sha-" + uuid.NewString()
	prID := dbfx.Insert(t, "vcs_pull_request", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"connection_id": connID,
		"provider":      "gitlab",
		"repo_owner":    "org",
		"repo_name":     "repo",
		"pr_number":     number,
		"title":         "MR " + uuid.NewString()[:8],
		"state":         "open",
		"html_url":      "https://gitlab.example.test/org/repo/-/merge_requests/1",
		"head_sha":      sha,
		"pr_created_at": testutil.Raw("now()"),
		"pr_updated_at": testutil.Raw("now()"),
	})
	dbfx.InsertNoID(t, "issue_vcs_pull_request", testutil.Cols{"issue_id": issueID, "pull_request_id": prID},
		"issue_id = $1 AND pull_request_id = $2", issueID, prID)
	for i, state := range states {
		dbfx.InsertNoID(t, "vcs_commit_status", testutil.Cols{
			"connection_id": connID, "sha": sha, "context": "ci/" + string(rune('a'+i)), "state": state,
		}, "connection_id = $1 AND sha = $2 AND context = $3", connID, sha, "ci/"+string(rune('a'+i)))
	}
	return prID
}

func callMergeReadiness(t *testing.T, issueID string) MergeReadinessResponse {
	t.Helper()
	var out MergeReadinessResponse
	testutil.Call(t, testHandler.GetIssueMergeReadiness, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/merge-readiness", nil), "id", issueID,
	)).Want(http.StatusOK).JSON(&out)
	return out
}

func blockerKinds(bs []MergeBlocker) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		out = append(out, b.Kind)
	}
	return out
}

func TestMergeReadinessWithoutPullRequest(t *testing.T) {
	issue := dbfx.Issue(t, "readiness no pr")
	got := callMergeReadiness(t, issue)
	if got.Ready || len(got.Blockers) != 1 || got.Blockers[0].Kind != blockerNoPR {
		t.Fatalf("readiness = %+v, want not ready with a single no_pr blocker", got)
	}
	if got.PRs == nil {
		t.Fatal("prs must be an empty list, not null")
	}
}

func TestMergeReadinessGreenPathAndThreadsAndTodos(t *testing.T) {
	conn := vcsConnection(t)
	issue := dbfx.Issue(t, "readiness green")
	vcsPR(t, conn, issue, 11, "passed", "passed")

	got := callMergeReadiness(t, issue)
	if !got.Ready || len(got.Blockers) != 0 {
		t.Fatalf("readiness = %+v, want ready", got)
	}
	if len(got.PRs) != 1 || !got.PRs[0].Ready || got.PRs[0].Checks.Passed != 2 || got.PRs[0].Source != "gitlab" {
		t.Fatalf("prs = %+v, want one ready gitlab MR with 2 passed checks", got.PRs)
	}

	// One open thread (root without resolution), one resolved thread (resolution
	// on the reply), one system comment that never counts.
	root := dbfx.Comment(t, issue, "- [ ] write docs\n- [x] done already\n```\n- [ ] not a todo\n```")
	resolvedRoot := dbfx.Comment(t, issue, "looks fine")
	dbfx.Comment(t, issue, "resolving", testutil.Cols{
		"parent_id": resolvedRoot, "resolved_at": testutil.Raw("now()"),
		"resolved_by_type": "member", "resolved_by_id": testUserID,
	})
	dbfx.Comment(t, issue, "- [ ] system noise", testutil.Cols{"author_type": "system", "author_id": testUserID})
	_ = root

	got = callMergeReadiness(t, issue)
	if got.Ready {
		t.Fatalf("readiness = %+v, want blocked by the thread and the todo", got)
	}
	if got.UnresolvedThreads != 1 || got.OpenTodos != 1 {
		t.Fatalf("threads = %d todos = %d, want 1 and 1", got.UnresolvedThreads, got.OpenTodos)
	}
	kinds := blockerKinds(got.Blockers)
	if len(kinds) != 2 || kinds[0] != blockerUnresolvedThreads || kinds[1] != blockerOpenTodos {
		t.Fatalf("blockers = %v, want [unresolved_threads open_todos]", kinds)
	}
	if got.Blockers[0].Count != 1 || got.Blockers[1].Count != 1 {
		t.Fatalf("blocker counts = %+v, want exact counts of 1", got.Blockers)
	}
}

func TestMergeReadinessChecksMatrixThroughVCS(t *testing.T) {
	conn := vcsConnection(t)
	failing := dbfx.Issue(t, "readiness failing")
	vcsPR(t, conn, failing, 21, "passed", "failed")
	if got := callMergeReadiness(t, failing); got.Ready || blockerKinds(got.Blockers)[0] != blockerChecksFailing {
		t.Fatalf("failing = %+v, want checks_failing", got)
	}

	pending := dbfx.Issue(t, "readiness pending")
	vcsPR(t, conn, pending, 22, "passed", "pending")
	if got := callMergeReadiness(t, pending); got.Ready || blockerKinds(got.Blockers)[0] != blockerChecksPending {
		t.Fatalf("pending = %+v, want checks_pending", got)
	}

	noChecks := dbfx.Issue(t, "readiness no checks")
	vcsPR(t, conn, noChecks, 23)
	if got := callMergeReadiness(t, noChecks); got.Ready || blockerKinds(got.Blockers)[0] != blockerChecksPending {
		t.Fatalf("no checks = %+v, want checks_pending, never passed", got)
	}
}

func TestMergeReadinessBlockingIssue(t *testing.T) {
	conn := vcsConnection(t)
	blocked := dbfx.Issue(t, "readiness blocked")
	vcsPR(t, conn, blocked, 31, "passed")
	openBlocker := dbfx.Issue(t, "readiness open blocker", testutil.Cols{"status": "in_progress"})
	doneBlocker := dbfx.Issue(t, "readiness done blocker", testutil.Cols{"status": "done"})
	callCreateDependency(t, openBlocker, blocked, "blocks").Want(http.StatusCreated)
	callCreateDependency(t, doneBlocker, blocked, "blocks").Want(http.StatusCreated)

	got := callMergeReadiness(t, blocked)
	if got.Ready || len(got.Blockers) != 1 || got.Blockers[0].Kind != blockerBlockingIssue {
		t.Fatalf("readiness = %+v, want exactly the open blocker", got)
	}
	var number int32
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id = $1`, openBlocker).Scan(&number)
	if got.Blockers[0].IssueIdentifier == "" || !containsInt(got.Blockers[0].IssueIdentifier, number) {
		t.Fatalf("blocker = %+v, want it to name the blocking issue's identifier", got.Blockers[0])
	}
}

func TestMergeReadinessOtherWorkspaceIs404(t *testing.T) {
	foreign := dbfx.Workspace(t, "Readiness foreign", "readiness-foreign-"+uuid.NewString())
	issue := dbfx.Issue(t, "readiness foreign issue", testutil.Cols{"workspace_id": foreign})
	testutil.Call(t, testHandler.GetIssueMergeReadiness, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issue+"/merge-readiness", nil), "id", issue,
	)).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.GetIssuePRStack, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issue+"/pr-stack", nil), "id", issue,
	)).Want(http.StatusNotFound)
}

func containsInt(s string, n int32) bool {
	return len(s) > 0 && s[len(s)-len(itoa(n)):] == itoa(n)
}

func itoa(n int32) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
