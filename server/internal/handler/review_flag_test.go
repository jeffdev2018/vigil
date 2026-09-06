package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/integrations/vcs"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
)

// Review flags by severity (F06 / JEF-19). The sort order is enforced in SQL,
// so it is asserted here through the endpoint rather than over a helper.

func reviewFlagCleanup(t *testing.T) {
	t.Helper()
	syncIssueCounter(t)
	t.Cleanup(func() {
		// NOT t.Context(): Go cancels it just before cleanups run.
		testPool.Exec(context.Background(), `DELETE FROM review_flag WHERE workspace_id = $1`, testWorkspaceID)
	})
}

// reviewFlagVCSPR links a VCS pull request to a fresh issue, the cheapest
// linked-PR fixture the loader accepts.
func reviewFlagVCSPR(t *testing.T, headSHA string) (issueID, prID, connID string, prNumber int32) {
	t.Helper()
	connID = dbfx.Insert(t, "vcs_connection", testutil.Cols{
		"workspace_id":             testWorkspaceID,
		"provider":                 "gitlab",
		"instance_url":             "https://gitlab-" + uuid.NewString()[:8] + ".example.test",
		"account_login":            "bot",
		"access_token_encrypted":   "x",
		"webhook_secret_encrypted": "y",
	})
	issueID = dbfx.Issue(t, "review flag issue "+uuid.NewString()[:8])
	prNumber = int32(time.Now().UnixNano() % 100000)
	prID = dbfx.Insert(t, "vcs_pull_request", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"connection_id": connID,
		"provider":      "gitlab",
		"repo_owner":    "org",
		"repo_name":     "repo-" + uuid.NewString()[:8],
		"pr_number":     prNumber,
		"title":         "MR " + uuid.NewString()[:8],
		"state":         "open",
		"html_url":      "https://gitlab.example.test/org/repo/-/merge_requests/" + uuid.NewString()[:8],
		"head_sha":      headSHA,
		"pr_created_at": testutil.Raw("now()"),
		"pr_updated_at": testutil.Raw("now()"),
	})
	dbfx.InsertNoID(t, "issue_vcs_pull_request", testutil.Cols{"issue_id": issueID, "pull_request_id": prID},
		"issue_id = $1 AND pull_request_id = $2", issueID, prID)
	return issueID, prID, connID, prNumber
}

func addFlag(t *testing.T, issueID string, body map[string]any, headers ...string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/review-flags", body)
	if len(headers) > 0 {
		req = testutil.WithHeaders(req, headers...)
	}
	return testutil.Call(t, testHandler.CreateIssueReviewFlag, testutil.WithURLParams(req, "id", issueID))
}

func listFlags(t *testing.T, issueID, state string) reviewFlagListResponse {
	t.Helper()
	var out reviewFlagListResponse
	testutil.Call(t, testHandler.ListIssueReviewFlags, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/review-flags?state="+state, nil),
		"id", issueID)).Want(http.StatusOK).JSON(&out)
	return out
}

func setFlagState(t *testing.T, issueID, flagID, state string, headers ...string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPatch, "/api/issues/"+issueID+"/review-flags/"+flagID,
		map[string]any{"state": state})
	if len(headers) > 0 {
		req = testutil.WithHeaders(req, headers...)
	}
	return testutil.Call(t, testHandler.SetIssueReviewFlagState, testutil.WithURLParams(req, "id", issueID, "flagId", flagID))
}

func flagPayload(prID, severity, file string, line int, over map[string]any) map[string]any {
	body := map[string]any{
		"pr_id":      prID,
		"file_path":  file,
		"line_start": line,
		"severity":   severity,
		"title":      severity + " on " + file,
	}
	for k, v := range over {
		body[k] = v
	}
	return body
}

