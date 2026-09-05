package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Agent duel (K39): two different agents get one independent run each on
// the same issue; a second duel is refused while one runs; a failed
// candidate with a retry pending is not final; a candidate that failed for
// good makes the duel inconclusive; two completed runs get an arbiter
// verdict with measured cost/duration; the human confirms (possibly
// against the arbiter) and the issue itself is never changed.

func TestAgentDuelLifecycle(t *testing.T) {
	agentA := dbfx.Agent(t, "duel agent a", handlerTestRuntimeID(t))
	agentB := dbfx.Agent(t, "duel agent b", handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "Duel issue", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, agentA, agentB)
		testPool.Exec(context.Background(), `DELETE FROM agent_duel WHERE workspace_id = $1`, testWorkspaceID)
	})
	ctx := context.Background()
	start := func(a, b string) *testutil.Response {
		return testutil.Call(t, testHandler.StartAgentDuel, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issue+"/duel", map[string]any{"agent_a_id": a, "agent_b_id": b}), "id", issue))
	}
	get := func() AgentDuelResponse {
		var out struct{ Duel AgentDuelResponse }
		testutil.Call(t, testHandler.GetIssueAgentDuel, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/duel", nil), "id", issue)).Want(http.StatusOK).JSON(&out)
		return out.Duel
	}
	if res := start(agentA, agentA).Want(http.StatusBadRequest); res.Map()["code"] != ErrCodeDuelIdentical {
		t.Fatalf("identical agents = %v", res.Map())
	}
	var out struct{ Duel AgentDuelResponse }
	start(agentA, agentB).Want(http.StatusCreated).JSON(&out)
	d := out.Duel
	if d.Status != "running" || d.A.AgentID != agentA || d.B.AgentID != agentB || d.A.TaskStatus != "queued" || d.B.TaskStatus != "queued" || d.A.TaskID == d.B.TaskID {
		t.Fatalf("duel = %+v", d)
	}
	if res := start(agentA, agentB).Want(http.StatusConflict); res.Map()["code"] != ErrCodeDuelActive {
		t.Fatalf("second duel = %v", res.Map())
	}
	// A completes: its side settles, the duel keeps running.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', started_at = now() - interval '90 seconds', completed_at = now() WHERE id = $1`, d.A.TaskID)
	testHandler.updateDuelBarrier(ctx, mustTask(t, d.A.TaskID))
	if d = get(); d.Status != "running" || d.A.Outcome == nil || *d.A.Outcome != "completed" || d.B.Outcome != nil || d.A.DurationSeconds < 89 {
		t.Fatalf("after A = %+v", d)
	}
	// B fails with a retry pending: not final.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', failure_reason = 'runtime_offline', completed_at = now() WHERE id = $1`, d.B.TaskID)
	retry := dbfx.Task(t, agentB, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "queued", "parent_task_id": d.B.TaskID, "retry_of_task_id": d.B.TaskID})
	testHandler.updateDuelBarrier(ctx, mustTask(t, d.B.TaskID))
	if d = get(); d.Status != "running" || d.B.Outcome != nil {
		t.Fatalf("a failed candidate with a retry pending must not settle: %+v", d)
	}
	// The retry fails for good: inconclusive, no arbiter needed.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', failure_reason = 'agent_error', completed_at = now() WHERE id = $1`, retry)
	testHandler.updateDuelBarrier(ctx, mustTask(t, retry))
	if d = get(); d.Status != "inconclusive" || d.B.TaskID != retry || d.SettledAt == nil {
		t.Fatalf("inconclusive duel = %+v", d)
	}
	testutil.Call(t, testHandler.ConfirmAgentDuel, testutil.WithURLParams(newRequest(http.MethodPost, "/api/duels/"+d.ID+"/confirm", map[string]any{"winner": "a"}), "id", d.ID)).Want(http.StatusConflict)

	// Second duel: both complete, the arbiter scores them, the human disagrees and confirms A.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"winner":"b","quality_a":55,"quality_b":85,"summary_a":"A skipped the tests.","summary_b":"B ran the suite.","reasoning":"B verified its work."}`))
	start(agentA, agentB).Want(http.StatusCreated).JSON(&out)
	d = out.Duel
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": d.A.TaskID, "model": "stub", "cost_usd_ticks": 1200})
	dbfx.Insert(t, "task_message", testutil.Cols{"task_id": d.B.TaskID, "seq": 1, "type": "tool_use", "tool": "bash", "content": "go test ./..."})
	dbfx.Insert(t, "task_message", testutil.Cols{"task_id": d.B.TaskID, "seq": 2, "type": "text", "content": "Suite green, PR opened."})
	for _, id := range []string{d.A.TaskID, d.B.TaskID} {
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', started_at = now() - interval '30 seconds', completed_at = now() WHERE id = $1`, id)
		testHandler.updateDuelBarrier(ctx, mustTask(t, id))
	}
	d = get()
	if d.Status != "verdict_ready" || d.ArbiterWinner == nil || *d.ArbiterWinner != "b" || d.B.QualityScore == nil || *d.B.QualityScore != 85 || d.B.ToolCalls != 1 || d.A.CostUsdTicks != 1200 || d.Reasoning == "" || d.ArbiterError != nil {
		t.Fatalf("arbitrated duel = %+v", d)
	}
	var issueStatus string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issue).Scan(&issueStatus)
	if issueStatus != "in_progress" {
		t.Fatalf("issue status = %s, a duel must not change it", issueStatus)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE id = $1 AND assignee_id IS NULL`, issue); n != 1 {
		t.Fatal("a duel must not assign the issue")
	}
	testutil.Call(t, testHandler.ConfirmAgentDuel, testutil.WithURLParams(newRequest(http.MethodPost, "/api/duels/"+d.ID+"/confirm", map[string]any{"winner": "nope"}), "id", d.ID)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ConfirmAgentDuel, testutil.WithURLParams(newRequest(http.MethodPost, "/api/duels/"+d.ID+"/confirm", map[string]any{"winner": "a"}), "id", d.ID)).Want(http.StatusOK).JSON(&out)
	if out.Duel.Status != "confirmed" || out.Duel.Winner == nil || *out.Duel.Winner != "a" || out.Duel.ConfirmedBy == nil || *out.Duel.ConfirmedBy != testUserID {
		t.Fatalf("confirmed duel = %+v", out.Duel)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2 AND details->>'winner' = 'a' AND details->>'arbiter_winner' = 'b'`, AuditDuel, issue); n != 1 {
		t.Fatalf("audit rows for the confirmation = %d", n)
	}
	testutil.Call(t, testHandler.ConfirmAgentDuel, testutil.WithURLParams(newRequest(http.MethodPost, "/api/duels/"+d.ID+"/confirm", map[string]any{"winner": "b"}), "id", d.ID)).Want(http.StatusConflict)
}

