package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handoff packets (K17): objective required, run must belong to the issue,
// a run may only hand off its own work, packets are immutable and listed in
// order, completion leaves a system packet, the next claim carries the
// latest, and the legacy handoff_note keeps working beside it.

func TestHandoffPacketsCreateListAndCompletionFallback(t *testing.T) {
	issue, task, agent := runningAgentRun(t, "handoff")
	other := dbfx.Issue(t, "handoff other issue")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM handoff_packet WHERE issue_id IN ($1, $2)`, issue, other)
	})
	post := func(issueID string, body map[string]any, headers []string) *testutil.Response {
		req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/handoff-packet", body)
		if headers != nil {
			req = testutil.WithHeaders(req, headers...)
		}
		return testutil.Call(t, testHandler.CreateHandoffPacket, testutil.WithURLParams(req, "id", issueID))
	}
	post(issue, map[string]any{"run_id": task, "objective": "  "}, nil).Want(http.StatusBadRequest)
	post(other, map[string]any{"run_id": task, "objective": "x"}, nil).Want(http.StatusBadRequest)
	// A run may only hand off its own work.
	_, otherTask, otherAgent := runningAgentRun(t, "handoff stranger")
	post(issue, map[string]any{"run_id": task, "objective": "x"}, gateHeaders(otherTask, otherAgent)).Want(http.StatusForbidden)

	var first HandoffPacketResponse
	post(issue, map[string]any{"run_id": task, "objective": "Ship the fix", "decisions": []string{"keep the table", " "}, "failed_attempts": []string{"tried dropping it"}, "next_action": "open the PR"}, gateHeaders(task, agent)).Want(http.StatusCreated).JSON(&first)
	if first.CreatedByType != "agent" || first.CreatedByID == nil || *first.CreatedByID != agent || len(first.Decisions) != 1 || len(first.Evidence) != 0 || first.NextAction != "open the PR" {
		t.Fatalf("agent packet = %+v", first)
	}
	// A member corrects it with a new packet; the first one is untouched.
	var second HandoffPacketResponse
	post(issue, map[string]any{"run_id": task, "objective": "Ship the fix (reviewed)", "evidence": []string{"https://example.test/pr/1"}}, nil).Want(http.StatusCreated).JSON(&second)
	if second.CreatedByType != "member" || second.ID == first.ID {
		t.Fatalf("member packet = %+v", second)
	}
	var latest struct{ Packet *HandoffPacketResponse }
	testutil.Call(t, testHandler.GetLatestHandoffPacket, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/handoff-packet/latest", nil), "id", issue)).Want(http.StatusOK).JSON(&latest)
	if latest.Packet == nil || latest.Packet.ID != second.ID {
		t.Fatalf("latest = %+v", latest.Packet)
	}
	var list struct{ Packets []HandoffPacketResponse }
	testutil.Call(t, testHandler.ListHandoffPackets, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/handoff-packets", nil), "id", issue)).Want(http.StatusOK).JSON(&list)
	if len(list.Packets) != 2 || list.Packets[0].ID != first.ID || list.Packets[0].Objective != "Ship the fix" {
		t.Fatalf("history = %+v", list.Packets)
	}
	// The next claim carries the latest packet beside the legacy note.
	var claim AgentTaskResponse
	claim = taskToResponse(mustTask(t, task), testWorkspaceID)
	claim.HandoffPacket = testHandler.latestHandoffPacket(context.Background(), parseUUID(issue))
	if claim.HandoffPacket == nil || claim.HandoffPacket.ID != second.ID {
		t.Fatalf("claim packet = %+v", claim.HandoffPacket)
	}
	// Completion without a packet of its own leaves a system one.
	completed := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed", "branch_name": "feat/x"})
	testHandler.ensureCompletionHandoffPacket(context.Background(), mustTask(t, completed), "https://example.test/pr/2")
	testHandler.ensureCompletionHandoffPacket(context.Background(), mustTask(t, completed), "https://example.test/pr/2")
	testutil.Call(t, testHandler.GetLatestHandoffPacket, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/handoff-packet/latest", nil), "id", issue)).Want(http.StatusOK).JSON(&latest)
	if latest.Packet == nil || latest.Packet.CreatedByType != "system" || !strings.Contains(latest.Packet.NextAction, "pr/2") || len(latest.Packet.Evidence) != 2 {
		t.Fatalf("system packet = %+v", latest.Packet)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM handoff_packet WHERE run_id = $1`, completed); n != 1 {
		t.Fatalf("completion packets = %d, want exactly one", n)
	}
	// The run that wrote its own packet gets no system one at completion.
	testHandler.ensureCompletionHandoffPacket(context.Background(), mustTask(t, task), "")
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM handoff_packet WHERE run_id = $1`, task); n != 2 {
		t.Fatalf("packets for the handing run = %d, want the two written", n)
	}
}

func mustTask(t *testing.T, id string) db.AgentTaskQueue {
	t.Helper()
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(id))
	if err != nil {
		t.Fatal(err)
	}
	return task
}
