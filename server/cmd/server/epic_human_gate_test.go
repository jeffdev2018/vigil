package main

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Epic Mode (F18) puts a human gate between steps: approving a step and
// applying the ticket breakdown are refused to an agent's task token at the
// router, so an agent cannot approve its own draft and create the child
// issues it wrote.
func TestEpicApproveAndApplyAreHumanOnly(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	var agentID string
	fx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 AND runtime_id IS NOT NULL LIMIT 1`, testWorkspaceID).Scan(&agentID)
	project := fx.Project(t, "epic human gate")
	for _, path := range []string{
		"/api/projects/" + project + "/epic/steps/tickets/approve",
		"/api/projects/" + project + "/epic/steps/tickets/apply",
	} {
		resp := authRequestWithAgent(t, http.MethodPost, path, map[string]any{}, agentID)
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("POST %s with a task token: status %d, want 403", path, resp.StatusCode)
		}
	}
}
