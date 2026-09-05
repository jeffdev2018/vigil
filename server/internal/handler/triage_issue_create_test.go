package handler

// Two API create paths answer to a triage source of their own: an agent
// filing an issue on its own initiative (agent_create) and natural-language
// capture (quick_create). Both default to direct, so an unconfigured
// workspace keeps creating issues exactly as before.

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/triage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// agentCreatesIssue files an issue as the agent, speaking from taskID — the
// only shape resolveActor accepts as an agent actor, and the one that stamps
// origin_type=agent_create.
func agentCreatesIssue(t *testing.T, agentID, taskID, title string) *testutil.Response {
	t.Helper()
	resp := testutil.Call(t, testHandler.CreateIssue, asRun(
		newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title": title, "status": "todo", "priority": "medium",
		}), agentID, taskID))
	var created struct {
		ID string `json:"id"`
	}
	if resp.Code == http.StatusCreated {
		resp.JSON(&created)
		dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, created.ID)
	}
	return resp
}

func newTriageTestAgentWithTask(t *testing.T, name string) (agentID, taskID string) {
	t.Helper()
	agentID = seedAllowListedAgent(t, name, testUserID, "private")
	taskID = dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "running",
	})
	cleanupTriageSourceKind(t, triage.SourceAgentCreate, agentID)
	return agentID, taskID
}

// triageSourceByRef loads the source a create path registered, failing the
// test when it was never registered at all.
func triageSourceByRef(t *testing.T, kind, refID string) db.TriageSource {
	t.Helper()
	src, err := testHandler.Queries.GetTriageSourceByRef(context.Background(), db.GetTriageSourceByRefParams{
		WorkspaceID: parseUUID(testWorkspaceID), Kind: kind, RefID: parseUUID(refID),
	})
	if err != nil {
		t.Fatalf("triage source %s/%s not registered: %v", kind, refID, err)
	}
	return src
}

func TestAgentCreatedIssueRegistersItsSourceAndStaysDirect(t *testing.T) {
	agentID, taskID := newTriageTestAgentWithTask(t, "Triage agent-create direct")

	agentCreatesIssue(t, agentID, taskID, "Agent-filed issue, direct source").Want(http.StatusCreated)

	// The source now exists so a human can find it in settings and gate it,
	// without a queue row per issue on the hot create path.
	src := triageSourceByRef(t, triage.SourceAgentCreate, agentID)
	if src.Mode != string(triage.ModeDirect) {
		t.Fatalf("new agent source mode = %q, want direct", src.Mode)
	}
	if len(triageItemsForSource(t, triage.SourceAgentCreate, agentID)) != 0 {
		t.Fatal("a direct agent source must not write a queue row per created issue")
	}
}

func TestGatedAgentSourceHoldsTheIssueInsteadOfCreatingIt(t *testing.T) {
	agentID, taskID := newTriageTestAgentWithTask(t, "Triage agent-create gate")
	agentCreatesIssue(t, agentID, taskID, "Seed so the source exists").Want(http.StatusCreated)
	setTriageSourceModeForKind(t, triage.SourceAgentCreate, agentID, string(triage.ModeGate))

	var out struct {
		Code   string `json:"code"`
		ItemID string `json:"item_id"`
		State  string `json:"state"`
	}
	agentCreatesIssue(t, agentID, taskID, "Held agent proposal").
		Want(http.StatusAccepted).JSON(&out)
	if out.Code != "triage_held" {
		t.Fatalf("code = %q, want triage_held so a client can branch on it", out.Code)
	}
	if out.State != triage.StatePending || out.ItemID == "" {
		t.Fatalf("response = %+v, want a pending queue entry", out)
	}

	var queued int
	for _, item := range triageItemsForSource(t, triage.SourceAgentCreate, agentID) {
		if item.State == triage.StatePending && item.Title == "Held agent proposal" {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("pending items titled after the held proposal = %d, want 1", queued)
	}
}

func TestBlockedAgentSourceRefusesTheCreate(t *testing.T) {
	agentID, taskID := newTriageTestAgentWithTask(t, "Triage agent-create blocked")
	agentCreatesIssue(t, agentID, taskID, "Seed so the source exists").Want(http.StatusCreated)
	setTriageSourceModeForKind(t, triage.SourceAgentCreate, agentID, string(triage.ModeBlocked))

	agentCreatesIssue(t, agentID, taskID, "Refused agent proposal").Want(http.StatusForbidden)

	var dropped int
	for _, item := range triageItemsForSource(t, triage.SourceAgentCreate, agentID) {
		if item.State == triage.StateDropped {
			dropped++
		}
	}
	if dropped != 1 {
		t.Fatalf("dropped audit rows = %d, want 1", dropped)
	}
}

// quickCreatesIssue mirrors the daemon CLI: the issue is stamped with the
// quick-create task that produced it, which must be a real queued task.
func quickCreatesIssue(t *testing.T, agentID, originTaskID, title string) *testutil.Response {
	t.Helper()
	resp := testutil.Call(t, testHandler.CreateIssue, asRun(
		newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
			"title": title, "status": "todo",
			"origin_type": "quick_create",
			"origin_id":   originTaskID,
		}), agentID, originTaskID))
	var created struct {
		ID string `json:"id"`
	}
	if resp.Code == http.StatusCreated {
		resp.JSON(&created)
		dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, created.ID)
	}
	return resp
}

func TestGatedQuickCreateHoldsTheIssue(t *testing.T) {
	// Quick-create is one source per workspace, so its ref is the workspace.
	cleanupTriageSourceKind(t, triage.SourceQuickCreate, testWorkspaceID)
	agentID := seedAllowListedAgent(t, "Triage quick-create", testUserID, "private")
	originTaskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"status":     "running",
		"context": `{"type":"quick_create","prompt":"file it","requester_id":"` + testUserID +
			`","workspace_id":"` + testWorkspaceID + `"}`,
	})
	quickCreatesIssue(t, agentID, originTaskID, "Quick create seeds the source").Want(http.StatusCreated)
	setTriageSourceModeForKind(t, triage.SourceQuickCreate, testWorkspaceID, string(triage.ModeGate))

	var out struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	quickCreatesIssue(t, agentID, originTaskID, "Held quick capture").Want(http.StatusAccepted).JSON(&out)
	if out.Code != "triage_held" || out.State != triage.StatePending {
		t.Fatalf("response = %+v, want a pending queue entry", out)
	}
}
