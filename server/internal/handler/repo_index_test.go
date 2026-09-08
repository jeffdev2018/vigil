package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Shared semantic repo index (K47). The chunking rules live on the daemon side
// (internal/daemon/repoindex_chunk_test.go); what is pinned here is the API
// contract: who may write to the index, what a write does to what a search
// returns, and what a claimed run ends up carrying.

// repoIndexRepoURL keeps every subtest of one run on its own repository, so a
// leftover row from an earlier run of the suite cannot satisfy an assertion.
func repoIndexRepoURL(t *testing.T) string {
	t.Helper()
	return "git@example.com:team/k47-" + uuid.NewString()[:8] + ".git"
}

// enableRepoIndex flips the workspace opt-in for one repository.
func enableRepoIndex(t *testing.T, repo string, enabled bool) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.PutRepoIndexSettings,
		newRequest(http.MethodPut, "/api/repo-index/settings", map[string]any{
			"repo_identifier": repo, "enabled": enabled,
		}))
}

// daemonRepoIndexCall drives one daemon endpoint with a daemon token.
func daemonRepoIndexCall(t *testing.T, h http.HandlerFunc, action string, body any) *testutil.Response {
	t.Helper()
	path := fmt.Sprintf("/api/daemon/workspaces/%s/repo-index/%s", testWorkspaceID, action)
	req := newDaemonTokenRequest(http.MethodPost, path, body, testWorkspaceID, "k47-daemon")
	return testutil.Call(t, h, withURLParam(req, "workspaceId", testWorkspaceID))
}

func TestRepoIndexDaemonWriteRequiresWorkspaceOptIn(t *testing.T) {
	repo := repoIndexRepoURL(t)
	t.Cleanup(func() { enableRepoIndex(t, repo, false) })

	// The opt-in is consent to copy source code into this database. Every write
	// verb refuses before it, not just the one that stores text.
	body := map[string]any{"repo_identifier": repo, "files": []map[string]string{{"path": "a.go", "hash": "h1"}}}
	daemonRepoIndexCall(t, testHandler.DiffRepoIndex, "diff", body).Want(http.StatusConflict)
	daemonRepoIndexCall(t, testHandler.UpsertRepoIndex, "upsert", map[string]any{
		"repo_identifier": repo, "commit": "c1",
		"chunks": []map[string]any{{"file_path": "a.go", "content": "package a", "content_hash": "h1"}},
	}).Want(http.StatusConflict)
	daemonRepoIndexCall(t, testHandler.PruneRepoIndex, "prune", map[string]any{
		"repo_identifier": repo, "commit": "c1", "present_paths": []string{"a.go"},
	}).Want(http.StatusConflict)

	enableRepoIndex(t, repo, true).Want(http.StatusOK)
	daemonRepoIndexCall(t, testHandler.DiffRepoIndex, "diff", body).Want(http.StatusOK)

	// A missing repo_identifier is a bad request, not a silent write to "".
	daemonRepoIndexCall(t, testHandler.DiffRepoIndex, "diff", map[string]any{"files": []any{}}).Want(http.StatusBadRequest)
}

