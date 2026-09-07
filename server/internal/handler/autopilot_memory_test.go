package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Autopilot execution memory (F24 / JEF-15). The write side is the whole
// point: only a run of THIS daemon may write, the revision is a real
// precondition, and an oversized document is cut rather than rejected.
//
// The truncation ARITHMETIC is canonical in TestTruncateAutopilotMemory below;
// the handler cases here prove the wiring around it.

type memoryFixture struct {
	AutopilotID string
	AgentID     string
	RunID       string
	TaskID      string
}

func seedAutopilotWithRun(t *testing.T, title string) memoryFixture {
	t.Helper()
	rt := dbfx.Runtime(t, "memory-rt-"+title)
	agentID := dbfx.Agent(t, "memory-agent-"+title, rt)
	apID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           title,
		"description":     "Keep notes.",
		"assignee_type":   "agent",
		"assignee_id":     agentID,
		"status":          "active",
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": apID,
		"source":       "manual",
		"status":       "running",
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"autopilot_run_id": runID, "runtime_id": rt})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM autopilot_memory WHERE autopilot_id = $1`, apID)
	})
	return memoryFixture{AutopilotID: apID, AgentID: agentID, RunID: runID, TaskID: taskID}
}

func getMemory(t *testing.T, apID string) *testutil.Response {
	t.Helper()
	req := withURLParam(newRequest("GET", "/api/autopilots/"+apID+"/memory", nil), "id", apID)
	return testutil.Call(t, testHandler.GetAutopilotMemory, req)
}

func putMemory(t *testing.T, fx memoryFixture, content string, headers ...string) *testutil.Response {
	t.Helper()
	req := newRequest("PUT", "/api/autopilots/"+fx.AutopilotID+"/memory", map[string]any{"content": content})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Task-ID", fx.TaskID)
	req.Header.Set("X-Agent-ID", fx.AgentID)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	req = withURLParam(req, "id", fx.AutopilotID)
	return testutil.Call(t, testHandler.UpdateAutopilotMemory, req)
}

type memoryBody struct {
	AutopilotID     string  `json:"autopilot_id"`
	Content         string  `json:"content"`
	Revision        int     `json:"revision"`
	UpdatedByTaskID *string `json:"updated_by_task_id"`
	Truncated       bool    `json:"truncated"`
}

// An empty memory is a state, not a missing resource: 200 with revision 0, so
// the UI does not have to tell "no daemon" apart from "nothing written yet".
func TestGetAutopilotMemoryEmptyIsNotAMiss(t *testing.T) {
	fx := seedAutopilotWithRun(t, "memory-empty")
	var got memoryBody
	getMemory(t, fx.AutopilotID).Want(http.StatusOK).JSON(&got)
	if got.Content != "" || got.Revision != 0 {
		t.Errorf("empty memory = %+v, want empty content at revision 0", got)
	}
}

// The write is authorized by the run, not by the caller's seat. A member has
// no way to put words in the daemon's mouth.
func TestUpdateAutopilotMemoryRejectsNonRunWriters(t *testing.T) {
	fx := seedAutopilotWithRun(t, "memory-auth")

	t.Run("member without a task token", func(t *testing.T) {
		req := withURLParam(
			newRequest("PUT", "/api/autopilots/"+fx.AutopilotID+"/memory", map[string]any{"content": "hi"}),
			"id", fx.AutopilotID)
		testutil.Call(t, testHandler.UpdateAutopilotMemory, req).Want(http.StatusForbidden)
	})

	t.Run("task token naming no task", func(t *testing.T) {
		req := newRequest("PUT", "/api/autopilots/"+fx.AutopilotID+"/memory", map[string]any{"content": "hi"})
		req.Header.Set("X-Actor-Source", "task_token")
		req = withURLParam(req, "id", fx.AutopilotID)
		testutil.Call(t, testHandler.UpdateAutopilotMemory, req).Want(http.StatusForbidden)
	})

	t.Run("a run of another daemon", func(t *testing.T) {
		other := seedAutopilotWithRun(t, "memory-auth-other")
		// other's run writing fx's memory: the scope is what keeps two daemons
		// sharing an agent from reading each other's notes, so it must also
		// keep them from writing each other's.
		req := newRequest("PUT", "/api/autopilots/"+fx.AutopilotID+"/memory", map[string]any{"content": "hi"})
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Task-ID", other.TaskID)
		req = withURLParam(req, "id", fx.AutopilotID)
		testutil.Call(t, testHandler.UpdateAutopilotMemory, req).Want(http.StatusForbidden)
	})

	t.Run("a task with no autopilot run", func(t *testing.T) {
		looseTask := dbfx.Task(t, fx.AgentID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t)})
		req := newRequest("PUT", "/api/autopilots/"+fx.AutopilotID+"/memory", map[string]any{"content": "hi"})
		req.Header.Set("X-Actor-Source", "task_token")
		req.Header.Set("X-Task-ID", looseTask)
		req = withURLParam(req, "id", fx.AutopilotID)
		testutil.Call(t, testHandler.UpdateAutopilotMemory, req).Want(http.StatusForbidden)
	})

	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot_memory WHERE autopilot_id = $1`, fx.AutopilotID); n != 0 {
		t.Errorf("a refused write left %d memory row(s)", n)
	}
}

