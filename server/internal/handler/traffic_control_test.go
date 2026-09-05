package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Traffic control (K18): a run's edited paths come from its tool calls;
// overlapping another active run alerts, disjoint paths never do; a
// human's dirty files reported on the heartbeat alert and point at the
// latest handoff packet; the workspace may ask for a pause; a daemon that
// sends no dirty field changes nothing; ignore and auto-resolve.

func editMessages(t *testing.T, task, agent string, seq int, paths ...string) {
	t.Helper()
	msgs := []map[string]any{}
	for i, p := range paths {
		msgs = append(msgs, map[string]any{"seq": seq + i, "type": "tool_use", "tool": "Edit", "input": map[string]any{"file_path": p, "old_string": "a", "new_string": "b"}})
	}
	reportMessages(t, task, agent, msgs)
}

func conflictsOf(t *testing.T, issue string) []TrafficConflictResponse {
	t.Helper()
	var out struct{ Conflicts []TrafficConflictResponse }
	testutil.Call(t, testHandler.ListTrafficConflicts, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/traffic-conflicts", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
	return out.Conflicts
}

func TestTrafficControlAgentsHumansPauseAndIgnore(t *testing.T) {
	rememberSettings(t)
	issueA, taskA, agentA := runningAgentRun(t, "traffic a")
	issueB, taskB, agentB := runningAgentRun(t, "traffic b")
	issueC, taskC, agentC := runningAgentRun(t, "traffic c")
	dbfx.Exec(t, `UPDATE agent_task_queue SET work_dir = '/w/tree-a' WHERE id = $1`, taskA)
	t.Cleanup(func() {
		for _, id := range []string{taskA, taskB, taskC} {
			testPool.Exec(context.Background(), `DELETE FROM task_message WHERE task_id = $1`, id)
		}
		testPool.Exec(context.Background(), `DELETE FROM traffic_conflict WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE type = $1 AND workspace_id = $2`, TrafficInboxType, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM handoff_packet WHERE issue_id = $1`, issueA)
	})
	// A edits src/a.go (absolute path under its work dir → repo-relative).
	editMessages(t, taskA, agentA, 1, "/w/tree-a/src/a.go", "docs/a.md")
	if got := jsonStrings(mustTask(t, taskA).TouchedPaths); len(got) != 2 || got[0] != "docs/a.md" && got[1] != "docs/a.md" {
		t.Fatalf("touched A = %v", got)
	}
	if len(conflictsOf(t, issueA)) != 0 {
		t.Fatal("a lone run has no conflict")
	}
	// C edits disjoint paths: no alert. B edits src/a.go: agent conflict on B against A, one inbox item.
	editMessages(t, taskC, agentC, 1, "other/file.go")
	editMessages(t, taskB, agentB, 1, "src/a.go", "docs/b.md")
	if len(conflictsOf(t, issueC)) != 0 {
		t.Fatal("disjoint paths never alert")
	}
	bc := conflictsOf(t, issueB)
	if len(bc) != 1 || bc[0].Kind != "agent" || bc[0].OtherTaskID == nil || *bc[0].OtherTaskID != taskA || len(bc[0].Paths) != 1 || bc[0].Paths[0] != "src/a.go" || bc[0].Status != "active" {
		t.Fatalf("agent conflict = %+v", bc)
	}
	editMessages(t, taskB, agentB, 5, "src/a.go")
	if len(conflictsOf(t, issueB)) != 1 {
		t.Fatal("the same overlap is not filed twice")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2`, TrafficInboxType, issueB); n < 1 {
		t.Fatal("the conflict must reach the attention inbox")
	}
	// A human's dirty checkout arrives on the heartbeat; a daemon without the field is fine too.
	rt := handlerTestRuntimeID(t)
	testutil.Call(t, testHandler.DaemonHeartbeat, newDaemonTokenRequest("POST", "/api/daemon/heartbeat", map[string]any{"runtime_id": rt}, testWorkspaceID, "traffic-daemon")).Want(http.StatusOK)
	testutil.Call(t, testHandler.DaemonHeartbeat, newDaemonTokenRequest("POST", "/api/daemon/heartbeat", map[string]any{"runtime_id": rt, "dirty_checkouts": []map[string]any{{"root": "/home/u/repo", "paths": []string{"src/human.go", "README.md"}}}}, testWorkspaceID, "traffic-daemon")).Want(http.StatusOK)
	var packet HandoffPacketResponse
	testutil.Call(t, testHandler.CreateHandoffPacket, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issueA+"/handoff-packet", map[string]any{"run_id": taskA, "objective": "context"}), "id", issueA)).Want(http.StatusCreated).JSON(&packet)
	editMessages(t, taskA, agentA, 3, "/w/tree-a/src/human.go")
	ac := conflictsOf(t, issueA)
	if len(ac) != 1 || ac[0].Kind != "human" || ac[0].HandoffPacketID == nil || *ac[0].HandoffPacketID != packet.ID || ac[0].Paths[0] != "src/human.go" {
		t.Fatalf("human conflict = %+v", ac)
	}
	if mustTask(t, taskA).PauseRequestedAt.Valid {
		t.Fatal("alert only by default: no pause")
	}
	// Ignore closes it; a new overlap with pause_on_conflict pauses the run.
	testutil.Call(t, testHandler.IgnoreTrafficConflict, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issueA+"/traffic-conflicts/"+ac[0].ID+"/ignore", nil), "id", issueA, "cid", ac[0].ID)).Want(http.StatusOK)
	if got := conflictsOf(t, issueA); got[0].Status != "ignored" {
		t.Fatalf("ignored = %+v", got[0])
	}
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"traffic_control":{"pause_on_conflict":true}}'::jsonb WHERE id = $1`, testWorkspaceID)
	editMessages(t, taskA, agentA, 4, "/w/tree-a/README.md")
	if got := conflictsOf(t, issueA); len(got) != 2 || !mustTask(t, taskA).PauseRequestedAt.Valid {
		t.Fatalf("pause_on_conflict: conflicts=%d pause_requested=%v", len(got), mustTask(t, taskA).PauseRequestedAt.Valid)
	}
	// A finished run's conflicts resolve on read.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskB)
	if got := conflictsOf(t, issueB); got[0].Status != "resolved" || got[0].ResolvedAt == nil {
		t.Fatalf("resolved = %+v", got[0])
	}
}