func TestRepoIndexDiffUpsertSearchAndPrune(t *testing.T) {
	repo := repoIndexRepoURL(t)
	enableRepoIndex(t, repo, true).Want(http.StatusOK)
	t.Cleanup(func() { enableRepoIndex(t, repo, false) })

	files := []map[string]string{
		{"path": "server/claim.go", "hash": "hash-claim-1"},
		{"path": "web/list.tsx", "hash": "hash-list-1"},
	}
	var diff repoIndexDiffResponse
	daemonRepoIndexCall(t, testHandler.DiffRepoIndex, "diff", map[string]any{
		"repo_identifier": repo, "files": files,
	}).Want(http.StatusOK).JSON(&diff)
	if len(diff.Missing) != 2 || len(diff.Stale) != 0 {
		t.Fatalf("a repo with no chunks reports every file missing, got missing=%v stale=%v", diff.Missing, diff.Stale)
	}

	var stored repoIndexUpsertResponse
	daemonRepoIndexCall(t, testHandler.UpsertRepoIndex, "upsert", map[string]any{
		"repo_identifier": repo,
		"commit":          "commit-aaaaaaa",
		"chunks": []map[string]any{
			{
				"file_path": "server/claim.go", "symbol": "buildClaimedTaskResponse",
				"start_line": 10, "end_line": 40, "content_hash": "hash-claim-1",
				"content": "func buildClaimedTaskResponse(task Task) Response {\n\t// assembles the claim payload for the daemon\n}",
			},
			{
				"file_path": "web/list.tsx", "symbol": "IssueList",
				"start_line": 1, "end_line": 20, "content_hash": "hash-list-1",
				"content": "export const IssueList = () => renderIssueRows()",
			},
		},
	}).Want(http.StatusOK).JSON(&stored)
	if stored.Stored != 2 {
		t.Fatalf("stored %d chunks, want 2", stored.Stored)
	}

	// The point of storing hashes: the next pass asks again and is told nothing
	// changed. Without this the daemon would re-upload the whole repo forever.
	diff = repoIndexDiffResponse{}
	daemonRepoIndexCall(t, testHandler.DiffRepoIndex, "diff", map[string]any{
		"repo_identifier": repo, "files": files,
	}).Want(http.StatusOK).JSON(&diff)
	if len(diff.Missing) != 0 || len(diff.Stale) != 0 {
		t.Fatalf("unchanged files reported as work: missing=%v stale=%v", diff.Missing, diff.Stale)
	}
	// A changed hash IS work, and lands in stale rather than missing.
	diff = repoIndexDiffResponse{}
	daemonRepoIndexCall(t, testHandler.DiffRepoIndex, "diff", map[string]any{
		"repo_identifier": repo,
		"files":           []map[string]string{{"path": "server/claim.go", "hash": "hash-claim-2"}},
	}).Want(http.StatusOK).JSON(&diff)
	if len(diff.Stale) != 1 || diff.Stale[0] != "server/claim.go" {
		t.Fatalf("a changed hash must report stale, got missing=%v stale=%v", diff.Missing, diff.Stale)
	}

	// Search: a lexical query reaches the right chunk on a deployment with no
	// embeddings model, which is the default and must stay usable.
	indexer := testHandler.repoIndexer()
	hits, err := indexer.Query(context.Background(), parseUUID(testWorkspaceID), repo, "claim response daemon", 5)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(hits) == 0 || hits[0].FilePath != "server/claim.go" {
		t.Fatalf("lexical query did not rank the matching chunk first: %+v", hits)
	}
	if hits[0].Symbol != "buildClaimedTaskResponse" || hits[0].StartLine != 10 || hits[0].EndLine != 40 {
		t.Errorf("hint lost its location: %+v", hits[0])
	}
	if hits[0].Stale {
		t.Error("a chunk at the newest indexed commit reported stale")
	}

	// Staleness is measured against the repo's newest indexed commit, so a file
	// re-indexed at a later commit makes the untouched one stale.
	daemonRepoIndexCall(t, testHandler.UpsertRepoIndex, "upsert", map[string]any{
		"repo_identifier": repo,
		"commit":          "commit-bbbbbbb",
		"chunks": []map[string]any{{
			"file_path": "web/list.tsx", "symbol": "IssueList",
			"start_line": 1, "end_line": 25, "content_hash": "hash-list-2",
			"content": "export const IssueList = () => renderIssueRows()",
		}},
	}).Want(http.StatusOK)
	hits, err = indexer.Query(context.Background(), parseUUID(testWorkspaceID), repo, "claim response daemon", 5)
	if err != nil {
		t.Fatalf("query after re-index: %v", err)
	}
	if len(hits) == 0 || !hits[0].Stale {
		t.Fatalf("a chunk behind the newest indexed commit must be flagged stale: %+v", hits)
	}

	// Closing the pass clears that flag for the files the pass verified but did
	// not have to re-upload. Without this stamp an incremental pass would leave
	// every unchanged file labelled "older commit" forever, and the flag would
	// stop distinguishing anything.
	daemonRepoIndexCall(t, testHandler.PruneRepoIndex, "prune", map[string]any{
		"repo_identifier": repo, "commit": "commit-bbbbbbb",
		"present_paths": []string{"server/claim.go", "web/list.tsx"},
	}).Want(http.StatusNoContent)
	hits, err = indexer.Query(context.Background(), parseUUID(testWorkspaceID), repo, "claim response daemon", 5)
	if err != nil {
		t.Fatalf("query after the pass closed: %v", err)
	}
	if len(hits) == 0 || hits[0].Stale {
		t.Fatalf("a chunk the completed pass verified is still reported stale: %+v", hits)
	}

	// Re-indexing a file replaces its chunks rather than appending: the second
	// upsert of web/list.tsx must leave one chunk, not two.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM repo_index_chunk WHERE workspace_id = $1 AND repo_identifier = $2 AND file_path = 'web/list.tsx'`, testWorkspaceID, repo); n != 1 {
		t.Fatalf("re-index left %d chunks for one file, want 1 (replace, not append)", n)
	}

	// Prune drops what no longer exists — and refuses an empty list, because a
	// walk that produced nothing is a broken checkout, not an emptied repo.
	daemonRepoIndexCall(t, testHandler.PruneRepoIndex, "prune", map[string]any{
		"repo_identifier": repo, "commit": "commit-bbbbbbb", "present_paths": []string{},
	}).Want(http.StatusBadRequest)
	daemonRepoIndexCall(t, testHandler.PruneRepoIndex, "prune", map[string]any{
		"repo_identifier": repo, "commit": "commit-bbbbbbb", "present_paths": []string{"web/list.tsx"},
	}).Want(http.StatusNoContent)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM repo_index_chunk WHERE workspace_id = $1 AND repo_identifier = $2 AND file_path = 'server/claim.go'`, testWorkspaceID, repo); n != 0 {
		t.Fatalf("prune left %d chunks for a deleted file", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM repo_index_chunk WHERE workspace_id = $1 AND repo_identifier = $2`, testWorkspaceID, repo); n == 0 {
		t.Fatal("prune removed the files that are still present")
	}
}

