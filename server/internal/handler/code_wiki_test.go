package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// F26 (JEF-22): the code wiki's write path. Publication is atomic, a page must
// cite files the run announced, and detaching the repository resource takes the
// wiki with it.

// wikiFixture makes a project the fixture user leads, with one github_repo
// resource, and returns both ids.
func wikiFixture(t *testing.T, repoURL string) (projectID, resourceID string) {
	t.Helper()
	projectID = dbfx.Project(t, "Wiki "+uuid.NewString()[:6], testutil.Cols{
		"lead_type": "member",
		"lead_id":   testUserID,
	})
	resourceID = dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    projectID,
		"workspace_id":  testWorkspaceID,
		"resource_type": "github_repo",
		"resource_ref":  testutil.Raw(`'{"url":"` + repoURL + `"}'::jsonb`),
	})
	dbfx.Cleanup(t, `DELETE FROM code_wiki_page WHERE project_resource_id = $1`, resourceID)
	dbfx.Cleanup(t, `DELETE FROM code_wiki_snapshot WHERE project_resource_id = $1`, resourceID)
	return projectID, resourceID
}

func wikiRequest(t *testing.T, method, path string, body any, params ...string) *http.Request {
	t.Helper()
	return testutil.WithURLParams(newRequest(method, path, body), params...)
}

// startWikiSnapshot claims a build and announces an inventory.
func startWikiSnapshot(t *testing.T, projectID string, paths []string) CodeWikiSnapshotResponse {
	t.Helper()
	var snapshot CodeWikiSnapshotResponse
	testutil.Call(t, testHandler.CreateProjectCodeWikiSnapshot,
		wikiRequest(t, http.MethodPost, "/api/projects/x/wiki/snapshots", map[string]any{
			"commit_sha": "c0ffee1",
			"repo_paths": paths,
		}, "id", projectID),
	).Want(http.StatusCreated).JSON(&snapshot)
	return snapshot
}

