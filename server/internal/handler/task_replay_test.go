package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Run replay (K70): the run's sources merge into one ordered, hash-chained
// stream; pages follow a cursor; the chain head sealed at completion is
// verified on read and a tampered message breaks it; a resume point starts a
// new run whose handoff carries the trace and the new instruction.

type replayOut struct {
	Run struct {
		AgentName string `json:"agent_name"`
		Links     []struct {
			Relation string `json:"relation"`
			TaskID   string `json:"task_id"`
		} `json:"links"`
	} `json:"run"`
	Events []struct {
		Seq      int            `json:"seq"`
		Kind     string         `json:"kind"`
		Title    string         `json:"title"`
		Text     string         `json:"text"`
		Data     map[string]any `json:"data"`
		PrevHash string         `json:"prev_hash"`
		Hash     string         `json:"hash"`
	} `json:"events"`
	Total      int    `json:"total"`
	NextCursor *int   `json:"next_cursor"`
	HeadHash   string `json:"head_hash"`
	Cost       struct {
		InputTokens int64 `json:"input_tokens"`
	} `json:"cost"`
	Sealed *struct {
		Events   int    `json:"events"`
		HeadHash string `json:"head_hash"`
		Verified bool   `json:"verified"`
	} `json:"sealed"`
}

func getReplay(t *testing.T, taskID, query string) replayOut {
	t.Helper()
	var out replayOut
	testutil.Call(t, testHandler.GetTaskReplay, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/tasks/"+taskID+"/replay"+query, nil), "taskId", taskID)).Want(http.StatusOK).JSON(&out)
	return out
}

func TestRunReplay(t *testing.T) {
	ctx := context.Background()
	agent := dbfx.Agent(t, "replay agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	issue := dbfx.Issue(t, "replay issue "+uuid.NewString()[:8], testutil.Cols{"status": "todo", "assignee_type": "agent", "assignee_id": agent})
	prior := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed"})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running", "retry_of_task_id": prior})
	t0 := time.Now().Add(-10 * time.Minute)
	at := func(m int) time.Time { return t0.Add(time.Duration(m) * time.Minute) }
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_effect WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue_decision WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM handoff_packet WHERE issue_id = $1`, issue)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1 AND id NOT IN ($2, $3)`, issue, prior, task)
		testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE entity_id = $1`, task)
	})
	// The transcript, with both dialects and a steer.
	for i, m := range []struct {
		typ, tool, content string
	}{{"text", "", "Starting"}, {"tool-use", "read_file", ""}, {"tool_result", "read_file", ""}, {"steering_instruction", "", "focus on tests"}, {"text", "", "Done"}} {
		dbfx.Insert(t, "task_message", testutil.Cols{"id": uuid.NewString(), "task_id": task, "seq": i + 1, "type": m.typ, "tool": m.tool, "content": m.content, "input": `{"path":"a.go"}`, "created_at": at(i)})
	}
	// An effect through the API, a decision, a handoff packet, usage.
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		runRequest(agent, task, http.MethodPut, "/api/issues/"+issue, map[string]any{"status": "in_progress"}), "id", issue)).Want(http.StatusOK)
	dbfx.Insert(t, "issue_decision", testutil.Cols{"id": uuid.NewString(), "workspace_id": testWorkspaceID, "issue_id": issue, "task_id": task, "asked_by_type": "agent", "asked_by_id": agent,
		"question": "Ship it?", "options": `[{"id":"yes","label":"Yes"}]`, "urgency": "normal", "created_at": at(6)})
	dbfx.Insert(t, "handoff_packet", testutil.Cols{"id": uuid.NewString(), "run_id": task, "workspace_id": testWorkspaceID, "issue_id": issue, "objective": "Fix the build", "decisions": "[]", "evidence": "[]", "failed_attempts": "[]",
		"next_action": "Merge", "created_by_type": "agent", "created_by_id": agent, "created_at": at(7)})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": task, "provider": "anthropic", "model": "claude", "input_tokens": 1200, "output_tokens": 300, "updated_at": at(8)})

	out := getReplay(t, task, "")
	if out.Total < 9 || out.Run.AgentName == "" {
		t.Fatalf("replay must merge every source: total %d", out.Total)
	}
	kinds := map[string]int{}
	for i, e := range out.Events {
		kinds[e.Kind]++
		if e.Seq != i || e.Hash == "" || (i > 0 && e.PrevHash != out.Events[i-1].Hash) || (i == 0 && e.PrevHash != "") {
			t.Fatalf("event %d breaks the chain: %+v", i, e)
		}
	}
	for _, k := range []string{"text", "tool_use", "tool_result", "steer", "effect", "decision_asked", "handoff", "cost"} {
		if kinds[k] == 0 {
			t.Fatalf("missing kind %s in %v", k, kinds)
		}
	}
	if out.Events[0].Kind != "text" || out.Events[0].Text != "Starting" || out.Events[1].Title != "Tool call: read_file" || out.Events[2].Kind != "tool_result" {
		t.Fatalf("transcript order and dialect normalization: %+v", out.Events[:3])
	}
	if out.HeadHash != out.Events[len(out.Events)-1].Hash || out.Cost.InputTokens != 1200 || len(out.Run.Links) != 1 || out.Run.Links[0].Relation != "retry_of" {
		t.Fatalf("head hash, cost and links: head=%s cost=%d links=%+v", out.HeadHash[:8], out.Cost.InputTokens, out.Run.Links)
	}
	if out.Sealed != nil {
		t.Fatal("a running run is not sealed yet")
	}

	// Cursor pagination walks the same chain.
	page := getReplay(t, task, "?limit=3")
	if len(page.Events) != 3 || page.NextCursor == nil || *page.NextCursor != 3 || page.Total != out.Total {
		t.Fatalf("first page = %d events, next %v", len(page.Events), page.NextCursor)
	}
	page2 := getReplay(t, task, "?limit=3&cursor=3")
	if page2.Events[0].Seq != 3 || page2.Events[0].Hash != out.Events[3].Hash {
		t.Fatal("the second page continues the chain")
	}

	// Sealing at completion, then verification, then tampering.
	testHandler.sealRunReplay(ctx, mustTask(t, task))
	sealed := getReplay(t, task, "")
	if sealed.Sealed == nil || !sealed.Sealed.Verified || sealed.Sealed.Events != out.Total || sealed.Sealed.HeadHash != out.HeadHash {
		t.Fatalf("seal = %+v", sealed.Sealed)
	}
	// Events after the seal (the effect is undone) append without breaking it.
	testutil.Call(t, testHandler.UndoTaskEffects, testutil.WithURLParams(newRequest(http.MethodPost, "/api/tasks/"+task+"/undo", nil), "id", task)).Want(http.StatusOK)
	after := getReplay(t, task, "")
	if after.Total != out.Total+1 || after.Sealed == nil || !after.Sealed.Verified || after.Events[after.Total-1].Kind != "effect_reversed" {
		t.Fatalf("an undo appends after the seal: total %d, sealed %+v", after.Total, after.Sealed)
	}
	dbfx.Exec(t, `UPDATE task_message SET content = 'Starting (edited)' WHERE task_id = $1 AND seq = 1`, task)
	if tampered := getReplay(t, task, ""); tampered.Sealed == nil || tampered.Sealed.Verified {
		t.Fatal("editing a message after the seal must break verification")
	}

	// Resume from a point: a new run of the issue carries the trace and the instruction.
	var resumed struct {
		TaskID  string `json:"task_id"`
		FromSeq int    `json:"from_seq"`
	}
	testutil.Call(t, testHandler.ResumeTaskReplay, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/tasks/"+task+"/replay/resume", map[string]any{"seq": 2, "instruction": "Try the other file"}), "taskId", task)).Want(http.StatusCreated).JSON(&resumed)
	if resumed.TaskID == "" || resumed.FromSeq != 2 || dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE id = $1 AND issue_id = $2`, resumed.TaskID, issue) != 1 {
		t.Fatalf("resume must enqueue a run of the issue: %+v", resumed)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE entity_id = $1 AND action = 'run.resumed_from_replay'`, task) != 1 {
		t.Fatal("the resume is audited on the source run")
	}
	testutil.Call(t, testHandler.ResumeTaskReplay, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/tasks/"+task+"/replay/resume", map[string]any{"seq": 999, "instruction": "x"}), "taskId", task)).Want(http.StatusBadRequest)
}