func TestRepoIndexSettingsListPermissionsAndPurge(t *testing.T) {
	repo := repoIndexRepoURL(t)
	project := dbfx.Project(t, "k47 project "+uuid.NewString()[:6])
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    project,
		"workspace_id":  testWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  testutil.Raw(fmt.Sprintf(`'{"url": %q}'::jsonb`, repo)),
	})
	t.Cleanup(func() { enableRepoIndex(t, repo, false) })

	// A repository attached to a project is offered in Settings even before
	// anyone opts in — otherwise the toggle would have nothing to appear on.
	var settings repoIndexSettingsResponse
	testutil.Call(t, testHandler.GetRepoIndexSettings,
		newRequest(http.MethodGet, "/api/repo-index/settings", nil)).Want(http.StatusOK).JSON(&settings)
	found := false
	for _, r := range settings.Repos {
		if r.RepoIdentifier == repo {
			found = true
			if r.Enabled {
				t.Error("a repository is opted out until someone opts it in")
			}
		}
	}
	if !found {
		t.Fatalf("the project's repository is missing from the settings list: %+v", settings.Repos)
	}

	// A plain member may read the setting but not change it: enabling the index
	// starts copying the workspace's source code into the database.
	member := dbfx.User(t, "k47 member", "k47-member-"+uuid.NewString()[:8]+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "member")
	memberReq := newRequest(http.MethodPut, "/api/repo-index/settings", map[string]any{"repo_identifier": repo, "enabled": true})
	memberReq.Header.Set("X-User-ID", member)
	testutil.Call(t, testHandler.PutRepoIndexSettings, memberReq).Want(http.StatusForbidden)
	memberGet := newRequest(http.MethodGet, "/api/repo-index/settings", nil)
	memberGet.Header.Set("X-User-ID", member)
	testutil.Call(t, testHandler.GetRepoIndexSettings, memberGet).Want(http.StatusOK)

	enableRepoIndex(t, repo, true).Want(http.StatusOK)
	daemonRepoIndexCall(t, testHandler.UpsertRepoIndex, "upsert", map[string]any{
		"repo_identifier": repo, "commit": "commit-ccccccc",
		"chunks": []map[string]any{{
			"file_path": "a.go", "symbol": "A", "start_line": 1, "end_line": 3,
			"content_hash": "h", "content": "func A() {}",
		}},
	}).Want(http.StatusOK)

	settings = repoIndexSettingsResponse{}
	testutil.Call(t, testHandler.GetRepoIndexSettings,
		newRequest(http.MethodGet, "/api/repo-index/settings", nil)).Want(http.StatusOK).JSON(&settings)
	for _, r := range settings.Repos {
		if r.RepoIdentifier != repo {
			continue
		}
		if !r.Enabled || r.ChunkCount != 1 || r.LastIndexedCommit != "commit-ccccccc" || r.LastIndexedAt == "" {
			t.Fatalf("settings do not report the indexed state: %+v", r)
		}
	}

	// Turning it off must not leave the source text behind: "off" is the
	// workspace's answer about storage, not just about search.
	enableRepoIndex(t, repo, false).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM repo_index_chunk WHERE workspace_id = $1 AND repo_identifier = $2`, testWorkspaceID, repo); n != 0 {
		t.Fatalf("disabling left %d chunks in the table", n)
	}
}

func TestRepoIndexClaimCarriesHints(t *testing.T) {
	ctx := context.Background()
	repo := repoIndexRepoURL(t)
	enableRepoIndex(t, repo, true).Want(http.StatusOK)
	t.Cleanup(func() { enableRepoIndex(t, repo, false) })

	project := dbfx.Project(t, "k47 claim project "+uuid.NewString()[:6])
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    project,
		"workspace_id":  testWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  testutil.Raw(fmt.Sprintf(`'{"url": %q}'::jsonb`, repo)),
	})
	daemonRepoIndexCall(t, testHandler.UpsertRepoIndex, "upsert", map[string]any{
		"repo_identifier": repo, "commit": "commit-ddddddd",
		"chunks": []map[string]any{
			{
				"file_path": "server/scheduler/retry.go", "symbol": "scheduleRetry",
				"start_line": 40, "end_line": 70, "content_hash": "h1",
				"content": "func scheduleRetry(task Task) { // exponential backoff for a failed run\n}",
			},
			{
				"file_path": "web/theme.css", "symbol": "", "start_line": 1, "end_line": 10,
				"content_hash": "h2", "content": ".button { color: rebeccapurple; }",
			},
		},
	}).Want(http.StatusOK)

	runtimeID := createClaimReclaimRuntime(t, ctx, "k47 runtime "+uuid.NewString()[:6])
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "k47 agent "+uuid.NewString()[:6])
	dbfx.Exec(t, `UPDATE issue SET project_id = $1, title = $2, description = $3 WHERE id = $4`,
		project, "Retry scheduling is wrong", "A failed run should schedule a retry with exponential backoff.", issueID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})

	var claim struct {
		Task *struct {
			ID               string                 `json:"id"`
			RepoIndexHints   []RepoIndexHintContext `json:"repo_index_hints"`
			RepoIndexEnabled []string               `json:"repo_index_enabled"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime,
		withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "k47-daemon"), "runtimeId", runtimeID),
	).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil || claim.Task.ID != taskID {
		t.Fatalf("claim did not return the queued task: %+v", claim.Task)
	}
	// The enabled list is what gates the daemon's post-run pass; it must be
	// present even when nothing matched, or the index could never be refreshed.
	if len(claim.Task.RepoIndexEnabled) != 1 || claim.Task.RepoIndexEnabled[0] != repo {
		t.Fatalf("claim did not name the enabled repository: %v", claim.Task.RepoIndexEnabled)
	}
	if len(claim.Task.RepoIndexHints) == 0 {
		t.Fatal("claim carried no repo index hints for an issue whose project has an indexed repo")
	}
	top := claim.Task.RepoIndexHints[0]
	if top.FilePath != "server/scheduler/retry.go" || top.Symbol != "scheduleRetry" {
		t.Fatalf("the issue's own subject did not rank first: %+v", claim.Task.RepoIndexHints)
	}
	if top.RepoIdentifier != repo || top.StartLine != 40 || top.EndLine != 70 || top.Snippet == "" {
		t.Fatalf("hint is missing what makes it openable: %+v", top)
	}
}

