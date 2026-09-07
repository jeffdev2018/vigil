package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Approval gates (K05): the workspace decides which MCP tools pause for a
// human, and that decision has to reach the daemon. It cannot come from the
// daemon's own environment — one daemon serves several workspaces — so it
// travels on the claim. Before it did, widening the pattern in workspace
// settings changed nothing: the daemon kept asking about its compiled
// default and never opened a gate for the tools the workspace had added.
func TestClaimCarriesWorkspaceSensitiveTools(t *testing.T) {
	var previous []byte
	testPool.QueryRow(context.Background(), `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&previous)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `UPDATE workspace SET settings = $1 WHERE id = $2`, previous, testWorkspaceID)
	})

	claimSensitiveTools := func(t *testing.T) string {
		t.Helper()
		agent := dbfx.Agent(t, "gate agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
		issue := dbfx.Issue(t, "gate issue "+uuid.NewString()[:6], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
		dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "queued"})
		req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+handlerTestRuntimeID(t)+"/tasks/claim", nil, testWorkspaceID, "gate-daemon")
		var claim struct {
			Task *struct {
				SensitiveTools string `json:"sensitive_tools"`
			} `json:"task"`
		}
		testutil.Call(t, testHandler.ClaimTaskByRuntime, testutil.WithURLParams(req, "runtimeId", handlerTestRuntimeID(t))).Want(http.StatusOK).JSON(&claim)
		if claim.Task == nil {
			t.Fatal("claim returned no task")
		}
		return claim.Task.SensitiveTools
	}

	// No workspace setting: the claim states the default rather than staying
	// silent, so the daemon never has to guess which server it is talking to.
	if got := claimSensitiveTools(t); got != service.DefaultSensitiveTools {
		t.Fatalf("sensitive_tools = %q, want the default %q", got, service.DefaultSensitiveTools)
	}

	// A workspace that widens the pattern is obeyed.
	const widened = `(?i)merge|delete|deploy|rotate_key`
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || jsonb_build_object('approval_gates', jsonb_build_object('sensitive_tools', $2::text)) WHERE id = $1`, testWorkspaceID, widened)
	if got := claimSensitiveTools(t); got != widened {
		t.Fatalf("sensitive_tools = %q, want the workspace's own pattern %q", got, widened)
	}
}