// A run and a human both write flags; the list comes back in the one order the
// server owns, and the counts describe only what is still open.
func TestReviewFlagsSortBySeverityThenConfidence(t *testing.T) {
	reviewFlagCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	agentID := dbfx.Agent(t, "flagger "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issueID, "status": "running"})
	asRun := []string{"X-Actor-Source", "task_token", "X-Agent-ID", agentID, "X-Task-ID", taskID}

	// Deliberately inserted worst-order-first: an info the author is certain
	// of, then a low-confidence bug. Severity has to win.
	addFlag(t, issueID, flagPayload(prID, "info", "z.go", 5, map[string]any{"confidence": 99}), asRun...).Want(http.StatusCreated)
	addFlag(t, issueID, flagPayload(prID, "bug", "a.go", 10, map[string]any{"confidence": 20}), asRun...).Want(http.StatusCreated)
	addFlag(t, issueID, flagPayload(prID, "bug", "b.go", 3, map[string]any{"confidence": 90}), asRun...).Want(http.StatusCreated)
	// No confidence at all sorts after every stated one of the same severity.
	addFlag(t, issueID, flagPayload(prID, "bug", "c.go", 1, nil), asRun...).Want(http.StatusCreated)
	addFlag(t, issueID, flagPayload(prID, "warning", "w.go", 7, map[string]any{"confidence": 50})).Want(http.StatusCreated)

	got := listFlags(t, issueID, "open")
	wantOrder := []string{"b.go", "a.go", "c.go", "w.go", "z.go"}
	if len(got.Flags) != len(wantOrder) {
		t.Fatalf("flags = %d, want %d: %+v", len(got.Flags), len(wantOrder), got.Flags)
	}
	for i, want := range wantOrder {
		if got.Flags[i].FilePath != want {
			t.Fatalf("position %d = %q, want %q — order is bug (conf desc, unstated last), warning, info",
				i, got.Flags[i].FilePath, want)
		}
	}
	if got.Counts != (ReviewFlagCounts{Bug: 3, Warning: 1, Info: 1}) {
		t.Errorf("counts = %+v", got.Counts)
	}

	// Authorship: the run stamped its agent and task, the member its user.
	if got.Flags[0].AuthorAgentID != agentID || got.Flags[0].TaskID != taskID || got.Flags[0].AuthorUserID != "" {
		t.Errorf("run-written flag author = %+v", got.Flags[0])
	}
	if got.Flags[3].AuthorUserID != testUserID || got.Flags[3].AuthorAgentID != "" {
		t.Errorf("member-written flag author = %+v", got.Flags[3])
	}
	// An omitted head defaults to the pull request's current one.
	if got.Flags[0].HeadSha != "head-1" {
		t.Errorf("head_sha = %q, want the PR's current head", got.Flags[0].HeadSha)
	}
	// "did not say" survives the round trip as null, not as 0.
	if got.Flags[2].Confidence != nil {
		t.Errorf("unstated confidence came back as %v, want null", *got.Flags[2].Confidence)
	}
}

// Resolve then reopen keeps the author and the revision the finding was made
// against; only the resolver moves.
func TestReviewFlagStateTransitionsKeepAuthorAndHead(t *testing.T) {
	reviewFlagCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	agentID := dbfx.Agent(t, "flagger "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issueID, "status": "running"})
	asRun := []string{"X-Actor-Source", "task_token", "X-Agent-ID", agentID, "X-Task-ID", taskID}

	var created ReviewFlagResponse
	addFlag(t, issueID, flagPayload(prID, "bug", "a.go", 12, map[string]any{"line_end": 18, "side": "old", "body": "the retry path"}),
		asRun...).Want(http.StatusCreated).JSON(&created)
	if created.LineEnd != 18 || created.Side != "old" || created.State != "open" {
		t.Fatalf("created = %+v", created)
	}

	// An agent may record a finding; only a human settles it.
	setFlagState(t, issueID, created.ID, "resolved", asRun...).Want(http.StatusForbidden)

	var resolved ReviewFlagResponse
	setFlagState(t, issueID, created.ID, "resolved").Want(http.StatusOK).JSON(&resolved)
	if resolved.State != "resolved" || resolved.ResolvedByType != "member" || resolved.ResolvedByID != testUserID || resolved.ResolvedAt == "" {
		t.Fatalf("resolved = %+v", resolved)
	}
	if resolved.AuthorAgentID != agentID || resolved.HeadSha != created.HeadSha {
		t.Fatalf("resolving rewrote the record: %+v", resolved)
	}
	// Resolved flags leave the open list and the counts, but `all` keeps them.
	if got := listFlags(t, issueID, "open"); len(got.Flags) != 0 || got.Counts.Bug != 0 {
		t.Fatalf("after resolve, open list = %+v", got)
	}
	if got := listFlags(t, issueID, "all"); len(got.Flags) != 1 || got.Counts.Bug != 0 {
		t.Fatalf("after resolve, all list = %+v", got)
	}

	var reopened ReviewFlagResponse
	setFlagState(t, issueID, created.ID, "open").Want(http.StatusOK).JSON(&reopened)
	if reopened.State != "open" || reopened.ResolvedByID != "" || reopened.ResolvedAt != "" || reopened.ResolvedByType != "" {
		t.Fatalf("reopened = %+v, want the resolver cleared", reopened)
	}
	if reopened.AuthorAgentID != agentID || reopened.HeadSha != created.HeadSha {
		t.Fatalf("reopening rewrote the record: %+v", reopened)
	}

	setFlagState(t, issueID, created.ID, "dismissed").Want(http.StatusOK)
	// `stale` is what a moving head does, never something a person asserts.
	setFlagState(t, issueID, created.ID, "stale").Want(http.StatusBadRequest)
	setFlagState(t, issueID, created.ID, "wat").Want(http.StatusBadRequest)
}

