package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Per-project review policy (JEF-238): defaults without a row, upsert
// round-trip, and the 400 ladder on bad input.

func reviewConfigCleanup(t *testing.T, projectID string) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_review_config WHERE project_id = $1`, projectID)
	})
}

func getReviewConfig(t *testing.T, projectID string) ProjectReviewConfigResponse {
	t.Helper()
	var out ProjectReviewConfigResponse
	testutil.Call(t, testHandler.GetProjectReviewConfig, testutil.WithURLParams(newRequest(http.MethodGet, "/api/projects/"+projectID+"/review-config", nil), "id", projectID)).Want(http.StatusOK).JSON(&out)
	return out
}

func putReviewConfig(t *testing.T, projectID string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.PutProjectReviewConfig, testutil.WithURLParams(newRequest(http.MethodPut, "/api/projects/"+projectID+"/review-config", body), "id", projectID))
}

func TestProjectReviewConfigDefaultsAndRoundTrip(t *testing.T) {
	project := dbfx.Project(t, "review config project")
	reviewConfigCleanup(t, project)
	reviewer := dbfx.Agent(t, "review cfg reviewer", handlerTestRuntimeID(t))

	// No row: the defaults, never a 404.
	got := getReviewConfig(t, project)
	if got.ProjectID != project || len(got.Checklist) != 0 || got.ReviewerAgentID != nil || got.GateEnabled || got.MaxCycles != 3 {
		t.Fatalf("defaults = %+v", got)
	}

	// Upsert: the saved row comes back, checklist entries trimmed.
	var saved ProjectReviewConfigResponse
	putReviewConfig(t, project, map[string]any{
		"checklist":         []string{"tests pass", "  no stray logs  "},
		"reviewer_agent_id": reviewer,
		"gate_enabled":      true,
		"max_cycles":        5,
	}).Want(http.StatusOK).JSON(&saved)
	if saved.GateEnabled != true || saved.MaxCycles != 5 || len(saved.Checklist) != 2 || saved.Checklist[1] != "no stray logs" || saved.ReviewerAgentID == nil || *saved.ReviewerAgentID != reviewer {
		t.Fatalf("saved = %+v", saved)
	}
	got = getReviewConfig(t, project)
	if !got.GateEnabled || got.MaxCycles != 5 || len(got.Checklist) != 2 {
		t.Fatalf("after put = %+v", got)
	}

	// A second PUT replaces the row: pinned reviewer cleared, gate off.
	putReviewConfig(t, project, map[string]any{"checklist": []string{}, "reviewer_agent_id": nil, "gate_enabled": false, "max_cycles": 1}).Want(http.StatusOK)
	got = getReviewConfig(t, project)
	if got.GateEnabled || got.MaxCycles != 1 || got.ReviewerAgentID != nil || len(got.Checklist) != 0 {
		t.Fatalf("after second put = %+v", got)
	}
	var stored int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM project_review_config WHERE project_id = $1`, project).Scan(&stored)
	if stored != 1 {
		t.Fatalf("rows = %d, want the upsert to keep exactly one", stored)
	}

	// Unknown project: 404, from the project ladder rather than the config.
	missing := "00000000-0000-0000-0000-0000000000aa"
	testutil.Call(t, testHandler.GetProjectReviewConfig, testutil.WithURLParams(newRequest(http.MethodGet, "/api/projects/"+missing+"/review-config", nil), "id", missing)).Want(http.StatusNotFound)
}

func TestProjectReviewConfigValidation(t *testing.T) {
	project := dbfx.Project(t, "review config validation")
	reviewConfigCleanup(t, project)
	agent := dbfx.Agent(t, "review cfg candidate", handlerTestRuntimeID(t))

	putReviewConfig(t, project, map[string]any{"max_cycles": 0}).Want(http.StatusBadRequest)
	putReviewConfig(t, project, map[string]any{"max_cycles": 11}).Want(http.StatusBadRequest)

	tooLong := make([]string, 21)
	for i := range tooLong {
		tooLong[i] = fmt.Sprintf("item %d", i)
	}
	putReviewConfig(t, project, map[string]any{"checklist": tooLong}).Want(http.StatusBadRequest)
	putReviewConfig(t, project, map[string]any{"checklist": []string{"ok", "   "}}).Want(http.StatusBadRequest)

	// The pinned reviewer must be a live agent of THIS workspace.
	otherWs := dbfx.Workspace(t, "Other WS", "other-ws-jef238")
	otherRt := dbfx.Runtime(t, "other runtime", testutil.Cols{"workspace_id": otherWs})
	otherAgent := dbfx.Agent(t, "other workspace agent", otherRt, testutil.Cols{"workspace_id": otherWs})
	putReviewConfig(t, project, map[string]any{"reviewer_agent_id": otherAgent}).Want(http.StatusBadRequest)

	archived := dbfx.Agent(t, "review cfg archived", handlerTestRuntimeID(t))
	dbfx.Exec(t, `UPDATE agent SET archived_at = now() WHERE id = $1`, archived)
	putReviewConfig(t, project, map[string]any{"reviewer_agent_id": archived}).Want(http.StatusBadRequest)

	putReviewConfig(t, project, map[string]any{"reviewer_agent_id": "not-a-uuid"}).Want(http.StatusBadRequest)

	// A valid write after the failures proves none of them half-wrote a row.
	putReviewConfig(t, project, map[string]any{"reviewer_agent_id": agent, "gate_enabled": true, "max_cycles": 2}).Want(http.StatusOK)
}
