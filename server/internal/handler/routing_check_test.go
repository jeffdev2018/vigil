package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Validated routing (JEF-275). The distinction under test is between an agent
// whose work WILL eventually run (offline machine, pool fallback) and one whose
// work never would (archived, unbound, bound outside the workspace). Only the
// second refuses the trigger.

func enqueueForIssue(t *testing.T, issueID string) (db.AgentTaskQueue, error) {
	t.Helper()
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatalf("load issue: %v", err)
	}
	return testHandler.TaskService.EnqueueTaskForIssue(context.Background(), issue)
}

// TestRoutingCheckFatalRefusesAndAlerts: an agent nothing can claim refuses the
// trigger instead of parking a task in the queue, and the accountable humans
// hear about it.
func TestRoutingCheckFatalRefusesAndAlerts(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = $2`, testWorkspaceID, InboxTypeRoutingAlert)
	})

	// An archived agent: the trigger is refused and an alert is filed.
	archived := dbfx.Agent(t, "archived agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"archived_at": testutil.Raw("now()")})
	archivedIssue := dbfx.Issue(t, "archived issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": archived})
	if _, err := enqueueForIssue(t, archivedIssue); err == nil {
		t.Fatal("a trigger for an archived agent must be refused, not queued")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, archivedIssue); n != 0 {
		t.Fatalf("a refused trigger must leave nothing in the queue, rows = %d", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2 AND details->>'code' = $3`, InboxTypeRoutingAlert, archivedIssue, service.RoutingProblemAgentArchived); n < 1 {
		t.Fatal("the refusal must reach the accountable humans' inbox")
	}
	// The same broken agent re-triggered must not file the alert again: an
	// archived agent that still owns an issue is re-triggered by every comment
	// on it, and one item per trigger is a reason to mute the inbox.
	before := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND details->>'agent_id' = $2`, InboxTypeRoutingAlert, archived)
	if _, err := enqueueForIssue(t, archivedIssue); err == nil {
		t.Fatal("the second trigger must be refused too")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND details->>'agent_id' = $2`, InboxTypeRoutingAlert, archived); n != before {
		t.Fatalf("a repeated refusal must not file another alert today, %d -> %d", before, n)
	}

	// A runtime of another workspace: nothing in THIS workspace can claim it,
	// so the task must not be created at all. Before JEF-275 it was queued and
	// waited forever.
	otherWS := dbfx.Workspace(t, "other ws "+uuid.NewString()[:8], "other-"+uuid.NewString()[:8])
	otherRuntime := dbfx.Insert(t, "agent_runtime", testutil.Cols{
		"workspace_id": otherWS, "name": "foreign runtime", "runtime_mode": "cloud",
		"provider": "handler_test_runtime", "status": "online", "device_info": "",
		"metadata": testutil.Raw("'{}'::jsonb"), "last_seen_at": testutil.Raw("now()"), "visibility": "private", "owner_id": testUserID,
	})
	foreign := dbfx.Agent(t, "foreign agent "+uuid.NewString()[:8], otherRuntime)
	foreignIssue := dbfx.Issue(t, "foreign issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": foreign})
	_, err := enqueueForIssue(t, foreignIssue)
	if err == nil {
		t.Fatal("a trigger for a runtime outside the workspace must be refused")
	}
	if !containsAll(err.Error(), service.RoutingInvalidReason) {
		t.Fatalf("the refusal must name its reason, got %q", err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, foreignIssue); n != 0 {
		t.Fatalf("nothing may hang in the queue for a foreign runtime, rows = %d", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2 AND details->>'code' = $3`, InboxTypeRoutingAlert, foreignIssue, service.RoutingProblemRuntimeNotInWs); n < 1 {
		t.Fatal("a foreign-runtime refusal must reach the inbox too")
	}
}

// TestRoutingCheckOfflineRuntimeStillQueues: offline is a wait, not a failure.
// With a pool the task moves to an online member unchanged; without one it
// still queues, and the run carries the warning that explains the wait.
func TestRoutingCheckOfflineRuntimeStillQueues(t *testing.T) {
	offline := dbfx.Runtime(t, "offline rt "+uuid.NewString()[:8], testutil.Cols{"status": "offline"})
	online := dbfx.Runtime(t, "online rt "+uuid.NewString()[:8])
	pool := dbfx.Insert(t, "runtime_pool", testutil.Cols{"workspace_id": testWorkspaceID, "name": "jef275 " + uuid.NewString()[:8], "runtime_ids": `["` + online + `"]`})
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM runtime_pool WHERE id = $1`, pool) })

	// Offline bound runtime + an online pool member: unchanged K28 behaviour.
	pooled := dbfx.Agent(t, "pooled agent "+uuid.NewString()[:8], offline, testutil.Cols{"runtime_pool_id": pool})
	pooledIssue := dbfx.Issue(t, "pooled issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": pooled})
	task, err := enqueueForIssue(t, pooledIssue)
	if err != nil {
		t.Fatalf("an offline runtime with an online pool member must still queue: %v", err)
	}
	if uuidToString(task.RuntimeID) != online {
		t.Fatalf("the task must land on the online pool member, got %s", uuidToString(task.RuntimeID))
	}
	if hasRoutingWarning(t, task, service.RoutingProblemRuntimeOfflineOnly) {
		t.Fatal("a working pool fallback is not a warning")
	}

	// Offline bound runtime, no pool: still queued — the machine comes back —
	// but the run says why it is waiting.
	lone := dbfx.Agent(t, "lone agent "+uuid.NewString()[:8], offline)
	loneIssue := dbfx.Issue(t, "lone issue "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": lone})
	loneTask, err := enqueueForIssue(t, loneIssue)
	if err != nil {
		t.Fatalf("an offline runtime is a wait, not a refusal: %v", err)
	}
	if uuidToString(loneTask.RuntimeID) != offline {
		t.Fatalf("the task must stay on its own runtime, got %s", uuidToString(loneTask.RuntimeID))
	}
	if !hasRoutingWarning(t, loneTask, service.RoutingProblemRuntimeOfflineOnly) {
		t.Fatalf("the wait must be explained in the routing trace, got %s", loneTask.Routing)
	}
}