// Acceptance 6: a stale revision is a 409 with a code the CLI and the UI can
// branch on, and the stored document is untouched.
func TestUpdateAutopilotMemoryRevisionPrecondition(t *testing.T) {
	fx := seedAutopilotWithRun(t, "memory-revision")

	var first memoryBody
	putMemory(t, fx, "first note").Want(http.StatusOK).JSON(&first)
	if first.Revision != 1 {
		t.Fatalf("first write landed at revision %d, want 1", first.Revision)
	}
	if first.UpdatedByTaskID == nil || *first.UpdatedByTaskID != fx.TaskID {
		t.Errorf("updated_by_task_id = %v, want the writing run %q", first.UpdatedByTaskID, fx.TaskID)
	}

	t.Run("matching revision succeeds", func(t *testing.T) {
		var got memoryBody
		putMemory(t, fx, "second note", "If-Match", "1").Want(http.StatusOK).JSON(&got)
		if got.Revision != 2 || got.Content != "second note" {
			t.Errorf("got %+v, want revision 2 with the new content", got)
		}
	})

	t.Run("stale revision is refused", func(t *testing.T) {
		body := putMemory(t, fx, "clobber", "If-Match", "1").Want(http.StatusConflict).Map()
		if body["code"] != "memory_revision_stale" {
			t.Errorf("code = %v, want memory_revision_stale", body["code"])
		}
		var still memoryBody
		getMemory(t, fx.AutopilotID).Want(http.StatusOK).JSON(&still)
		if still.Content != "second note" || still.Revision != 2 {
			t.Errorf("a refused write changed the document: %+v", still)
		}
	})

	t.Run("unparseable If-Match is a 400", func(t *testing.T) {
		putMemory(t, fx, "x", "If-Match", "not-a-number").Want(http.StatusBadRequest)
	})
}

