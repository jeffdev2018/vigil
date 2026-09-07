package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Acceptance 5: a non-empty daemon memory reaches the next run's brief.
//
// It rides the SAME assembly point as agent memory (buildClaimedTaskResponse
// in daemon.go), which is the property under test here: a second channel would
// drift from the one place a brief is built. The rendering of the section, and
// the fact that it is labelled as DATA rather than instructions, is pinned on
// the daemon side in internal/daemon/execenv.
func TestClaimedTaskCarriesAutopilotMemory(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "Autopilot memory brief runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Autopilot memory brief agent")

	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "Brief memory daemon",
		"description":     "Keep notes.",
		"assignee_type":   "agent",
		"assignee_id":     agentID,
		"status":          "active",
		"execution_mode":  "create_issue",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID,
		"source":       "schedule",
		"status":       "running",
		"issue_id":     issueID,
	})
	dbfx.InsertNoID(t, "autopilot_memory", testutil.Cols{
		"autopilot_id": autopilotID,
		"workspace_id": testWorkspaceID,
		"content":      "The queue label is `inbound`, not `inbox`.",
		"revision":     1,
	}, "autopilot_id = $1", autopilotID)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM autopilot_memory WHERE autopilot_id = $1`, autopilotID)
	})

	taskID := seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)
	dbfx.Exec(t, `UPDATE agent_task_queue SET autopilot_run_id = $1 WHERE id = $2`, runID, taskID)

	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if task == nil {
		t.Fatal("no task claimed")
	}
	runtime, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(runtimeID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "autopilot-memory-brief")
	resp, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, task, runtime, runtimeID, testWorkspaceID)
	if failure != nil {
		t.Fatalf("claim build failed: %+v", failure)
	}

	if !strings.Contains(resp.AutopilotMemory, "inbound") {
		t.Errorf("autopilot_memory = %q, want the daemon's note", resp.AutopilotMemory)
	}
	// The two memories are different scopes and must not leak into each other:
	// this agent has no agent-memory facts, so a non-empty Memories here would
	// mean the daemon's note was routed through the agent channel.
	if len(resp.Agent.Memories) != 0 {
		t.Errorf("agent memories = %v; the daemon note must not be delivered as agent memory", resp.Agent.Memories)
	}
}

// A run with no autopilot behind it carries no daemon memory, so an ordinary
// issue run's brief stays byte-identical.
func TestClaimedTaskWithoutAutopilotCarriesNoDaemonMemory(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "No autopilot memory runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "No autopilot memory agent")
	seedQueuedIssueTask(t, ctx, agentID, runtimeID, issueID)

	task, err := testHandler.TaskService.ClaimTaskForRuntime(ctx, parseUUID(runtimeID))
	if err != nil || task == nil {
		t.Fatalf("claim task: %v (task %v)", err, task)
	}
	runtime, err := testHandler.Queries.GetAgentRuntimeForWorkspace(ctx, db.GetAgentRuntimeForWorkspaceParams{
		ID:          parseUUID(runtimeID),
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "no-autopilot-memory")
	resp, _, _, _, failure := testHandler.buildClaimedTaskResponse(req, task, runtime, runtimeID, testWorkspaceID)
	if failure != nil {
		t.Fatalf("claim build failed: %+v", failure)
	}
	if resp.AutopilotMemory != "" {
		t.Errorf("autopilot_memory = %q on a run with no autopilot", resp.AutopilotMemory)
	}
}
