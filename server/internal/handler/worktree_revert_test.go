package handler

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// F09 (JEF-26): reverting a conversation to one of its turns.
//
// The endpoint's whole job is to refuse the cases that would lose work and to
// enqueue exactly one request for the rest. The git side of it is pinned in
// internal/daemon/execenv/local_worktree_test.go; the pure "may this run be
// reverted to" matrix lives in worktree_revert_unit_test.go.

// revertFixture builds a project with a worktree local_directory, a runtime
// advertising the revert capability, and one checkpointed run on an issue.
type revertFixture struct {
	IssueID   string
	TaskID    string
	RuntimeID string
	AgentID   string
	DaemonID  string
}

func newRevertFixture(t *testing.T, over ...testutil.Cols) revertFixture {
	t.Helper()
	suffix := time.Now().UnixNano()
	daemonID := fmt.Sprintf("daemon-revert-%d", suffix)

	runtime := dbfx.Runtime(t, fmt.Sprintf("revert-runtime-%d", suffix), testutil.Cols{
		"daemon_id":    daemonID,
		"runtime_mode": "local",
		"metadata": testutil.Raw(fmt.Sprintf(`'{"capabilities":["%s"]}'::jsonb`,
			protocol.DaemonCapabilityWorktreeRevertV1)),
	})
	agent := dbfx.Agent(t, fmt.Sprintf("revert-agent-%d", suffix), runtime)
	project := dbfx.Project(t, fmt.Sprintf("revert-project-%d", suffix))
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    project,
		"workspace_id":  testWorkspaceID,
		"resource_type": "local_directory",
		"resource_ref": testutil.Raw(fmt.Sprintf(
			`'{"local_path":"/tmp/revert-repo","daemon_id":%s,"execution_mode":"worktree"}'::jsonb`,
			quoteJSONString(daemonID))),
		"position": 0,
	})
	issue := dbfx.Issue(t, fmt.Sprintf("revert issue %d", suffix), testutil.Cols{"project_id": project})

	cols := testutil.Cols{
		"issue_id":       issue,
		"runtime_id":     runtime,
		"status":         "completed",
		"branch_name":    "agent/j/revert",
		"checkpoint_sha": "aaaa1111bbbb2222cccc3333dddd4444eeee5555",
		"turn_seq":       1,
	}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	task := dbfx.Task(t, agent, cols)
	return revertFixture{IssueID: issue, TaskID: task, RuntimeID: runtime, AgentID: agent, DaemonID: daemonID}
}

func quoteJSONString(s string) string {
	return `"` + s + `"`
}

func requestRevert(t *testing.T, issueID, taskID string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/runs/"+taskID+"/revert", nil)
	return testutil.Call(t, testHandler.RequestIssueRunRevert,
		testutil.WithURLParams(req, "id", issueID, "taskId", taskID))
}

func TestWorktreeRevertAcceptsACheckpointedRun(t *testing.T) {
	f := newRevertFixture(t)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID) })

	var out WorktreeRevertResponse
	requestRevert(t, f.IssueID, f.TaskID).Want(http.StatusAccepted).JSON(&out)

	if out.RequestID == "" {
		t.Fatal("no request id returned; the client has nothing to poll")
	}
	if out.Status != worktreeRevertStatusPending {
		t.Fatalf("status = %q, want %q", out.Status, worktreeRevertStatusPending)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID); n != 1 {
		t.Fatalf("worktree_revert_request rows = %d, want 1", n)
	}
}

// The second revert must be refused, not queued: two of them racing on one
// branch is exactly the case where the loser reads a tip the winner has
// already moved.
func TestWorktreeRevertRefusesASecondInFlightRequest(t *testing.T) {
	f := newRevertFixture(t)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID) })

	requestRevert(t, f.IssueID, f.TaskID).Want(http.StatusAccepted)
	requestRevert(t, f.IssueID, f.TaskID).Want(http.StatusConflict)

	if n := dbfx.Count(t, `SELECT count(*) FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID); n != 1 {
		t.Fatalf("worktree_revert_request rows = %d, want 1 — a second request was queued", n)
	}
}

func TestWorktreeRevertRejectsARunWithoutACheckpoint(t *testing.T) {
	f := newRevertFixture(t, testutil.Cols{"checkpoint_sha": nil, "turn_seq": nil})

	requestRevert(t, f.IssueID, f.TaskID).Want(http.StatusBadRequest)

	if n := dbfx.Count(t, `SELECT count(*) FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID); n != 0 {
		t.Fatalf("a run with no checkpoint queued %d revert(s)", n)
	}
}