func TestRepoIndexClaimSkipsRepoWithoutOptIn(t *testing.T) {
	ctx := context.Background()
	repo := repoIndexRepoURL(t)
	enableRepoIndex(t, repo, true).Want(http.StatusOK)

	project := dbfx.Project(t, "k47 optout project "+uuid.NewString()[:6])
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    project,
		"workspace_id":  testWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  testutil.Raw(fmt.Sprintf(`'{"url": %q}'::jsonb`, repo)),
	})
	daemonRepoIndexCall(t, testHandler.UpsertRepoIndex, "upsert", map[string]any{
		"repo_identifier": repo, "commit": "commit-eeeeeee",
		"chunks": []map[string]any{{
			"file_path": "server/scheduler/retry.go", "symbol": "scheduleRetry",
			"start_line": 1, "end_line": 5, "content_hash": "h1",
			"content": "func scheduleRetry(task Task) { // exponential backoff }",
		}},
	}).Want(http.StatusOK)
	// Opting out purges the chunks, so a later claim has nothing to offer even
	// though the project still points at the repository.
	enableRepoIndex(t, repo, false).Want(http.StatusOK)

	runtimeID := createClaimReclaimRuntime(t, ctx, "k47 optout runtime "+uuid.NewString()[:6])
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "k47 optout agent "+uuid.NewString()[:6])
	dbfx.Exec(t, `UPDATE issue SET project_id = $1, title = $2 WHERE id = $3`, project, "Retry scheduling is wrong", issueID)
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})

	var claim struct {
		Task *struct {
			RepoIndexHints   []RepoIndexHintContext `json:"repo_index_hints"`
			RepoIndexEnabled []string               `json:"repo_index_enabled"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime,
		withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "k47-daemon"), "runtimeId", runtimeID),
	).Want(http.StatusOK).JSON(&claim)
	if claim.Task == nil {
		t.Fatal("claim returned no task")
	}
	if len(claim.Task.RepoIndexEnabled) != 0 || len(claim.Task.RepoIndexHints) != 0 {
		t.Fatalf("an opted-out repository still reached the run: enabled=%v hints=%d",
			claim.Task.RepoIndexEnabled, len(claim.Task.RepoIndexHints))
	}
}
