package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Undo for agent actions (K69): a run's writes through the API are journaled
// with their previous state; a member reverses a whole run newest first, or
// one effect; a second undo finds nothing to do; an effect outside the window
// or a non-reversible one is reported as skipped, never silently; undoing too
// many of one agent's runs in a day lowers its trust mode and files an inbox
// item; the settings endpoint validates its bounds.

// runRequest stamps the headers the auth middleware writes for a run's task token.
func runRequest(agentID, taskID, method, path string, body any) *http.Request {
	req := newRequest(method, path, body)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	return req
}

type undoOut struct {
	Reversed int `json:"reversed"`
	Skipped  []struct {
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	} `json:"skipped"`
	Breaker struct {
		Tripped   bool   `json:"tripped"`
		TrustMode string `json:"trust_mode"`
	} `json:"breaker"`
}

func TestUndoAgentEffects(t *testing.T) {
	ctx := context.Background()
	rememberSettings(t)
	agent := dbfx.Agent(t, "undo agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	issue := dbfx.Issue(t, "undo issue "+uuid.NewString()[:8], testutil.Cols{"status": "todo", "priority": "medium"})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	triageItem := newPendingTriageItem(t, "undo triage "+uuid.NewString()[:8])
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_effect WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE type = $1 AND workspace_id = $2`, InboxTypeUndoBreaker, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM trust_mode_change WHERE agent_id = $1`, agent)
		testPool.Exec(ctx, `DELETE FROM workspace_note WHERE workspace_id = $1 AND source_agent_id = $2`, testWorkspaceID, agent)
	})
	var prevTitle string
	dbfx.QueryRow(t, `SELECT title FROM issue WHERE id = $1`, issue).Scan(&prevTitle)

	// The run changes three fields, posts a comment, writes a note, suggests a verdict.
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task, http.MethodPut, "/api/issues/"+issue, map[string]any{"status": "in_progress", "priority": "high", "title": "renamed by the run"}),
		"id", issue)).Want(http.StatusOK)
	var comment struct {
		ID string `json:"id"`
	}
	testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(
		runRequest(agent, task, http.MethodPost, "/api/issues/"+issue+"/comments", map[string]any{"content": "the run says hi", "type": "comment"}),
		"id", issue)).Want(http.StatusCreated).JSON(&comment)
	var note struct {
		ID string `json:"id"`
	}
	testutil.Call(t, testHandler.CreateWorkspaceNote,
		runRequest(agent, task, http.MethodPost, "/api/workspace-notes", map[string]any{"title": "run note", "content": "written by the run", "tags": []string{}}),
	).Want(http.StatusCreated).JSON(&note)
	testutil.Call(t, testHandler.SetTriageVerdict, testutil.WithURLParams(
		runRequest(agent, task, http.MethodPost, "/api/triage/items/"+triageItem+"/verdict", map[string]any{"verdict": "dismiss", "reason": "noise"}),
		"id", triageItem)).Want(http.StatusOK)

	// A human's own edit is not journaled.
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issue, map[string]any{"description": "human context"}), "id", issue)).Want(http.StatusOK)

	// The issue lists the run's effects on it: status, priority, title, comment.
	var listed struct {
		Effects []struct {
			Kind         string `json:"kind"`
			AgentName    string `json:"agent_name"`
			Reversible   bool   `json:"reversible"`
			WithinWindow bool   `json:"within_window"`
		} `json:"effects"`
		WindowHours int `json:"window_hours"`
	}
	testutil.Call(t, testHandler.ListIssueAgentEffects, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issue+"/agent-effects", nil), "id", issue)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Effects) != 4 || listed.WindowHours != 24 {
		t.Fatalf("listed %d effects (window %d), want 4 in a 24h window: %+v", len(listed.Effects), listed.WindowHours, listed.Effects)
	}
	for _, e := range listed.Effects {
		if !e.Reversible || !e.WithinWindow || e.AgentName == "" {
			t.Fatalf("effect %+v must be reversible, within window and name its agent", e)
		}
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1`, task); n != 6 {
		t.Fatalf("journaled %d effects for the run, want 6 (3 issue fields, comment, note, verdict)", n)
	}

	// Undo the run: everything comes back, newest first.
	var out undoOut
	testutil.Call(t, testHandler.UndoTaskEffects, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/tasks/"+task+"/undo", nil), "id", task)).Want(http.StatusOK).JSON(&out)
	if out.Reversed != 6 || len(out.Skipped) != 0 {
		t.Fatalf("undo = %+v, want 6 reversed and nothing skipped", out)
	}
	var status, priority, title, description string
	dbfx.QueryRow(t, `SELECT status, priority, title, COALESCE(description, '') FROM issue WHERE id = $1`, issue).Scan(&status, &priority, &title, &description)
	if status != "todo" || priority != "medium" || title != prevTitle || description != "human context" {
		t.Fatalf("issue after undo = %s/%s/%q/%q, want todo/medium/%q with the human's description kept", status, priority, title, description, prevTitle)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE id = $1`, comment.ID) != 0 {
		t.Fatal("the run's comment must be gone")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM workspace_note WHERE id = $1`, note.ID) != 0 {
		t.Fatal("the run's note must be gone")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM triage_item WHERE id = $1 AND verdict IS NULL AND verdict_agent_id IS NULL`, triageItem) != 1 {
		t.Fatal("the suggested verdict must be cleared")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2`, testWorkspaceID, AuditAgentEffectReversed) < 6 {
		t.Fatal("every reversal is audited")
	}

	// A second undo has nothing left to do and says so per effect.
	testutil.Call(t, testHandler.UndoTaskEffects, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/tasks/"+task+"/undo", nil), "id", task)).Want(http.StatusOK).JSON(&out)
	if out.Reversed != 0 || len(out.Skipped) != 6 || out.Skipped[0].Reason != "already_reversed" {
		t.Fatalf("second undo = %+v, want 0 reversed, 6 already_reversed", out)
	}

	// Outside the window: skipped, the change stands. Non-reversible: skipped too.
	task2 := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task2, http.MethodPut, "/api/issues/"+issue, map[string]any{"priority": "urgent"}), "id", issue)).Want(http.StatusOK)
	dbfx.Exec(t, `UPDATE agent_effect SET created_at = now() - interval '48 hours' WHERE task_id = $1`, task2)
	// A journaled-but-not-reversible effect (the shape CreateIssue records).
	dbfx.Insert(t, "agent_effect", testutil.Cols{
		"id": uuid.NewString(), "workspace_id": testWorkspaceID, "task_id": task2, "agent_id": agent, "issue_id": issue,
		"kind": "issue_create", "target_type": "issue", "target_id": issue, "reversible": false,
	})
	testutil.Call(t, testHandler.UndoTaskEffects, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/tasks/"+task2+"/undo", nil), "id", task2)).Want(http.StatusOK).JSON(&out)
	reasons := map[string]string{}
	for _, s := range out.Skipped {
		reasons[s.Kind] = s.Reason
	}
	if out.Reversed != 0 || reasons["issue_field"] != "window_expired" || reasons["issue_create"] != "not_reversible" {
		t.Fatalf("expired/non-reversible undo = %+v, want both skipped with their reasons", out)
	}
	dbfx.QueryRow(t, `SELECT priority FROM issue WHERE id = $1`, issue).Scan(&priority)
	if priority != "urgent" {
		t.Fatalf("priority after expired undo = %s, want urgent (untouched)", priority)
	}

	// One effect on its own, from the issue's list.
	task3 := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task3, http.MethodPut, "/api/issues/"+issue, map[string]any{"status": "in_progress"}), "id", issue)).Want(http.StatusOK)
	var effectID string
	dbfx.QueryRow(t, `SELECT id FROM agent_effect WHERE task_id = $1`, task3).Scan(&effectID)
	testutil.Call(t, testHandler.UndoAgentEffect, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/agent-effects/"+effectID+"/undo", nil), "id", effectID)).Want(http.StatusOK).JSON(&out)
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issue).Scan(&status)
	if out.Reversed != 1 || status != "todo" {
		t.Fatalf("single undo = %+v, status %s; want 1 reversed and todo", out, status)
	}

	// Breaker: with a threshold of 1, the next undone run lowers the trust mode and alerts the managers.
	testutil.Call(t, testHandler.PutUndoSettings, newRequest(http.MethodPut, "/api/undo-settings", map[string]any{"window_hours": 0, "breaker_threshold": 1})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutUndoSettings, newRequest(http.MethodPut, "/api/undo-settings", map[string]any{"window_hours": 48, "breaker_threshold": 1})).Want(http.StatusOK)
	var settings struct {
		WindowHours      int `json:"window_hours"`
		BreakerThreshold int `json:"breaker_threshold"`
	}
	testutil.Call(t, testHandler.GetUndoSettings, newRequest(http.MethodGet, "/api/undo-settings", nil)).Want(http.StatusOK).JSON(&settings)
	if settings.WindowHours != 48 || settings.BreakerThreshold != 1 {
		t.Fatalf("settings = %+v", settings)
	}
	dbfx.Exec(t, `UPDATE agent_effect SET reversed_at = NULL WHERE agent_id = $1`, agent) // only the next undo counts
	task4 := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task4, http.MethodPut, "/api/issues/"+issue, map[string]any{"status": "in_progress"}), "id", issue)).Want(http.StatusOK)
	testutil.Call(t, testHandler.UndoTaskEffects, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/tasks/"+task4+"/undo", nil), "id", task4)).Want(http.StatusOK).JSON(&out)
	if !out.Breaker.Tripped || out.Breaker.TrustMode != "approval" {
		t.Fatalf("breaker = %+v, want tripped and lowered from autonomous to approval", out.Breaker)
	}
	var mode string
	dbfx.QueryRow(t, `SELECT trust_mode FROM agent WHERE id = $1`, agent).Scan(&mode)
	if mode != "approval" {
		t.Fatalf("agent trust mode = %s, want approval", mode)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND recipient_id = $2`, InboxTypeUndoBreaker, testUserID) != 1 {
		t.Fatal("the breaker files one inbox item for the manager")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM trust_mode_change WHERE agent_id = $1 AND to_mode = 'approval'`, agent) != 1 {
		t.Fatal("the trust change is recorded")
	}
}
