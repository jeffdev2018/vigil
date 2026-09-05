package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// K69 follow-ups: a run's deletions are journaled and restored under the
// old id, a chat reply is journaled and retracted with a corrective message,
// a held deletion waits for approval, and a discarded preview run counts
// toward the breaker like an undone one.

func TestUndoDeletionsAndChatReply(t *testing.T) {
	ctx := context.Background()
	rememberSettings(t)
	agent := dbfx.Agent(t, "gaps agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	issue := dbfx.Issue(t, "gaps issue "+uuid.NewString()[:8], testutil.Cols{"status": "todo"})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	session := dbfx.Insert(t, "chat_session", testutil.Cols{"id": uuid.NewString(), "workspace_id": testWorkspaceID, "agent_id": agent, "creator_id": testUserID, "title": "gaps"})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_effect WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM chat_message WHERE chat_session_id = $1`, session)
		testPool.Exec(ctx, `DELETE FROM workspace_note WHERE workspace_id = $1 AND source_agent_id = $2`, testWorkspaceID, agent)
		testPool.Exec(ctx, `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type IN ('agent_undo_breaker', 'decision_request')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM trust_mode_change WHERE agent_id = $1`, agent)
	})

	// The run creates then deletes its own comment and note.
	var created struct {
		ID string `json:"id"`
	}
	testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(
		runRequest(agent, task, http.MethodPost, "/api/issues/"+issue+"/comments", map[string]any{"content": "gaps comment", "type": "comment"}), "id", issue)).Want(http.StatusCreated).JSON(&created)
	commentID := created.ID
	testutil.Call(t, testHandler.DeleteComment, testutil.WithURLParams(
		runRequest(agent, task, http.MethodDelete, "/api/issues/"+issue+"/comments/"+commentID, nil), "id", issue, "commentId", commentID)).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.CreateWorkspaceNote,
		runRequest(agent, task, http.MethodPost, "/api/workspace-notes", map[string]any{"title": "gaps note", "content": "body", "tags": []string{"k69"}}),
	).Want(http.StatusCreated).JSON(&created)
	noteID := created.ID
	testutil.Call(t, testHandler.DeleteWorkspaceNote, testutil.WithURLParams(
		runRequest(agent, task, http.MethodDelete, "/api/workspace-notes/"+noteID, nil), "id", noteID)).Want(http.StatusNoContent)
	if dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE id = $1`, commentID) != 0 || dbfx.Count(t, `SELECT COUNT(*) FROM workspace_note WHERE id = $1`, noteID) != 0 {
		t.Fatal("deletions must land in apply mode")
	}
	// ...and replies in a chat session (journaled by the service on completion; here the row is the journal's).
	chatMsg := dbfx.Insert(t, "chat_message", testutil.Cols{"id": uuid.NewString(), "chat_session_id": session, "role": "assistant", "content": "wrong answer", "task_id": task})
	dbfx.Insert(t, "agent_effect", testutil.Cols{
		"id": uuid.NewString(), "workspace_id": testWorkspaceID, "task_id": task, "agent_id": agent, "kind": "chat_message", "target_type": "chat_message", "target_id": chatMsg,
		"before": "{}", "after": `{"chat_session_id":"` + session + `","excerpt":"wrong answer"}`, "reversible": true, "status": "applied", "payload": "{}",
	})
	for _, kind := range []string{"comment_create", "comment_delete", "note_create", "note_delete", "chat_message"} {
		if dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND kind = $2 AND status = 'applied' AND reversible`, task, kind) != 1 {
			t.Fatalf("journal must hold one applied, reversible %s", kind)
		}
	}

	// Undo the run: the deletions come back under their old ids, the creations
	// go away again, and the chat reply gets a corrective message in the session.
	var out undoOut
	testutil.Call(t, testHandler.UndoTaskEffects, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/tasks/"+task+"/undo", nil), "id", task)).Want(http.StatusOK).JSON(&out)
	if out.Reversed != 5 {
		t.Fatalf("undo = %+v, want 5 reversed (newest first: chat, note delete, note create, comment delete, comment create)", out)
	}
	// Newest-first: the delete is undone (row restored), then the create is undone (row deleted again).
	if dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE id = $1`, commentID) != 0 || dbfx.Count(t, `SELECT COUNT(*) FROM workspace_note WHERE id = $1`, noteID) != 0 {
		t.Fatal("undoing create-then-delete ends without the rows")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM chat_message WHERE chat_session_id = $1 AND role = 'assistant' AND content LIKE '%Retracted:%'`, session) != 1 {
		t.Fatal("the chat reply's undo posts one corrective message in the session")
	}

	// A single undo of a deletion alone restores the row with its old id and content.
	task2 := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	commentID = dbfx.Insert(t, "comment", testutil.Cols{"id": uuid.NewString(), "issue_id": issue, "workspace_id": testWorkspaceID, "author_type": "agent", "author_id": agent, "content": "keep me", "type": "comment"})
	testutil.Call(t, testHandler.DeleteComment, testutil.WithURLParams(
		runRequest(agent, task2, http.MethodDelete, "/api/issues/"+issue+"/comments/"+commentID, nil), "id", issue, "commentId", commentID)).Want(http.StatusNoContent)
	var effectID string
	dbfx.QueryRow(t, `SELECT id FROM agent_effect WHERE task_id = $1 AND kind = 'comment_delete'`, task2).Scan(&effectID)
	testutil.Call(t, testHandler.UndoAgentEffect, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/agent-effects/"+effectID+"/undo", nil), "id", effectID)).Want(http.StatusOK).JSON(&out)
	var content string
	dbfx.QueryRow(t, `SELECT content FROM comment WHERE id = $1 AND author_type = 'agent' AND author_id = $2`, commentID, agent).Scan(&content)
	if out.Reversed != 1 || content != "keep me" {
		t.Fatalf("restored comment = %q (reversed %d), want the original under the old id", content, out.Reversed)
	}

	// Preview mode holds a deletion too; approval deletes it and journals the deletion.
	testutil.Call(t, testHandler.SetAgentEffectMode, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/agents/"+agent+"/effect-mode", map[string]any{"mode": "preview"}), "id", agent)).Want(http.StatusOK)
	task3 := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	testutil.Call(t, testHandler.DeleteComment, testutil.WithURLParams(
		runRequest(agent, task3, http.MethodDelete, "/api/issues/"+issue+"/comments/"+commentID, nil), "id", issue, "commentId", commentID)).Want(http.StatusAccepted)
	if dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE id = $1`, commentID) != 1 {
		t.Fatal("a held deletion must not land")
	}
	testHandler.settlePendingEffects(ctx, mustTask(t, task3), true)
	var decisionID string
	dbfx.QueryRow(t, `SELECT id FROM issue_decision WHERE task_id = $1`, task3).Scan(&decisionID)
	respondDecision(t, issue, decisionID, map[string]any{"option_id": "apply_effects"}).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM comment WHERE id = $1`, commentID) != 0 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM agent_effect WHERE task_id = $1 AND kind = 'comment_delete' AND status = 'applied'`, task3) != 1 {
		t.Fatal("approval deletes the comment and journals a reversible deletion")
	}

	// A discarded preview run counts toward the breaker like an undone one.
	testutil.Call(t, testHandler.PutUndoSettings, newRequest(http.MethodPut, "/api/undo-settings", map[string]any{"window_hours": 24, "breaker_threshold": 1})).Want(http.StatusOK)
	task4 := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task4, http.MethodPut, "/api/issues/"+issue, map[string]any{"priority": "urgent"}), "id", issue)).Want(http.StatusAccepted)
	testHandler.settlePendingEffects(ctx, mustTask(t, task4), true)
	dbfx.QueryRow(t, `SELECT id FROM issue_decision WHERE task_id = $1`, task4).Scan(&decisionID)
	// Earlier undone runs of this agent already exceed the threshold of 1; reset the day so this discard is the one that trips it.
	dbfx.Exec(t, `UPDATE agent_effect SET reversed_at = reversed_at - interval '2 days' WHERE agent_id = $1 AND reversed_at IS NOT NULL`, agent)
	respondDecision(t, issue, decisionID, map[string]any{"option_id": "discard_effects"}).Want(http.StatusOK)
	var trust string
	dbfx.QueryRow(t, `SELECT trust_mode FROM agent WHERE id = $1`, agent).Scan(&trust)
	if trust != "approval" {
		t.Fatalf("trust after a discarded run = %s, want approval (breaker lowered autonomous one notch)", trust)
	}
}
