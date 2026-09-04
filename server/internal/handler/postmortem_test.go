package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func postmortemRequest(method, path, workspaceID string, body any) *http.Request {
	return testutil.WithHeaders(
		testutil.JSONRequest(method, path, body),
		"X-User-ID", testUserID,
		"X-Workspace-ID", workspaceID,
	)
}

func postmortemWorkspaceHandler(handler http.HandlerFunc) http.HandlerFunc {
	return middleware.RequireWorkspaceMember(testHandler.Queries)(handler).ServeHTTP
}

func seedPostmortem(t *testing.T, workspaceID, state string) string {
	t.Helper()
	cols := testutil.Cols{
		"workspace_id":   workspaceID,
		"source_task_id": uuid.NewString(),
		"trigger":        "failed",
		"state":          state,
		"failure_reason": "timeout",
		"summary":        "The run timed out.",
		"root_cause":     "The task exceeded the run window.",
		"impact":         "No work was delivered.",
	}
	if state != "draft" {
		// The state-machine CHECK requires resolved_at (and a resolver) once a
		// postmortem leaves draft.
		cols["resolved_at"] = time.Now().UTC()
		cols["resolved_by_type"] = "member"
		cols["resolved_by_id"] = testUserID
	}
	return dbfx.Insert(t, "postmortem", cols)
}

func TestListPostmortemsFiltersByState(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Postmortem list", "pm-list-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	seedPostmortem(t, workspaceID, "draft")
	seedPostmortem(t, workspaceID, "approved")

	var resp struct {
		Items []PostmortemResponse `json:"items"`
	}
	testutil.Call(t, postmortemWorkspaceHandler(testHandler.GetPostmortems),
		postmortemRequest(http.MethodGet, "/api/postmortems?state=draft", workspaceID, nil)).
		Want(http.StatusOK).
		JSON(&resp)

	if len(resp.Items) != 1 {
		t.Fatalf("draft postmortems = %d, want 1: %+v", len(resp.Items), resp.Items)
	}
	if resp.Items[0].State != "draft" {
		t.Errorf("state = %q, want draft", resp.Items[0].State)
	}
}

func TestGetPostmortemReturnsOne(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Postmortem get", "pm-get-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	pmID := seedPostmortem(t, workspaceID, "draft")

	var resp PostmortemResponse
	testutil.Call(t, postmortemWorkspaceHandler(testHandler.GetPostmortem),
		testutil.WithURLParams(
			postmortemRequest(http.MethodGet, "/api/postmortems/"+pmID, workspaceID, nil),
			"id", pmID)).
		Want(http.StatusOK).
		JSON(&resp)

	if resp.ID != pmID {
		t.Fatalf("id = %q, want %q", resp.ID, pmID)
	}
	if resp.Summary != "The run timed out." {
		t.Errorf("summary = %q", resp.Summary)
	}
}

func TestGetPostmortemNotFound(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Postmortem 404", "pm-404-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	missing := uuid.NewString()

	testutil.Call(t, postmortemWorkspaceHandler(testHandler.GetPostmortem),
		testutil.WithURLParams(
			postmortemRequest(http.MethodGet, "/api/postmortems/"+missing, workspaceID, nil),
			"id", missing)).
		Want(http.StatusNotFound)
}

func TestApprovePostmortemThenConflict(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Postmortem approve", "pm-approve-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	pmID := seedPostmortem(t, workspaceID, "draft")

	var resp PostmortemResponse
	testutil.Call(t, postmortemWorkspaceHandler(testHandler.ApprovePostmortem),
		testutil.WithURLParams(
			postmortemRequest(http.MethodPost, "/api/postmortems/"+pmID+"/approve", workspaceID, nil),
			"id", pmID)).
		Want(http.StatusOK).
		JSON(&resp)
	if resp.State != "approved" {
		t.Fatalf("state = %q, want approved", resp.State)
	}

	// Already resolved -> 409.
	testutil.Call(t, postmortemWorkspaceHandler(testHandler.ApprovePostmortem),
		testutil.WithURLParams(
			postmortemRequest(http.MethodPost, "/api/postmortems/"+pmID+"/approve", workspaceID, nil),
			"id", pmID)).
		Want(http.StatusConflict)
}

func TestDiscardPostmortem(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Postmortem discard", "pm-discard-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	pmID := seedPostmortem(t, workspaceID, "draft")

	var resp PostmortemResponse
	testutil.Call(t, postmortemWorkspaceHandler(testHandler.DiscardPostmortem),
		testutil.WithURLParams(
			postmortemRequest(http.MethodPost, "/api/postmortems/"+pmID+"/discard", workspaceID, nil),
			"id", pmID)).
		Want(http.StatusOK).
		JSON(&resp)
	if resp.State != "discarded" {
		t.Fatalf("state = %q, want discarded", resp.State)
	}
}