func TestCodeWikiPublicationIsAtomic(t *testing.T) {
	projectID, _ := wikiFixture(t, "https://github.com/acme/atomic")
	snapshot := startWikiSnapshot(t, projectID, []string{"src/app.py", "README.md"})

	writePage := func(snapshotID, slug string) *testutil.Response {
		return testutil.Call(t, testHandler.CreateProjectCodeWikiPage,
			wikiRequest(t, http.MethodPost, "/api/projects/x/wiki/snapshots/y/pages", map[string]any{
				"slug": slug, "title": "Overview " + slug, "content": "# " + slug,
				"citations": []map[string]any{{"path": "src/app.py"}},
			}, "id", projectID, "sid", snapshotID),
		)
	}
	writePage(snapshot.ID, "overview").Want(http.StatusCreated)

	// A build that has not been published is invisible.
	var before CodeWikiResponse
	testutil.Call(t, testHandler.GetProjectCodeWiki,
		wikiRequest(t, http.MethodGet, "/api/projects/x/wiki", nil, "id", projectID),
	).Want(http.StatusOK).JSON(&before)
	if before.Snapshot != nil {
		t.Fatalf("an unpublished build must not be served: %+v", before.Snapshot)
	}
	if !before.Building {
		t.Fatal("the panel must be able to tell that a generation is running")
	}

	testutil.Call(t, testHandler.PublishProjectCodeWikiSnapshot,
		wikiRequest(t, http.MethodPost, "/api/projects/x/wiki/snapshots/y/publish", nil, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusOK)

	var published CodeWikiResponse
	testutil.Call(t, testHandler.GetProjectCodeWiki,
		wikiRequest(t, http.MethodGet, "/api/projects/x/wiki", nil, "id", projectID),
	).Want(http.StatusOK).JSON(&published)
	if published.Snapshot == nil || published.Snapshot.PageCount != 1 || len(published.Pages) != 1 {
		t.Fatalf("published snapshot: %+v pages=%d", published.Snapshot, len(published.Pages))
	}
	if !published.Snapshot.Generated {
		t.Fatal("every wiki payload must declare that it is generated")
	}

	// A second generation that dies half-way must not replace it. The claim
	// succeeds (the first build is finished), one page lands, and then nothing.
	second := startWikiSnapshot(t, projectID, []string{"src/app.py"})
	writePage(second.ID, "half-written").Want(http.StatusCreated)

	var afterCrash CodeWikiResponse
	testutil.Call(t, testHandler.GetProjectCodeWiki,
		wikiRequest(t, http.MethodGet, "/api/projects/x/wiki", nil, "id", projectID),
	).Want(http.StatusOK).JSON(&afterCrash)
	if afterCrash.Snapshot == nil || afterCrash.Snapshot.ID != published.Snapshot.ID {
		t.Fatalf("an interrupted run must leave the previous snapshot in place, got %+v", afterCrash.Snapshot)
	}
	if len(afterCrash.Pages) != 1 || afterCrash.Pages[0].Slug != "overview" {
		t.Fatalf("the previous snapshot's pages must be untouched: %+v", afterCrash.Pages)
	}

	// A published snapshot is immutable: no page may be added to it afterwards.
	writePage(published.Snapshot.ID, "late").Want(http.StatusConflict)
}

func TestCodeWikiRefusesPagesWithoutValidCitations(t *testing.T) {
	projectID, _ := wikiFixture(t, "https://github.com/acme/cites")
	snapshot := startWikiSnapshot(t, projectID, []string{"src/billing.py", "docs/guide.md"})

	post := func(body map[string]any) *testutil.Response {
		return testutil.Call(t, testHandler.CreateProjectCodeWikiPage,
			wikiRequest(t, http.MethodPost, "/api/projects/x/wiki/snapshots/y/pages", body, "id", projectID, "sid", snapshot.ID))
	}

	for name, body := range map[string]map[string]any{
		"no citations field": {"slug": "a", "title": "A", "content": "x"},
		"empty citations":    {"slug": "b", "title": "B", "content": "x", "citations": []any{}},
		"blank path": {"slug": "c", "title": "C", "content": "x",
			"citations": []map[string]any{{"path": "   "}}},
		"path not in the announced inventory": {"slug": "d", "title": "D", "content": "x",
			"citations": []map[string]any{{"path": "src/invented.py"}}},
		"one good and one invented path": {"slug": "e", "title": "E", "content": "x",
			"citations": []map[string]any{{"path": "src/billing.py"}, {"path": "src/nope.py"}}},
		"end line before start line": {"slug": "f", "title": "F", "content": "x",
			"citations": []map[string]any{{"path": "src/billing.py", "start_line": 40, "end_line": 12}}},
	} {
		t.Run(name, func(t *testing.T) {
			res := post(body).Want(http.StatusBadRequest)
			if res.Body.Len() == 0 {
				t.Fatal("a refusal must say why")
			}
		})
	}

	post(map[string]any{"slug": "ok", "title": "Billing", "content": "How billing works",
		"citations": []map[string]any{{"path": "src/billing.py", "start_line": 40, "end_line": 88}},
	}).Want(http.StatusCreated)

	// The inventory is required at claim time — without it the citation check
	// would have nothing to check against.
	otherProject, _ := wikiFixture(t, "https://github.com/acme/no-inventory")
	testutil.Call(t, testHandler.CreateProjectCodeWikiSnapshot,
		wikiRequest(t, http.MethodPost, "/api/projects/x/wiki/snapshots", map[string]any{
			"commit_sha": "abc", "repo_paths": []string{},
		}, "id", otherProject),
	).Want(http.StatusBadRequest)
}

func TestCodeWikiPageSurvivesUnparseableCitations(t *testing.T) {
	// Acceptance 7: a page whose citations column cannot be read is served as
	// unsourced rather than breaking the panel.
	projectID, resourceID := wikiFixture(t, "https://github.com/acme/broken-citations")
	snapshot := startWikiSnapshot(t, projectID, []string{"src/app.py"})
	testutil.Call(t, testHandler.CreateProjectCodeWikiPage,
		wikiRequest(t, http.MethodPost, "/x", map[string]any{
			"slug": "overview", "title": "Overview", "content": "body",
			"citations": []map[string]any{{"path": "src/app.py"}},
		}, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusCreated)
	dbfx.Exec(t, `UPDATE code_wiki_page SET citations = '"not an array"'::jsonb WHERE project_resource_id = $1`, resourceID)
	testutil.Call(t, testHandler.PublishProjectCodeWikiSnapshot,
		wikiRequest(t, http.MethodPost, "/x", nil, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusOK)

	var page CodeWikiPageResponse
	testutil.Call(t, testHandler.GetProjectCodeWikiPage,
		wikiRequest(t, http.MethodGet, "/x", nil, "id", projectID, "slug", "overview"),
	).Want(http.StatusOK).JSON(&page)
	if page.Citations == nil {
		t.Fatal("citations must serialise as an empty array, never null, so the panel can map over it")
	}
	if len(page.Citations) != 0 || page.Content != "body" {
		t.Fatalf("an unreadable citation column must leave the page readable and unsourced: %+v", page)
	}
}

func TestCodeWikiStaleFlagFollowsTheKnownHead(t *testing.T) {
	projectID, resourceID := wikiFixture(t, "https://github.com/acme/stale")
	snapshot := startWikiSnapshot(t, projectID, []string{"src/app.py"})
	testutil.Call(t, testHandler.CreateProjectCodeWikiPage,
		wikiRequest(t, http.MethodPost, "/x", map[string]any{
			"slug": "overview", "title": "Overview", "content": "body",
			"citations": []map[string]any{{"path": "src/app.py"}},
		}, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusCreated)
	testutil.Call(t, testHandler.PublishProjectCodeWikiSnapshot,
		wikiRequest(t, http.MethodPost, "/x", nil, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusOK)

	var fresh CodeWikiResponse
	testutil.Call(t, testHandler.GetProjectCodeWiki,
		wikiRequest(t, http.MethodGet, "/x", nil, "id", projectID),
	).Want(http.StatusOK).JSON(&fresh)
	if fresh.Snapshot == nil || fresh.Snapshot.Stale {
		t.Fatalf("the newest published snapshot is not stale: %+v", fresh.Snapshot)
	}

	// A newer commit was recorded for this repository without a wiki behind it.
	dbfx.Insert(t, "code_wiki_snapshot", testutil.Cols{
		"workspace_id":        testWorkspaceID,
		"project_resource_id": resourceID,
		"commit_sha":          "deadbee",
		"state":               "failed",
	})
	var stale CodeWikiResponse
	testutil.Call(t, testHandler.GetProjectCodeWiki,
		wikiRequest(t, http.MethodGet, "/x", nil, "id", projectID),
	).Want(http.StatusOK).JSON(&stale)
	if stale.Snapshot == nil || !stale.Snapshot.Stale {
		t.Fatalf("a snapshot behind the known head is stale: %+v", stale.Snapshot)
	}
}

func TestCodeWikiIsPurgedWithItsResource(t *testing.T) {
	projectID, resourceID := wikiFixture(t, "https://github.com/acme/purge")
	snapshot := startWikiSnapshot(t, projectID, []string{"src/app.py"})
	testutil.Call(t, testHandler.CreateProjectCodeWikiPage,
		wikiRequest(t, http.MethodPost, "/x", map[string]any{
			"slug": "overview", "title": "Overview", "content": "body",
			"citations": []map[string]any{{"path": "src/app.py"}},
		}, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusCreated)
	testutil.Call(t, testHandler.PublishProjectCodeWikiSnapshot,
		wikiRequest(t, http.MethodPost, "/x", nil, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusOK)

	testutil.Call(t, testHandler.DeleteProjectResource,
		wikiRequest(t, http.MethodDelete, "/x", nil, "id", projectID, "resourceId", resourceID),
	).Want(http.StatusNoContent)

	var pages, snapshots int
	dbfx.QueryRow(t, `SELECT count(*) FROM code_wiki_page WHERE project_resource_id = $1`, resourceID).Scan(&pages)
	dbfx.QueryRow(t, `SELECT count(*) FROM code_wiki_snapshot WHERE project_resource_id = $1`, resourceID).Scan(&snapshots)
	if pages != 0 || snapshots != 0 {
		t.Fatalf("detaching the repository must take its wiki with it: %d pages, %d snapshots", pages, snapshots)
	}
}

func TestCodeWikiRejectsMalformedRequestBodies(t *testing.T) {
	projectID, _ := wikiFixture(t, "https://github.com/acme/malformed")
	testutil.Call(t, testHandler.CreateProjectCodeWikiSnapshot,
		testutil.WithURLParams(testutil.WithHeaders(
			testutil.JSONRequest(http.MethodPost, "/x", "{not json"),
			"X-User-ID", testUserID, "X-Workspace-ID", testWorkspaceID), "id", projectID),
	).Want(http.StatusBadRequest)

	snapshot := startWikiSnapshot(t, projectID, []string{"src/app.py"})
	testutil.Call(t, testHandler.CreateProjectCodeWikiPage,
		wikiRequest(t, http.MethodPost, "/x", map[string]any{
			"slug": "a", "title": "A", "content": "x", "citations": json.RawMessage(`"not-an-array"`),
		}, "id", projectID, "sid", snapshot.ID),
	).Want(http.StatusBadRequest)
}

func TestGitHubOwnerRepoMatching(t *testing.T) {
	// The URL form a user pasted must not decide whether their repository is
	// recognised on merge.
	for _, url := range []string{
		"https://github.com/acme/widget",
		"https://github.com/acme/widget.git",
		"git@github.com:acme/widget.git",
		"ssh://git@github.com/acme/widget",
		"https://github.com/ACME/Widget",
	} {
		if !repoRefMatchesGitHub([]byte(`{"url":"`+url+`"}`), "acme", "widget") {
			t.Errorf("%s should match acme/widget", url)
		}
	}
	for _, url := range []string{
		"https://github.com/acme/other",
		"https://github.com/someone/widget",
		"not a url",
		"",
	} {
		if repoRefMatchesGitHub([]byte(`{"url":"`+url+`"}`), "acme", "widget") {
			t.Errorf("%s must not match acme/widget", url)
		}
	}
	if repoRefMatchesGitHub([]byte(`{`), "acme", "widget") {
		t.Error("an unreadable resource ref matches nothing")
	}
}
