package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// "Show me first" (K69, lot 2): a preview-mode run's writes are held (202,
// nothing changes), a completed run turns them into one decision, approval
// replays them with the run's attribution and journals them as reversible
// effects, a refusal or a failed run discards them, and the effect mode has
// its own endpoint.

func TestPreviewEffects(t *testing.T) {
	ctx := context.Background()
	rememberSettings(t)
	agent := dbfx.Agent(t, "preview agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous", "effect_mode": "preview"})
	issue := dbfx.Issue(t, "preview issue "+uuid.NewString()[:8], testutil.Cols{"status": "todo", "priority": "medium"})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	triageItem := newPendingTriageItem(t, "preview triage "+uuid.NewString()[:8])
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_effect WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND issue_id = $2`, testWorkspaceID, issue)
		testPool.Exec(ctx, `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM workspace_note WHERE workspace_id = $1 AND source_agent_id = $2`, testWorkspaceID, agent)
	})

	// Held: every write answers 202 and changes nothing.
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task, http.MethodPut, "/api/issues/"+issue, map[string]any{"status": "in_progress", "priority": "high"}), "id", issue)).Want(http.StatusAccepted)
	var held struct {
		PendingApproval bool `json:"pending_approval"`
	}
	testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(
		runRequest(agent, task, http.MethodPost, "/api/issues/"+issue+"/comments", map[string]any{"content": "held comment", "type": "comment"}), "id", issue)).Want(http.StatusAccepted).JSON(&held)
	if !held.PendingApproval {
		t.Fatal("a held comment says so")
	}
	testutil.Call(t, testHandler.CreateWorkspaceNote,
		runRequest(agent, task, http.MethodPost, "/api/workspace-notes", map[string]any{"title": "held note", "content": "held", "tags": []string{"k69"}}),
	).Want(http.StatusAccepted)
	testutil.Call(t, testHandler.SetTriageVerdict, testutil.WithURLParams(
		runRequest(agent, task, http.MethodPost, "/api/triage/items/"+triageItem+"/verdict", map[string]any{"verdict": "accept", "reason": "real"}), "id", triageItem)).Want(http.StatusAccepted)
	var status, priority string
	dbfx.QueryRow(t, `SELECT status, priority FROM issue WHERE id = $1`, issue).Scan(&status, &priority)
	if status != "todo" || priority != "medium" {
		t.Fatalf("issue changed while held: %s/%s", status, priority)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND author_type = 'agent'`, issue) != 0 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM workspace_note WHERE source_agent_id = $1`, agent) != 0 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM triage_item WHERE id = $1 AND verdict IS NOT NULL`, triageItem) != 0 {
		t.Fatal("held writes must not land")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND status = 'pending'`, task) != 4 {
		t.Fatal("four held writes journaled as pending")
	}

	// The run completes: one decision lists them; the effects point at it.
	testHandler.settlePendingEffects(ctx, mustTask(t, task), true)
	var decisionID, question string
	dbfx.QueryRow(t, `SELECT id, question FROM issue_decision WHERE task_id = $1 ORDER BY created_at DESC LIMIT 1`, task).Scan(&decisionID, &question)
	if decisionID == "" || dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND decision_id = $2`, task, decisionID) != 4 {
		t.Fatalf("decision %q must own the four effects", decisionID)
	}
	if !containsAll(question, "4 change(s)", "held comment", "held note", "Triage verdict: accept", "yes or no") {
		t.Fatalf("decision question must preview the payload and name the action: %q", question)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'decision_request' AND recipient_id = $2`, issue, testUserID) < 1 {
		t.Fatal("the decision reaches the inbox")
	}

	// Approval replays the writes with the run's attribution and journals them as reversible.
	respondDecision(t, issue, decisionID, map[string]any{"option_id": "apply_effects"}).Want(http.StatusOK)
	dbfx.QueryRow(t, `SELECT status, priority FROM issue WHERE id = $1`, issue).Scan(&status, &priority)
	if status != "in_progress" || priority != "high" {
		t.Fatalf("issue after approval = %s/%s, want in_progress/high", status, priority)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2 AND source_task_id = $3 AND content = 'held comment'`, issue, agent, task) != 1 {
		t.Fatal("the comment lands with the run's attribution")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM workspace_note WHERE source_agent_id = $1 AND source_task_id = $2 AND title = 'held note'`, agent, task) != 1 {
		t.Fatal("the note lands with the run's attribution")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM triage_item WHERE id = $1 AND verdict_agent_id = $2`, triageItem, agent) != 1 {
		t.Fatal("the verdict lands")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND status = 'approved'`, task) != 4 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND status = 'applied' AND reversible`, task) != 5 {
		t.Fatal("approval marks the held writes approved and journals five applied, reversible effects (status, priority, comment, note, verdict)")
	}
	// ...which the undo of the run reverses; the held rows themselves are reported, not reversed.
	var out undoOut
	testutil.Call(t, testHandler.UndoTaskEffects, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/tasks/"+task+"/undo", nil), "id", task)).Want(http.StatusOK).JSON(&out)
	notApplied := 0
	for _, s := range out.Skipped {
		if s.Reason == "not_applied" {
			notApplied++
		}
	}
	if out.Reversed != 5 || notApplied != 4 {
		t.Fatalf("undo after approval = %+v, want 5 reversed and 4 not_applied", out)
	}
	dbfx.QueryRow(t, `SELECT status, priority FROM issue WHERE id = $1`, issue).Scan(&status, &priority)
	if status != "todo" || priority != "medium" {
		t.Fatalf("issue after undo = %s/%s", status, priority)
	}

	// A failed run drops its held writes.
	task2 := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task2, http.MethodPut, "/api/issues/"+issue, map[string]any{"priority": "low"}), "id", issue)).Want(http.StatusAccepted)
	testHandler.settlePendingEffects(ctx, mustTask(t, task2), false)
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND status = 'rejected'`, task2) != 1 || dbfx.Count(t, `SELECT COUNT(*) FROM issue_decision WHERE task_id = $1`, task2) != 0 {
		t.Fatal("a failed run's held writes are rejected without a decision")
	}

	// A refusal discards them too.
	task3 := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task3, http.MethodPut, "/api/issues/"+issue, map[string]any{"priority": "urgent"}), "id", issue)).Want(http.StatusAccepted)
	testHandler.settlePendingEffects(ctx, mustTask(t, task3), true)
	dbfx.QueryRow(t, `SELECT id FROM issue_decision WHERE task_id = $1`, task3).Scan(&decisionID)
	respondDecision(t, issue, decisionID, map[string]any{"option_id": "discard_effects"}).Want(http.StatusOK)
	dbfx.QueryRow(t, `SELECT priority FROM issue WHERE id = $1`, issue).Scan(&priority)
	if priority != "medium" || dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND status = 'rejected'`, task3) != 1 {
		t.Fatalf("refused writes must not land (priority %s)", priority)
	}

	// The mode has its own endpoint.
	testutil.Call(t, testHandler.SetAgentEffectMode, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/agents/"+agent+"/effect-mode", map[string]any{"mode": "sideways"}), "id", agent)).Want(http.StatusBadRequest)
	var mode struct {
		Mode string `json:"mode"`
	}
	testutil.Call(t, testHandler.SetAgentEffectMode, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/agents/"+agent+"/effect-mode", map[string]any{"mode": "apply"}), "id", agent)).Want(http.StatusOK).JSON(&mode)
	testutil.Call(t, testHandler.GetAgentEffectMode, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/agents/"+agent+"/effect-mode", nil), "id", agent)).Want(http.StatusOK).JSON(&mode)
	if mode.Mode != "apply" {
		t.Fatalf("mode = %s, want apply", mode.Mode)
	}
	// Back in apply mode, the same write lands at once.
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task3, http.MethodPut, "/api/issues/"+issue, map[string]any{"priority": "low"}), "id", issue)).Want(http.StatusOK)
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