// Acceptance 7: an oversized document is TRUNCATED, not rejected. The writer
// is an agent finishing a run; a 4xx would either lose the note entirely or
// send it into a retry loop it has no way out of.
func TestUpdateAutopilotMemoryTruncatesInsteadOfFailing(t *testing.T) {
	fx := seedAutopilotWithRun(t, "memory-truncate")

	line := strings.Repeat("x", 200) + "\n"
	oversized := strings.Repeat(line, 60) // ~12 KiB, well past the 8 KiB cap

	var got memoryBody
	putMemory(t, fx, oversized).Want(http.StatusOK).JSON(&got)

	if !got.Truncated {
		t.Error("truncation was not reported back, so the agent cannot tell its note was cut")
	}
	if len(got.Content) > autopilotMemoryMaxBytes {
		t.Errorf("stored %d bytes, cap is %d", len(got.Content), autopilotMemoryMaxBytes)
	}
	if strings.HasSuffix(got.Content, "x") == false {
		t.Errorf("content does not end on a full line: %q", got.Content[max(0, len(got.Content)-20):])
	}
	// A mid-line cut would leave the next run reading half a sentence as if it
	// were whole; every surviving line must be a whole one.
	for i, l := range strings.Split(got.Content, "\n") {
		if l != strings.Repeat("x", 200) {
			t.Fatalf("line %d is not a whole line (%d chars)", i, len(l))
		}
	}
}

// Acceptance 8: deleting the daemon deletes its memory. Runs and deliveries
// are execution history and survive on purpose; the memory is what the NEXT
// run would have been told, and there is no next run.
func TestDeleteAutopilotDeletesItsMemory(t *testing.T) {
	fx := seedAutopilotWithRun(t, "memory-delete")
	putMemory(t, fx, "some note").Want(http.StatusOK)

	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot_memory WHERE autopilot_id = $1`, fx.AutopilotID); n != 1 {
		t.Fatalf("setup failed: %d memory rows", n)
	}

	req := withURLParam(newRequest("DELETE", "/api/autopilots/"+fx.AutopilotID, nil), "id", fx.AutopilotID)
	testutil.Call(t, testHandler.DeleteAutopilot, req).Want(http.StatusNoContent)

	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot_memory WHERE autopilot_id = $1`, fx.AutopilotID); n != 0 {
		t.Errorf("memory rows = %d after delete, want 0", n)
	}
	// The run history is deliberately NOT deleted with it.
	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, fx.AutopilotID); n != 1 {
		t.Errorf("run history was destroyed by the delete: %d rows", n)
	}
}

// TestTruncateAutopilotMemory is the canonical arithmetic: line boundaries,
// the no-newline case, and UTF-8 safety.
func TestTruncateAutopilotMemory(t *testing.T) {
	t.Run("under the cap is untouched", func(t *testing.T) {
		in := "short\nnote\n"
		got, truncated := truncateAutopilotMemory(in)
		if got != in || truncated {
			t.Errorf("got (%q, %v), want the input unchanged", got, truncated)
		}
	})

	t.Run("cuts on the last full line", func(t *testing.T) {
		in := strings.Repeat("abcd\n", autopilotMemoryMaxBytes) // way past the cap
		got, truncated := truncateAutopilotMemory(in)
		if !truncated {
			t.Fatal("oversized input reported as untruncated")
		}
		if len(got) > autopilotMemoryMaxBytes {
			t.Errorf("kept %d bytes, cap is %d", len(got), autopilotMemoryMaxBytes)
		}
		if strings.HasSuffix(got, "\n") {
			t.Errorf("the trailing newline should be consumed by the cut: %q", got[len(got)-5:])
		}
		for _, l := range strings.Split(got, "\n") {
			if l != "abcd" {
				t.Fatalf("partial line survived: %q", l)
			}
		}
	})

	t.Run("a single oversized line still yields valid UTF-8", func(t *testing.T) {
		// No newline to cut on, and multi-byte runes straddling the cap: a
		// naive byte slice would leave a broken rune the next run renders as
		// a replacement character.
		in := strings.Repeat("é", autopilotMemoryMaxBytes)
		got, truncated := truncateAutopilotMemory(in)
		if !truncated {
			t.Fatal("oversized input reported as untruncated")
		}
		if len(got) > autopilotMemoryMaxBytes {
			t.Errorf("kept %d bytes, cap is %d", len(got), autopilotMemoryMaxBytes)
		}
		if strings.ContainsRune(got, '�') || !isValidUTF8(got) {
			t.Errorf("truncation broke a rune")
		}
	})
}

func isValidUTF8(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