func TestReviewFlagRejectsBadRangesAndSeverities(t *testing.T) {
	reviewFlagCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	cases := []struct {
		name string
		over map[string]any
	}{
		{"end before start", map[string]any{"line_end": 3}},
		{"zero line", map[string]any{"line_start": 0}},
		{"confidence over 100", map[string]any{"confidence": 101}},
		{"confidence below zero", map[string]any{"confidence": -1}},
		{"unknown side", map[string]any{"side": "middle"}},
		{"empty title", map[string]any{"title": "   "}},
		{"empty file", map[string]any{"file_path": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addFlag(t, issueID, flagPayload(prID, "bug", "a.go", 10, tc.over)).Want(http.StatusBadRequest)
		})
	}
	// An unknown severity is refused at the boundary rather than stored: the
	// "unknown reads as info" rule is the CLIENT's, and applying it here too
	// would silently downgrade a typo'd `bugg` to info.
	addFlag(t, issueID, flagPayload(prID, "critical", "a.go", 10, nil)).Want(http.StatusBadRequest)
	// A pull request that is not linked to this issue does not exist for it.
	addFlag(t, issueID, flagPayload(uuid.NewString(), "bug", "a.go", 10, nil)).Want(http.StatusNotFound)
}

// One run cannot bury the reviewer under its own findings.
func TestReviewFlagCapsFlagsPerRun(t *testing.T) {
	reviewFlagCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")
	agentID := dbfx.Agent(t, "flagger "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issueID, "status": "running"})
	asRun := []string{"X-Actor-Source", "task_token", "X-Agent-ID", agentID, "X-Task-ID", taskID}

	// Seed the cap directly: a hundred round trips through the endpoint would
	// prove nothing the 101st does not.
	dbfx.Exec(t, `
		INSERT INTO review_flag (id, workspace_id, issue_id, pr_source, pr_id, head_sha,
		                         file_path, line_start, line_end, severity, title, task_id, author_agent_id)
		SELECT gen_random_uuid(), $1, $2, 'vcs', $3, 'head-1', 'f' || n || '.go', n, n, 'info', 'seeded', $4, $5
		FROM generate_series(1, $6) AS n
	`, testWorkspaceID, issueID, prID, taskID, agentID, reviewFlagsPerTaskCap)

	resp := addFlag(t, issueID, flagPayload(prID, "bug", "over.go", 1, nil), asRun...).Want(http.StatusUnprocessableEntity)
	if code, _ := resp.Map()["code"].(string); code != "too_many_flags" {
		t.Errorf("error code = %q, want too_many_flags", code)
	}
	// The cap is the RUN's, not the issue's: a human is never blocked by it.
	addFlag(t, issueID, flagPayload(prID, "bug", "human.go", 1, nil)).Want(http.StatusCreated)
	// Nor is a second run.
	otherTask := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issueID, "status": "running"})
	addFlag(t, issueID, flagPayload(prID, "bug", "second-run.go", 1, nil),
		"X-Actor-Source", "task_token", "X-Agent-ID", agentID, "X-Task-ID", otherTask).Want(http.StatusCreated)
}

// A flag belonging to another issue is indistinguishable from one that does
// not exist, on every endpoint that names one.
func TestReviewFlagOfAnotherIssueIsNotFound(t *testing.T) {
	reviewFlagCleanup(t)
	mineIssue, minePR, _, _ := reviewFlagVCSPR(t, "head-1")
	otherIssue, otherPR, _, _ := reviewFlagVCSPR(t, "head-1")

	var mine ReviewFlagResponse
	addFlag(t, mineIssue, flagPayload(minePR, "bug", "a.go", 1, nil)).Want(http.StatusCreated).JSON(&mine)
	addFlag(t, otherIssue, flagPayload(otherPR, "warning", "b.go", 2, nil)).Want(http.StatusCreated)

	// The other issue's list never shows it, and its counts never count it.
	got := listFlags(t, otherIssue, "all")
	if len(got.Flags) != 1 || got.Flags[0].FilePath != "b.go" || got.Counts.Bug != 0 {
		t.Fatalf("other issue's flags = %+v counts = %+v", got.Flags, got.Counts)
	}
	setFlagState(t, otherIssue, mine.ID, "resolved").Want(http.StatusNotFound)
	setFlagState(t, otherIssue, uuid.NewString(), "resolved").Want(http.StatusNotFound)
	// Cross-issue PR too: the other issue's PR is not linked to mine.
	addFlag(t, mineIssue, flagPayload(otherPR, "bug", "a.go", 1, nil)).Want(http.StatusNotFound)
}