// TestGetAgentRoutingCheckEndpoint: the read surface the agent page uses.
func TestGetAgentRoutingCheckEndpoint(t *testing.T) {
	healthy := dbfx.Agent(t, "healthy agent "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	var ok RoutingCheckResponse
	testutil.Call(t, testHandler.GetAgentRoutingCheck, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/agents/"+healthy+"/routing-check", nil), "id", healthy)).Want(http.StatusOK).JSON(&ok)
	if !ok.OK || ok.Fatal || len(ok.Problems) != 0 {
		t.Fatalf("a healthy agent has no routing problem: %+v", ok)
	}

	unbound := dbfx.Agent(t, "unbound agent "+uuid.NewString()[:8], "")
	var broken RoutingCheckResponse
	testutil.Call(t, testHandler.GetAgentRoutingCheck, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/agents/"+unbound+"/routing-check", nil), "id", unbound)).Want(http.StatusOK).JSON(&broken)
	if broken.OK || !broken.Fatal || len(broken.Problems) != 1 || broken.Problems[0].Code != service.RoutingProblemNoRuntime {
		t.Fatalf("an agent bound to no runtime is a fatal routing problem: %+v", broken)
	}
	if broken.Problems[0].Message == "" {
		t.Fatal("a routing problem must carry a message a human can act on")
	}
}

func hasRoutingWarning(t *testing.T, task db.AgentTaskQueue, code string) bool {
	t.Helper()
	if len(task.Routing) == 0 {
		return false
	}
	var trace struct {
		Warnings []service.RoutingProblem `json:"warnings"`
	}
	if err := json.Unmarshal(task.Routing, &trace); err != nil {
		t.Fatalf("routing trace is not an object: %s", task.Routing)
	}
	for _, w := range trace.Warnings {
		if w.Code == code {
			return true
		}
	}
	return false
}