// Without the capability nothing may be emitted: an old daemon would claim the
// request, skip the field it does not know, and never report — leaving the user
// on a spinner that can only end in the stale-claim sweeper.
func TestWorktreeRevertRefusesARuntimeWithoutTheCapability(t *testing.T) {
	f := newRevertFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata = '{"capabilities":[]}'::jsonb WHERE id = $1`, f.RuntimeID)

	requestRevert(t, f.IssueID, f.TaskID).Want(http.StatusBadRequest)

	if n := dbfx.Count(t, `SELECT count(*) FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID); n != 0 {
		t.Fatalf("a runtime without the capability queued %d revert(s)", n)
	}
}

// The taskId path segment must not be a way to reach a run on another issue.
func TestWorktreeRevertRejectsARunFromAnotherIssue(t *testing.T) {
	f := newRevertFixture(t)
	other := dbfx.Issue(t, "an unrelated issue")

	requestRevert(t, other, f.TaskID).Want(http.StatusNotFound)
}

// The whole point of splitting the work: the daemon moves refs, and only a
// success removes the runs after the target turn.
func TestWorktreeRevertResultRemovesTheLaterTurns(t *testing.T) {
	f := newRevertFixture(t)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID) })

	later := dbfx.Task(t, f.AgentID, testutil.Cols{
		"issue_id":       f.IssueID,
		"runtime_id":     f.RuntimeID,
		"status":         "completed",
		"branch_name":    "agent/j/revert",
		"checkpoint_sha": "bbbb1111bbbb2222cccc3333dddd4444eeee5555",
		"turn_seq":       2,
	})

	var enqueued WorktreeRevertResponse
	requestRevert(t, f.IssueID, f.TaskID).Want(http.StatusAccepted).JSON(&enqueued)
	// The daemon only ever reports on a request it claimed.
	dbfx.Exec(t, `UPDATE worktree_revert_request SET status = 'claimed', claimed_at = now() WHERE id = $1`, enqueued.RequestID)

	reportRevert(t, f.RuntimeID, enqueued.RequestID, map[string]any{"status": "completed"}).Want(http.StatusOK)

	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id = $1`, later); n != 0 {
		t.Fatal("the run after the target turn survived the revert")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id = $1`, f.TaskID); n != 1 {
		t.Fatal("the target turn's own run was deleted")
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM worktree_revert_request WHERE id = $1`, enqueued.RequestID).Scan(&status)
	if status != worktreeRevertStatusDone {
		t.Fatalf("request status = %q, want %q", status, worktreeRevertStatusDone)
	}
}

// A refusal must leave the conversation exactly as it was, and must carry the
// daemon's cause verbatim — "the branch moved" is the answer the user needs.
func TestWorktreeRevertResultFailureKeepsTheLaterTurns(t *testing.T) {
	f := newRevertFixture(t)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID) })

	later := dbfx.Task(t, f.AgentID, testutil.Cols{
		"issue_id":       f.IssueID,
		"runtime_id":     f.RuntimeID,
		"status":         "completed",
		"branch_name":    "agent/j/revert",
		"checkpoint_sha": "cccc1111bbbb2222cccc3333dddd4444eeee5555",
		"turn_seq":       2,
	})

	var enqueued WorktreeRevertResponse
	requestRevert(t, f.IssueID, f.TaskID).Want(http.StatusAccepted).JSON(&enqueued)
	dbfx.Exec(t, `UPDATE worktree_revert_request SET status = 'claimed', claimed_at = now() WHERE id = $1`, enqueued.RequestID)

	var out WorktreeRevertResponse
	reportRevert(t, f.RuntimeID, enqueued.RequestID, map[string]any{
		"status": "failed",
		"error":  "revert refused: branch agent/j/revert has moved off that run's checkpoint",
	}).Want(http.StatusOK).JSON(&out)

	if out.Status != worktreeRevertStatusFailed {
		t.Fatalf("status = %q, want %q", out.Status, worktreeRevertStatusFailed)
	}
	if out.Error == "" {
		t.Fatal("the refusal reached the client without its cause")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE id = $1`, later); n != 1 {
		t.Fatal("a refused revert deleted the later run anyway")
	}
}