// A moving head makes a flag stale — readable, no longer a claim about the
// current code — and never deletes it. Both sources, both real call sites.
func TestReviewFlagsGoStaleWhenTheHeadMoves(t *testing.T) {
	reviewFlagCleanup(t)
	ctx := context.Background()

	t.Run("vcs webhook", func(t *testing.T) {
		issueID, prID, connID, prNumber := reviewFlagVCSPR(t, "head-1")
		var open, settled ReviewFlagResponse
		addFlag(t, issueID, flagPayload(prID, "bug", "a.go", 1, nil)).Want(http.StatusCreated).JSON(&open)
		addFlag(t, issueID, flagPayload(prID, "info", "b.go", 2, nil)).Want(http.StatusCreated).JSON(&settled)
		setFlagState(t, issueID, settled.ID, "resolved").Want(http.StatusOK)

		conn, err := testHandler.Queries.GetVCSConnectionByID(ctx, util.MustParseUUID(connID))
		if err != nil {
			t.Fatalf("load connection: %v", err)
		}
		pr, err := testHandler.Queries.GetVCSPullRequestByID(ctx, util.MustParseUUID(prID))
		if err != nil {
			t.Fatalf("load pr: %v", err)
		}
		testHandler.mirrorVCSPullRequest(ctx, conn, vcs.PullRequestEvent{
			Action: "update", RepoOwner: pr.RepoOwner, RepoName: pr.RepoName, Number: prNumber,
			Title: pr.Title, State: "open", HTMLURL: pr.HtmlUrl, HeadSHA: "head-2",
			CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339),
			UpdatedAt: time.Now().Add(time.Minute).Format(time.RFC3339),
		})

		got := listFlags(t, issueID, "all")
		if len(got.Flags) != 2 {
			t.Fatalf("stale must never delete: %d flags left", len(got.Flags))
		}
		byID := map[string]ReviewFlagResponse{got.Flags[0].ID: got.Flags[0], got.Flags[1].ID: got.Flags[1]}
		if byID[open.ID].State != "stale" {
			t.Errorf("open flag on the old head = %q, want stale", byID[open.ID].State)
		}
		if byID[open.ID].HeadSha != "head-1" {
			t.Errorf("staling rewrote head_sha to %q — the flag still describes head-1", byID[open.ID].HeadSha)
		}
		// A settled flag is already answered; the move must not reopen the
		// question by dragging it back into the stale bucket.
		if byID[settled.ID].State != "resolved" {
			t.Errorf("resolved flag became %q on a head move", byID[settled.ID].State)
		}
		if got.Counts != (ReviewFlagCounts{}) {
			t.Errorf("stale flags still counted as open: %+v", got.Counts)
		}
	})

	t.Run("github snapshot", func(t *testing.T) {
		issueID := dbfx.Issue(t, "review flag gh "+uuid.NewString()[:8])
		prID := dbfx.Insert(t, "github_pull_request", testutil.Cols{
			"workspace_id":    testWorkspaceID,
			"installation_id": 1,
			"repo_owner":      "multica-ai",
			"repo_name":       "repo-" + uuid.NewString()[:8],
			"pr_number":       int32(time.Now().UnixNano() % 100000),
			"title":           "PR " + uuid.NewString()[:8],
			"state":           "open",
			"html_url":        "https://github.test/pr/" + uuid.NewString()[:8],
			"head_sha":        "gh-head-1",
			"pr_created_at":   testutil.Raw("now()"),
			"pr_updated_at":   testutil.Raw("now()"),
		})
		dbfx.InsertNoID(t, "issue_pull_request", testutil.Cols{"issue_id": issueID, "pull_request_id": prID},
			"issue_id = $1 AND pull_request_id = $2", issueID, prID)

		var flag ReviewFlagResponse
		addFlag(t, issueID, flagPayload(prID, "bug", "a.go", 1, nil)).Want(http.StatusCreated).JSON(&flag)
		if flag.PrSource != "github" {
			t.Fatalf("pr_source = %q, want github", flag.PrSource)
		}

		// The snapshot pipeline is where the server learns a GitHub head moved.
		dbfx.Exec(t, `UPDATE github_pull_request SET head_sha = 'gh-head-2' WHERE id = $1`, prID)
		testHandler.broadcastPRSnapshotApplied(ctx, util.MustParseUUID(prID))

		got := listFlags(t, issueID, "all")
		if len(got.Flags) != 1 || got.Flags[0].State != "stale" {
			t.Fatalf("after the head moved: %+v", got.Flags)
		}
		// Re-applying a snapshot for the head we are already on changes nothing.
		testHandler.broadcastPRSnapshotApplied(ctx, util.MustParseUUID(prID))
		if got := listFlags(t, issueID, "all"); len(got.Flags) != 1 || got.Flags[0].State != "stale" {
			t.Fatalf("re-applied snapshot: %+v", got.Flags)
		}
	})
}
