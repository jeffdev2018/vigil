package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func seedPostmortem(t *testing.T, workspaceID, state string, over ...testutil.Cols) string {
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
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	return dbfx.Insert(t, "postmortem", cols)
}

func TestApprovePostmortemStoresRulesAsAgentMemory(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Postmortem rules", "pm-rules-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	agentID := dbfx.Agent(t, "Rules agent", "", testutil.Cols{"workspace_id": workspaceID})
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_memory WHERE agent_id = $1`, agentID)
	})
	// One duplicate and one blank rule: only the distinct, non-empty rule lands.
	pmID := seedPostmortem(t, workspaceID, "draft", testutil.Cols{
		"agent_id":         agentID,
		"preventive_rules": testutil.Raw(`'["Run the test suite before pushing", "  ", "run the test suite before pushing"]'::jsonb`),
	})

	var resp PostmortemResponse
	testutil.Call(t, postmortemWorkspaceHandler(testHandler.ApprovePostmortem),
		testutil.WithURLParams(
			postmortemRequest(http.MethodPost, "/api/postmortems/"+pmID+"/approve", workspaceID, nil),
			"id", pmID)).
		Want(http.StatusOK).
		JSON(&resp)
	if resp.AppliedRules == nil || *resp.AppliedRules != 1 {
		t.Fatalf("applied_rules = %v, want 1", resp.AppliedRules)
	}

	memories, err := testHandler.Queries.ListAgentMemories(context.Background(), db.ListAgentMemoriesParams{
		AgentID: parseUUID(agentID), WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(memories) != 1 {
		t.Fatalf("agent memories = %d, want 1", len(memories))
	}
	if memories[0].Source != "postmortem" || memories[0].Content != "Run the test suite before pushing" {
		t.Fatalf("memory = %q/%q, want postmortem/rule text", memories[0].Source, memories[0].Content)
	}
	// The postmortem was human-approved, so its rules land approved (JEF-269).
	if memories[0].State != "approved" {
		t.Fatalf("postmortem rule memory state = %q, want approved", memories[0].State)
	}

	// Discard must not touch memory.
	pm2 := seedPostmortem(t, workspaceID, "draft", testutil.Cols{
		"agent_id":         agentID,
		"preventive_rules": testutil.Raw(`'["Never retry a failed deploy"]'::jsonb`),
	})
	var discarded PostmortemResponse
	testutil.Call(t, postmortemWorkspaceHandler(testHandler.DiscardPostmortem),
		testutil.WithURLParams(
			postmortemRequest(http.MethodPost, "/api/postmortems/"+pm2+"/discard", workspaceID, nil),
			"id", pm2)).
		Want(http.StatusOK).
		JSON(&discarded)
	if discarded.AppliedRules != nil {
		t.Fatalf("applied_rules on discard = %v, want absent", *discarded.AppliedRules)
	}
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