// The daemon retries terminal reports. A second delivery of a destructive
// action must change nothing.
func TestWorktreeRevertResultIsIdempotent(t *testing.T) {
	f := newRevertFixture(t)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID) })

	var enqueued WorktreeRevertResponse
	requestRevert(t, f.IssueID, f.TaskID).Want(http.StatusAccepted).JSON(&enqueued)
	dbfx.Exec(t, `UPDATE worktree_revert_request SET status = 'claimed', claimed_at = now() WHERE id = $1`, enqueued.RequestID)

	reportRevert(t, f.RuntimeID, enqueued.RequestID, map[string]any{"status": "completed"}).Want(http.StatusOK)

	var out WorktreeRevertResponse
	reportRevert(t, f.RuntimeID, enqueued.RequestID, map[string]any{
		"status": "failed", "error": "a retry that must not overwrite the outcome",
	}).Want(http.StatusOK).JSON(&out)

	if out.Status != worktreeRevertStatusDone {
		t.Fatalf("a retried report rewrote the outcome: status = %q, want %q", out.Status, worktreeRevertStatusDone)
	}
}

// The heartbeat is the only delivery channel, and the claim has to carry
// everything the daemon needs in one payload.
func TestWorktreeRevertHeartbeatClaimCarriesTheWholePayload(t *testing.T) {
	f := newRevertFixture(t)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM worktree_revert_request WHERE target_task_id = $1`, f.TaskID) })

	later := dbfx.Task(t, f.AgentID, testutil.Cols{
		"issue_id":       f.IssueID,
		"runtime_id":     f.RuntimeID,
		"status":         "completed",
		"branch_name":    "agent/j/revert",
		"checkpoint_sha": "dddd1111bbbb2222cccc3333dddd4444eeee5555",
		"turn_seq":       2,
	})
	requestRevert(t, f.IssueID, f.TaskID).Want(http.StatusAccepted)

	pending := testHandler.claimWorktreeRevertForHeartbeat(t.Context(), parseUUID(f.RuntimeID))
	if pending == nil {
		t.Fatal("the heartbeat claimed nothing for a runtime with a pending revert")
	}
	if pending.LocalPath != "/tmp/revert-repo" {
		t.Fatalf("local_path = %q, want the project's local_directory path", pending.LocalPath)
	}
	if pending.Branch != "agent/j/revert" {
		t.Fatalf("branch = %q", pending.Branch)
	}
	if pending.Checkpoint != "aaaa1111bbbb2222cccc3333dddd4444eeee5555" {
		t.Fatalf("checkpoint = %q", pending.Checkpoint)
	}
	if len(pending.LaterTaskIDs) != 1 || pending.LaterTaskIDs[0] != later {
		t.Fatalf("later_task_ids = %v, want [%s]", pending.LaterTaskIDs, later)
	}

	// The claim is exactly-once: a second heartbeat must not hand the same
	// destructive request to a second worker.
	if again := testHandler.claimWorktreeRevertForHeartbeat(t.Context(), parseUUID(f.RuntimeID)); again != nil {
		t.Fatalf("a second heartbeat re-claimed the same request: %+v", again)
	}
}

func reportRevert(t *testing.T, runtimeID, requestID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost,
		fmt.Sprintf("/api/daemon/runtimes/%s/worktree-revert/%s/result", runtimeID, requestID), body)
	return testutil.Call(t, testHandler.ReportWorktreeRevertResult,
		testutil.WithURLParams(req, "runtimeId", runtimeID, "requestId", requestID))
}
