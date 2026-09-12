package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Why search (K55): sources are indexed as they are written, a plain
// question finds them by meaning of words (stemming aside), results link to
// the issue, a deleted comment leaves the index, and the search stays
// inside the workspace.

func whySearch(t *testing.T, q string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, inboxWorkspaceHandler(testHandler.SearchWhy),
		testutil.WithHeaders(newRequest(http.MethodGet, "/api/search/why?q="+q, nil), "X-Workspace-ID", testWorkspaceID))
}

func TestWhySearchIndexesFindsLinksAndForgets(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM decision_search_chunk WHERE workspace_id = $1`, testWorkspaceID)
	})
	issue := dbfx.Issue(t, "why search issue")
	commentID := postCommentForTriggerPreviewTest(t, issue, map[string]any{"content": "We picked Chi over Gin because the router stays stdlib-compatible and middleware composes."})
	whySearch(t, "ab").Want(http.StatusBadRequest)

	var out struct {
		Results []WhySearchResult `json:"results"`
	}
	whySearch(t, "why+chi+instead+of+gin").Want(http.StatusOK).JSON(&out)
	if len(out.Results) == 0 || out.Results[0].SourceType != "comment" || out.Results[0].SourceID != commentID {
		t.Fatalf("results = %+v, want the comment first", out.Results)
	}
	if out.Results[0].IssueID == nil || *out.Results[0].IssueID != issue || out.Results[0].IssueIdentifier == "" || out.Results[0].Snippet == "" {
		t.Fatalf("result must link to its issue with a snippet: %+v", out.Results[0])
	}

	// A decision record recorded by hand is indexed too.
	_, task := completedAgentRun(t, "why search run")
	dbfx.Insert(t, "task_message", testutil.Cols{"task_id": task, "seq": 1, "type": "text", "content": "Decided to denormalize the invoice table to keep one read path for billing."})
	_ = task
	runIssue := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE id = $1`, task)
	if runIssue != 1 {
		t.Fatal("fixture")
	}
	var reindexed struct {
		Indexed map[string]int `json:"indexed"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ReindexWhy),
		testutil.WithHeaders(newRequest(http.MethodPost, "/api/search/why/reindex", nil), "X-Workspace-ID", testWorkspaceID)).Want(http.StatusOK).JSON(&reindexed)
	if reindexed.Indexed["task_message"] < 1 || reindexed.Indexed["comment"] < 1 {
		t.Fatalf("reindex = %+v", reindexed.Indexed)
	}
	whySearch(t, "denormalize+invoice").Want(http.StatusOK).JSON(&out)
	if len(out.Results) == 0 || out.Results[0].SourceType != "task_message" {
		t.Fatalf("run message must be found: %+v", out.Results)
	}

	// Another workspace never sees it.
	foreign := dbfx.Workspace(t, "Why foreign", "why-foreign-"+commentID[:8])
	dbfx.Member(t, foreign, testUserID, "owner")
	var foreignOut struct {
		Results []WhySearchResult `json:"results"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.SearchWhy),
		testutil.WithHeaders(newRequest(http.MethodGet, "/api/search/why?q=chi+gin", nil), "X-Workspace-ID", foreign)).Want(http.StatusOK).JSON(&foreignOut)
	if len(foreignOut.Results) != 0 {
		t.Fatalf("workspace leak: %+v", foreignOut.Results)
	}

	// Deleting the comment removes its chunk.
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, "/api/comments/"+commentID, nil), "commentId", commentID)
	testHandler.DeleteComment(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteComment: %d %s", w.Code, w.Body.String())
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM decision_search_chunk WHERE source_type = 'comment' AND source_id = $1`, commentID); n != 0 {
		t.Fatal("a deleted comment must leave the index")
	}
}

// ReindexWhy batches each source type's upserts into one statement instead
// of one UpsertWhyChunk call per row; this pins several comments, each on
// its own issue with distinct content, through one reindex call so a
// batching bug that zips one comment's content or issue link onto another
// (an unnest array misalignment) fails this test instead of shipping
// silently.
func TestReindexWhyBatchesMultipleCommentsWithoutMixingContent(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM decision_search_chunk WHERE workspace_id = $1`, testWorkspaceID)
	})
	type fixture struct {
		issue, comment, needle string
	}
	fixtures := make([]fixture, 0, 3)
	for i, phrase := range []string{
		"Alpha decided to cache the catalog response for ninety seconds.",
		"Bravo decided to retry the webhook delivery with backoff.",
		"Charlie decided to shard the audit log by workspace id.",
	} {
		issue := dbfx.Issue(t, "why batch issue")
		commentID := postCommentForTriggerPreviewTest(t, issue, map[string]any{"content": phrase})
		fixtures = append(fixtures, fixture{issue: issue, comment: commentID, needle: phrase})
		_ = i
	}

	var reindexed struct {
		Indexed map[string]int `json:"indexed"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ReindexWhy),
		testutil.WithHeaders(newRequest(http.MethodPost, "/api/search/why/reindex", nil), "X-Workspace-ID", testWorkspaceID)).Want(http.StatusOK).JSON(&reindexed)
	if reindexed.Indexed["comment"] < 3 {
		t.Fatalf("reindex = %+v, want at least the 3 fixture comments", reindexed.Indexed)
	}

	for _, f := range fixtures {
		var out struct {
			Results []WhySearchResult `json:"results"`
		}
		// A distinctive word from ONE comment's content must find THAT
		// comment, linked to THAT comment's own issue -- not another
		// fixture's, which is what a misaligned batch would produce.
		q := ""
		switch f.needle[0] {
		case 'A':
			q = "ninety+seconds+cache"
		case 'B':
			q = "webhook+backoff+retry"
		case 'C':
			q = "shard+audit+log"
		}
		whySearch(t, q).Want(http.StatusOK).JSON(&out)
		if len(out.Results) == 0 || out.Results[0].SourceType != "comment" || out.Results[0].SourceID != f.comment {
			t.Fatalf("query %q must find comment %s first, got %+v", q, f.comment, out.Results)
		}
		if out.Results[0].IssueID == nil || *out.Results[0].IssueID != f.issue {
			t.Fatalf("comment %s must link to its own issue %s, got %+v", f.comment, f.issue, out.Results[0])
		}
	}
}
