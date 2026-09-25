package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Comment threads anchored to a diff line (F07 / JEF-21).
//
// The rules pinned here are the ones a reader of the handler cannot infer:
// the anchor belongs to the thread ROOT, replies INHERIT it, an anchor is only
// admissible against a pull request linked to THIS issue, and a head that has
// moved marks the thread stale instead of hiding it.

func commentAnchorCleanup(t *testing.T) {
	t.Helper()
	syncIssueCounter(t)
	t.Cleanup(func() {
		// NOT context.Background(): Go cancels it just before cleanups run.
		testPool.Exec(context.Background(), `DELETE FROM review_flag WHERE workspace_id = $1`, testWorkspaceID)
	})
}

// postComment posts through the real CreateComment handler so validation,
// stamping and the response shape are all exercised together.
func postComment(t *testing.T, issueID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", body)
	return testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(req, "id", issueID))
}

func anchorBody(prID, path string, lineStart int, over map[string]any) map[string]any {
	anchor := map[string]any{"pr_id": prID, "file_path": path, "line_start": lineStart}
	for k, v := range over {
		anchor[k] = v
	}
	return map[string]any{"content": "why is this here?", "anchor": anchor}
}

func anchoredThreads(t *testing.T, issueID, prID, sha string) anchoredThreadsResponse {
	t.Helper()
	path := "/api/issues/" + issueID + "/pull-requests/" + prID + "/anchored-threads"
	if sha != "" {
		path += "?sha=" + sha
	}
	var out anchoredThreadsResponse
	testutil.Call(t, testHandler.GetIssueAnchoredThreads, testutil.WithURLParams(
		newRequest(http.MethodGet, path, nil), "id", issueID, "prId", prID)).Want(http.StatusOK).JSON(&out)
	return out
}

// Acceptance 1: a thread opened from a hunk line stores the file, the range,
// the side and the head, and the head defaults to the pull request's current
// one so the client never has to know about commits.
func TestCommentAnchorStoresLocationAndDefaultsHead(t *testing.T) {
	commentAnchorCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	var created CommentResponse
	postComment(t, issueID, anchorBody(prID, "server/internal/handler/comment.go", 41,
		map[string]any{"line_end": 44, "side": "old"})).Want(http.StatusCreated).JSON(&created)

	if created.Anchor == nil {
		t.Fatalf("created comment came back without an anchor: %+v", created)
	}
	got := *created.Anchor
	if got.Kind != "diff_line" || got.FilePath != "server/internal/handler/comment.go" ||
		got.LineStart != 41 || got.LineEnd != 44 || got.Side != "old" {
		t.Fatalf("anchor = %+v, want the file, the 41-44 range and the old side", got)
	}
	if got.HeadSha != "head-1" {
		t.Errorf("head_sha = %q, want the pull request's current head", got.HeadSha)
	}
	if got.PrSource != "vcs" || got.PrID != prID {
		t.Errorf("anchor pull request = %s/%s, want vcs/%s", got.PrSource, got.PrID, prID)
	}
	if created.AnchorStale {
		t.Errorf("a thread anchored to the CURRENT head must not be stale")
	}
	// line_end is optional: omitted, it collapses onto line_start rather than
	// storing 0, which would render as a range ending before it begins.
	var single CommentResponse
	postComment(t, issueID, anchorBody(prID, "a.go", 7, nil)).Want(http.StatusCreated).JSON(&single)
	if single.Anchor == nil || single.Anchor.LineEnd != 7 || single.Anchor.Side != "new" {
		t.Fatalf("single-line anchor = %+v, want line_end 7 and the default new side", single.Anchor)
	}
}

// Acceptance 2: a thread opened from a review flag row carries the flag, so
// the discussion and the finding stay tied together.
func TestCommentAnchorCarriesReviewFlag(t *testing.T) {
	commentAnchorCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	var flag ReviewFlagResponse
	addFlag(t, issueID, flagPayload(prID, "bug", "a.go", 12, map[string]any{"line_end": 18})).
		Want(http.StatusCreated).JSON(&flag)

	var created CommentResponse
	postComment(t, issueID, anchorBody(prID, "a.go", 12,
		map[string]any{"line_end": 18, "review_flag_id": flag.ID})).Want(http.StatusCreated).JSON(&created)

	if created.Anchor == nil || created.Anchor.ReviewFlagID == nil || *created.Anchor.ReviewFlagID != flag.ID {
		t.Fatalf("anchor = %+v, want review_flag_id %s", created.Anchor, flag.ID)
	}
}

// Acceptance 3: an anchor on a reply is refused. It is a 400 rather than a
// silent drop because a thread that could describe two places is a thread the
// reader cannot resolve — and the client needs to learn that now, not by
// noticing later that its anchor vanished.
func TestCommentAnchorOnReplyIsRefused(t *testing.T) {
	commentAnchorCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	var root CommentResponse
	postComment(t, issueID, anchorBody(prID, "a.go", 3, nil)).Want(http.StatusCreated).JSON(&root)

	body := anchorBody(prID, "b.go", 9, nil)
	body["parent_id"] = root.ID
	postComment(t, issueID, body).Want(http.StatusBadRequest)
}

