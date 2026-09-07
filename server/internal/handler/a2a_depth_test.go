package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// a2a_depth — the SHORT-loop breaker (F19 / JEF-32).
//
// Depth is STORED at enqueue time, not derived. The attribution chain is copied
// rather than linked (migration 184), so once the run exists there is no edge
// left to count. These tests therefore assert the COLUMN of the run that was
// actually enqueued, never a recomputation.

// latestTaskDepth returns the newest run for (issue, agent) and its depth.
func latestTaskDepth(t *testing.T, issueID, agentID string) (string, int32) {
	t.Helper()
	var id string
	var depth int32
	// Always one row, so "this agent has no run yet" is an empty id rather than
	// a fatal scan the caller cannot ask about.
	dbfx.QueryRow(t, `
		SELECT COALESCE(t.id::text, ''), COALESCE(t.a2a_depth, 0)
		FROM (SELECT 1) probe
		LEFT JOIN LATERAL (
			SELECT id, a2a_depth FROM agent_task_queue
			WHERE issue_id = $1 AND agent_id = $2
			ORDER BY created_at DESC, id DESC LIMIT 1
		) t ON true`, issueID, agentID).Scan(&id, &depth)
	return id, depth
}

// ACCEPTANCE 6: a run a HUMAN triggered is always at depth 0. This is what makes
// `a2a_depth > 0` mean "descends from an agent-to-agent message" — the predicate
// the per-issue budget and its partial index both key on.
func TestA2ADepth_HumanTriggeredRunIsZero(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)
	issueID := dbfx.Issue(t, "a2a human depth")
	agentID := dbfx.Agent(t, "a2a-human-target", runtimeID)

	r := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "[@a](mention://agent/" + agentID + ")\n\nplease start",
	})
	r = withURLParam(r, "id", issueID)
	var resp CommentResponse
	testutil.Call(t, testHandler.CreateComment, r).Want(http.StatusCreated).JSON(&resp)

	taskID, depth := latestTaskDepth(t, issueID, agentID)
	if taskID == "" {
		t.Fatalf("member mention enqueued no run (trigger_outcomes=%v)", resp.TriggerOutcomes)
	}
	if depth != 0 {
		t.Fatalf("human-triggered run a2a_depth = %d, want 0", depth)
	}
}

// ACCEPTANCE 4 and 7 together: two agents handing work back and forth climb one
// step per hop and are cut off at the hop that would exceed maxA2ADepth. With
// the default of 4 the fourth hop passes and the fifth is refused, so the
// exchange costs at most maxA2ADepth runs however long the agents keep at it.
//
// The previous run of the RECIPIENT is completed before each hop on purpose:
// pending-task dedup would otherwise coalesce the mention into the run that is
// already queued, and this test is about depth, not about dedup.
func TestA2ADepth_PingPongStopsAtMaxDepth(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	if max := service.MaxA2ADepth(); max != 4 {
		t.Skipf("MULTICA_A2A_MAX_DEPTH is overridden to %d; this test pins the shipped default of 4", max)
	}
	runtimeID := handlerTestRuntimeID(t)
	issueID := dbfx.Issue(t, "a2a ping pong")
	agentA := dbfx.Agent(t, "a2a-ping-a", runtimeID)
	agentB := dbfx.Agent(t, "a2a-ping-b", runtimeID)

	complete := func(taskID string) {
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, taskID)
	}
	// Hop 0 is the human's: A is running at depth 0.
	taskA := dbfx.Task(t, agentA, testutil.Cols{
		"issue_id": issueID, "runtime_id": runtimeID, "originator_user_id": testUserID,
		"accountable_user_id": testUserID, "a2a_depth": 0,
	})

	// Four accepted hops: A→B→A→B→A... each exactly one deeper than the last.
	from, fromTask, to := agentA, taskA, agentB
	for hop := int32(1); hop <= 4; hop++ {
		if prev, _ := latestTaskDepth(t, issueID, to); prev != "" {
			complete(prev)
		}
		sendA2A(t, issueID, from, fromTask, map[string]any{
			"to_agent_id": to, "intent": "question", "body": "your turn"}).
			Want(http.StatusCreated)

		nextTask, depth := latestTaskDepth(t, issueID, to)
		if nextTask == "" {
			t.Fatalf("hop %d: no run enqueued for the recipient", hop)
		}
		if depth != hop {
			t.Fatalf("hop %d: a2a_depth = %d, want %d", hop, depth, hop)
		}
		from, fromTask, to = to, nextTask, from
	}

	// The fifth hop would land at depth 5. It is refused, and NOTHING is
	// written: no comment, so no run.
	var beforeComments, beforeTasks int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&beforeComments)
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&beforeTasks)

	if prev, _ := latestTaskDepth(t, issueID, to); prev != "" {
		complete(prev)
	}
	resp := sendA2A(t, issueID, from, fromTask, map[string]any{
		"to_agent_id": to, "intent": "question", "body": "one hop too far"}).
		Want(http.StatusTooManyRequests)
	if got := readReasonCode(t, resp.Body.Bytes()); got != "a2a_depth_exceeded" {
		t.Errorf("reason_code = %q, want a2a_depth_exceeded", got)
	}

	var afterComments, afterTasks int
	dbfx.QueryRow(t, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&afterComments)
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&afterTasks)
	if afterComments != beforeComments {
		t.Errorf("refused hop wrote %d comment(s)", afterComments-beforeComments)
	}
	if afterTasks != beforeTasks {
		t.Errorf("refused hop enqueued %d run(s)", afterTasks-beforeTasks)
	}
}

// An A2A message whose sending run cannot be resolved still counts as hop 1. The
// depth lookup LEFT JOINs the parent precisely so an unresolvable sender fails
// TOWARD the breaker: an A2A message is by construction at least one hop from
// the human, and reading it as 0 would hand a loop a free depth reset.
func TestA2ADepth_UnresolvableSenderCountsAsFirstHop(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := handlerTestRuntimeID(t)
	issueID := dbfx.Issue(t, "a2a orphan sender")
	agentID := dbfx.Agent(t, "a2a-orphan-target", runtimeID)
	authorID := dbfx.Agent(t, "a2a-orphan-author", runtimeID)
	// An intent-carrying comment with NO source_task_id — the shape a deleted
	// sending run leaves behind.
	commentID := dbfx.Comment(t, issueID,
		"[@a](mention://agent/"+agentID+")\n\norphaned request",
		testutil.Cols{"author_type": "agent", "author_id": authorID, "a2a_intent": "review"})

	depth := testHandler.TaskService.A2ADepthForTriggerComment(t.Context(),
		parseUUID(testWorkspaceID), parseUUID(commentID))
	if !depth.Valid || depth.Int32 != 1 {
		t.Fatalf("orphan-sender depth = %+v, want 1", depth)
	}
}