// Without an LLM the duel still settles with measured metrics, and the human decides.
func TestAgentDuelSettlesWithoutArbiter(t *testing.T) {
	agentA := dbfx.Agent(t, "duel noarb a", handlerTestRuntimeID(t))
	agentB := dbfx.Agent(t, "duel noarb b", handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "Duel without arbiter")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, agentA, agentB)
		testPool.Exec(context.Background(), `DELETE FROM agent_duel WHERE issue_id = $1`, issue)
	})
	prev := testHandler.LLM
	testHandler.LLM = nil
	t.Cleanup(func() { testHandler.LLM = prev })
	var out struct{ Duel AgentDuelResponse }
	testutil.Call(t, testHandler.StartAgentDuel, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issue+"/duel", map[string]any{"agent_a_id": agentA, "agent_b_id": agentB}), "id", issue)).Want(http.StatusCreated).JSON(&out)
	for _, id := range []string{out.Duel.A.TaskID, out.Duel.B.TaskID} {
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, id)
		testHandler.updateDuelBarrier(context.Background(), mustTask(t, id))
	}
	testutil.Call(t, testHandler.GetAgentDuel, testutil.WithURLParams(newRequest(http.MethodGet, "/api/duels/"+out.Duel.ID, nil), "id", out.Duel.ID)).Want(http.StatusOK).JSON(&out)
	if out.Duel.Status != "verdict_ready" || out.Duel.ArbiterWinner != nil || out.Duel.ArbiterError == nil || *out.Duel.ArbiterError != "llm_disabled" {
		t.Fatalf("duel without arbiter = %+v", out.Duel)
	}
}