// A reply INHERITS the root's anchor: nothing is written on the reply row, and
// the response resolves it. This is what lets the whole thread render under
// the same hunk however deep the conversation gets.
func TestCommentAnchorRepliesInheritRootAnchor(t *testing.T) {
	commentAnchorCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	var root CommentResponse
	postComment(t, issueID, anchorBody(prID, "a.go", 3, map[string]any{"line_end": 5})).
		Want(http.StatusCreated).JSON(&root)

	var reply CommentResponse
	postComment(t, issueID, map[string]any{"content": "still unclear", "parent_id": root.ID}).
		Want(http.StatusCreated).JSON(&reply)
	if reply.Anchor == nil || reply.Anchor.FilePath != "a.go" || reply.Anchor.LineStart != 3 {
		t.Fatalf("reply anchor = %+v, want the root's a.go:3-5", reply.Anchor)
	}

	// A reply to the reply resolves to the same root, not to its direct parent.
	var nested CommentResponse
	postComment(t, issueID, map[string]any{"content": "same here", "parent_id": reply.ID}).
		Want(http.StatusCreated).JSON(&nested)
	if nested.Anchor == nil || nested.Anchor.LineEnd != 5 {
		t.Fatalf("nested reply anchor = %+v, want the thread ROOT's anchor", nested.Anchor)
	}

	// And the list path resolves it for the whole page in one pass.
	var listed []CommentResponse
	testutil.Call(t, testHandler.ListComments, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/comments", nil), "id", issueID)).
		Want(http.StatusOK).JSON(&listed)
	anchored := 0
	for _, c := range listed {
		if c.Anchor != nil && c.Anchor.FilePath == "a.go" {
			anchored++
		}
	}
	if anchored != 3 {
		t.Fatalf("list returned %d anchored comments, want the root and both replies", anchored)
	}
}

// The anchor must be admissible only against a pull request linked to THIS
// issue, and a review flag only when it belongs to the same issue AND the same
// pull request. Both mismatches answer 404 rather than 403: a caller must not
// be able to tell "not yours" from "does not exist".
func TestCommentAnchorRejectsForeignPullRequestAndFlag(t *testing.T) {
	commentAnchorCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")
	otherIssueID, otherPrID, _, _ := reviewFlagVCSPR(t, "head-2")

	// A pull request linked to another issue.
	postComment(t, issueID, anchorBody(otherPrID, "a.go", 3, nil)).Want(http.StatusNotFound)
	// An id that is not a pull request at all.
	postComment(t, issueID, anchorBody(uuid.NewString(), "a.go", 3, nil)).Want(http.StatusNotFound)

	// A flag from the other issue's pull request.
	var foreign ReviewFlagResponse
	addFlag(t, otherIssueID, flagPayload(otherPrID, "bug", "a.go", 1, nil)).Want(http.StatusCreated).JSON(&foreign)
	postComment(t, issueID, anchorBody(prID, "a.go", 3, map[string]any{"review_flag_id": foreign.ID})).
		Want(http.StatusNotFound)
}

// Malformed anchors are refused at the boundary, before anything is written —
// a rejected anchor must never leave an unanchored comment behind.
func TestCommentAnchorValidation(t *testing.T) {
	commentAnchorCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	cases := []struct {
		name string
		over map[string]any
		path string
	}{
		{name: "empty file path", path: "   "},
		{name: "line_start below one", path: "a.go", over: map[string]any{"line_start": 0}},
		{name: "inverted range", path: "a.go", over: map[string]any{"line_end": 1}},
		{name: "unknown side", path: "a.go", over: map[string]any{"side": "both"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			postComment(t, issueID, anchorBody(prID, tc.path, 9, tc.over)).Want(http.StatusBadRequest)
		})
	}

	// Nothing above was stored.
	if got := anchoredThreads(t, issueID, prID, ""); len(got.Threads) != 0 {
		t.Fatalf("a refused anchor left %d thread(s) behind", len(got.Threads))
	}
}

// Acceptance 6 (read side): the anchored-threads endpoint returns whole
// threads — root plus every reply — and never a thread cut off mid-conversation.
func TestAnchoredThreadsReturnsWholeThreads(t *testing.T) {
	commentAnchorCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	var root CommentResponse
	postComment(t, issueID, anchorBody(prID, "a.go", 3, nil)).Want(http.StatusCreated).JSON(&root)
	postComment(t, issueID, map[string]any{"content": "reply one", "parent_id": root.ID}).Want(http.StatusCreated)
	var second CommentResponse
	postComment(t, issueID, map[string]any{"content": "reply two", "parent_id": root.ID}).
		Want(http.StatusCreated).JSON(&second)
	postComment(t, issueID, map[string]any{"content": "nested", "parent_id": second.ID}).Want(http.StatusCreated)
	// An unanchored thread on the same issue must not leak into the result.
	postComment(t, issueID, map[string]any{"content": "unrelated"}).Want(http.StatusCreated)

	got := anchoredThreads(t, issueID, prID, "")
	if len(got.Threads) != 1 {
		t.Fatalf("threads = %d, want exactly the one anchored thread", len(got.Threads))
	}
	th := got.Threads[0]
	if th.Root.ID != root.ID {
		t.Fatalf("root = %s, want %s", th.Root.ID, root.ID)
	}
	if len(th.Replies) != 3 {
		t.Fatalf("replies = %d, want both replies and the nested one", len(th.Replies))
	}
	if th.Anchor == nil || th.Anchor.FilePath != "a.go" {
		t.Fatalf("thread anchor = %+v", th.Anchor)
	}
	for _, r := range th.Replies {
		if r.Anchor == nil || r.Anchor.FilePath != "a.go" {
			t.Errorf("reply %s came back without the thread's anchor", r.ID)
		}
	}

	// ?sha= scopes to one head; a head nothing is anchored to answers empty
	// rather than falling back to "everything".
	if scoped := anchoredThreads(t, issueID, prID, "head-1"); len(scoped.Threads) != 1 {
		t.Errorf("sha=head-1 returned %d threads, want 1", len(scoped.Threads))
	}
	if scoped := anchoredThreads(t, issueID, prID, "nope"); len(scoped.Threads) != 0 {
		t.Errorf("sha=nope returned %d threads, want 0", len(scoped.Threads))
	}
}

// A head that moves marks the thread stale and keeps it readable. Losing the
// question when the code changes is exactly backwards: the push is often the
// answer, and the reviewer needs to see both.
func TestAnchoredThreadGoesStaleWhenHeadMoves(t *testing.T) {
	commentAnchorCleanup(t)
	issueID, prID, _, _ := reviewFlagVCSPR(t, "head-1")

	var root CommentResponse
	postComment(t, issueID, anchorBody(prID, "a.go", 3, nil)).Want(http.StatusCreated).JSON(&root)

	if _, err := testPool.Exec(context.Background(),
		`UPDATE vcs_pull_request SET head_sha = 'head-2' WHERE id = $1`, prID); err != nil {
		t.Fatalf("move the head: %v", err)
	}

	got := anchoredThreads(t, issueID, prID, "")
	if len(got.Threads) != 1 {
		t.Fatalf("a thread anchored to an old head disappeared: %d threads", len(got.Threads))
	}
	if !got.Threads[0].AnchorStale {
		t.Errorf("thread anchored to head-1 is not marked stale after the head moved to head-2")
	}
	if got.Threads[0].Anchor.HeadSha != "head-1" {
		t.Errorf("stale anchor head = %q, want the head it was written against", got.Threads[0].Anchor.HeadSha)
	}

	// The reply path still works on a stale thread, and the reply reports the
	// same staleness so the composer can say so.
	var reply CommentResponse
	postComment(t, issueID, map[string]any{"content": "fixed by the push?", "parent_id": root.ID}).
		Want(http.StatusCreated).JSON(&reply)
	if reply.Anchor == nil || !reply.AnchorStale {
		t.Fatalf("reply on a stale thread = anchor %+v stale %v", reply.Anchor, reply.AnchorStale)
	}
}

// An ordinary comment is untouched: `anchor` is null and `anchor_stale` false,
// which is also what an older backend's response degrades to.
func TestCommentWithoutAnchorStaysUnanchored(t *testing.T) {
	commentAnchorCleanup(t)
	issueID := dbfx.Issue(t, "plain comment issue "+uuid.NewString()[:8])

	var created CommentResponse
	postComment(t, issueID, map[string]any{"content": "just a comment"}).Want(http.StatusCreated).JSON(&created)
	if created.Anchor != nil || created.AnchorStale {
		t.Fatalf("plain comment = anchor %+v stale %v, want no anchor", created.Anchor, created.AnchorStale)
	}
}

// A pull request that is not linked to the issue is a 404 on the read side too,
// for the same reason it is on the write side.
func TestAnchoredThreadsRejectsForeignPullRequest(t *testing.T) {
	commentAnchorCleanup(t)
	issueID, _, _, _ := reviewFlagVCSPR(t, "head-1")
	_, otherPrID, _, _ := reviewFlagVCSPR(t, "head-2")

	testutil.Call(t, testHandler.GetIssueAnchoredThreads, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/pull-requests/"+otherPrID+"/anchored-threads", nil),
		"id", issueID, "prId", otherPrID)).Want(http.StatusNotFound)
}
