package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Racing attempts (F11 / JEF-6), the API half: starting a race queues N
// attempts, one race per issue at a time, settling keeps the winner and
// cancels every other open attempt through the ordinary user-cancel path,
// abandoning keeps nothing.

// startRunGroup posts a race and returns the decoded envelope.
func startRunGroup(t *testing.T, issueID string, body map[string]any, want int) RunGroupResponse {
	t.Helper()
	var out struct {
		Group RunGroupResponse `json:"group"`
	}
	res := testutil.Call(t, testHandler.StartRunGroup, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+issueID+"/run-groups", body), "id", issueID)).Want(want)
	if want == http.StatusCreated {
		res.JSON(&out)
	}
	return out.Group
}

// cleanupRunGroups removes what the handler created; only fixture rows are
// dropped automatically.
func cleanupRunGroups(t *testing.T, issueID string) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM run_group WHERE issue_id = $1`, issueID)
	})
}

// The whole loop: start, refuse a second, list with diffs, settle, refuse a
// second settle.
func TestRunGroupStartListAndSettle(t *testing.T) {
	runtime := handlerTestRuntimeID(t)
	agentA := dbfx.Agent(t, "race agent a", runtime)
	agentB := dbfx.Agent(t, "race agent b", runtime)
	issue := dbfx.Issue(t, "three ways to fix this")
	cleanupRunGroups(t, issue)

	// Two attempts of the SAME agent differing only by model, plus a third
	// agent: the shape the pending-slot index was relaxed for.
	group := startRunGroup(t, issue, map[string]any{
		"note": "try three ways",
		"attempts": []map[string]any{
			{"agent_id": agentA, "model": "sonnet"},
			{"agent_id": agentA, "model": "opus"},
			{"agent_id": agentB},
		},
	}, http.StatusCreated)
	if group.Status != "running" || group.AttemptCount != 3 || len(group.Attempts) != 3 {
		t.Fatalf("started group = %+v", group)
	}
	for _, a := range group.Attempts {
		if a.Status != "queued" {
			t.Fatalf("attempt %s status = %q, want queued", a.TaskID, a.Status)
		}
	}
	if group.Attempts[0].Model != "sonnet" || group.Attempts[1].Model != "opus" || group.Attempts[2].Model != "" {
		t.Fatalf("model overrides = %q %q %q", group.Attempts[0].Model, group.Attempts[1].Model, group.Attempts[2].Model)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE run_group_id = $1 AND status = 'queued'`, group.ID); n != 3 {
		t.Fatalf("queued attempts in db = %d", n)
	}

	// A second race while this one is running is refused.
	startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{{"agent_id": agentA}, {"agent_id": agentB}}}, http.StatusConflict)

	// A stat with no unified diff is the "too large to store" case; a stat with
	// one is an ordinary diff.
	winner := group.Attempts[0].TaskID
	loserRunning := group.Attempts[1].TaskID
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now(), diff_stat = '{"files":2}'::jsonb, diff_unified = 'patch' WHERE id = $1`, winner)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', dispatched_at = now(), started_at = now(), diff_stat = '{"files":9}'::jsonb WHERE id = $1`, loserRunning)

	var listed struct {
		Groups []RunGroupResponse `json:"groups"`
	}
	testutil.Call(t, testHandler.ListIssueRunGroups, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/run-groups", nil), "id", issue)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Groups) != 1 || len(listed.Groups[0].Attempts) != 3 {
		t.Fatalf("listed = %+v", listed.Groups)
	}
	byTask := map[string]RunGroupAttemptResponse{}
	for _, a := range listed.Groups[0].Attempts {
		byTask[a.TaskID] = a
	}
	if got := byTask[winner]; got.DiffUnified == nil || *got.DiffUnified != "patch" || got.DiffTruncated {
		t.Fatalf("winner attempt = %+v, want the stored patch and no truncation flag", got)
	}
	if got := byTask[loserRunning]; got.DiffUnified != nil || !got.DiffTruncated || len(got.DiffStat) == 0 {
		t.Fatalf("truncated attempt = %+v, want diff_truncated with a stat", got)
	}

	// Settle: the winner is untouched, every other open attempt is cancelled.
	var settled struct {
		Group RunGroupResponse `json:"group"`
	}
	testutil.Call(t, testHandler.SettleRunGroup, testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+group.ID+"/settle", map[string]any{"winner_task_id": winner}), "id", group.ID)).Want(http.StatusOK).JSON(&settled)
	if settled.Group.Status != "settled" || settled.Group.WinnerTaskID == nil || *settled.Group.WinnerTaskID != winner {
		t.Fatalf("settled group = %+v", settled.Group)
	}
	if mustTask(t, winner).Status != "completed" {
		t.Fatalf("the winner must be left alone, got %q", mustTask(t, winner).Status)
	}
	for _, loser := range []string{loserRunning, group.Attempts[2].TaskID} {
		if got := mustTask(t, loser).Status; got != "cancelled" {
			t.Fatalf("losing attempt %s status = %q, want cancelled", loser, got)
		}
	}

	// The CAS makes a double-click a 409, not a second round of cancellations.
	if res := testutil.Call(t, testHandler.SettleRunGroup, testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+group.ID+"/settle", map[string]any{"winner_task_id": winner}), "id", group.ID)).Want(http.StatusConflict); res.Map()["code"] != ErrCodeRunGroupSettled {
		t.Fatalf("second settle = %v", res.Map())
	}

	// A settled race releases the issue's slot.
	startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{{"agent_id": agentA}, {"agent_id": agentB}}}, http.StatusCreated)
}

// The cap, its floor, and an agent that is not in this workspace.
func TestRunGroupAttemptBounds(t *testing.T) {
	runtime := handlerTestRuntimeID(t)
	agent := dbfx.Agent(t, "race bounds agent", runtime)
	issue := dbfx.Issue(t, "bounded race")
	cleanupRunGroups(t, issue)

	startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{}}, http.StatusBadRequest)
	startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{{"agent_id": agent}}}, http.StatusBadRequest)

	tooMany := make([]map[string]any, 0, maxRunGroupAttempts+1)
	for i := 0; i <= maxRunGroupAttempts; i++ {
		tooMany = append(tooMany, map[string]any{"agent_id": agent})
	}
	startRunGroup(t, issue, map[string]any{"attempts": tooMany}, http.StatusBadRequest)

	startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{
		{"agent_id": agent}, {"agent_id": uuid.NewString()},
	}}, http.StatusUnprocessableEntity)

	// Nothing was created by any of the refusals.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM run_group WHERE issue_id = $1`, issue); n != 0 {
		t.Fatalf("refused races left %d group rows", n)
	}
}

// Abandoning keeps nothing: every open attempt is cancelled and the group ends
// with no winner.
func TestRunGroupAbandonCancelsEveryOpenAttempt(t *testing.T) {
	runtime := handlerTestRuntimeID(t)
	agentA := dbfx.Agent(t, "abandon agent a", runtime)
	agentB := dbfx.Agent(t, "abandon agent b", runtime)
	issue := dbfx.Issue(t, "abandoned race")
	cleanupRunGroups(t, issue)

	group := startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{{"agent_id": agentA}, {"agent_id": agentB}}}, http.StatusCreated)
	// One attempt already finished for good: abandoning must not rewrite it.
	done := group.Attempts[0].TaskID
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', completed_at = now(), failure_reason = 'agent_error' WHERE id = $1`, done)

	var out struct {
		Group RunGroupResponse `json:"group"`
	}
	testutil.Call(t, testHandler.AbandonRunGroup, testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+group.ID+"/abandon", nil), "id", group.ID)).Want(http.StatusOK).JSON(&out)
	if out.Group.Status != "abandoned" || out.Group.WinnerTaskID != nil {
		t.Fatalf("abandoned group = %+v", out.Group)
	}
	if got := mustTask(t, done).Status; got != "failed" {
		t.Fatalf("terminal attempt = %q, want it left as failed", got)
	}
	if got := mustTask(t, group.Attempts[1].TaskID).Status; got != "cancelled" {
		t.Fatalf("open attempt = %q, want cancelled", got)
	}
	testutil.Call(t, testHandler.AbandonRunGroup, testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+group.ID+"/abandon", nil), "id", group.ID)).Want(http.StatusConflict)
}

// The tenant guard, and a winner that is not an attempt of this race.
func TestRunGroupGuards(t *testing.T) {
	runtime := handlerTestRuntimeID(t)
	agentA := dbfx.Agent(t, "guard agent a", runtime)
	agentB := dbfx.Agent(t, "guard agent b", runtime)
	issue := dbfx.Issue(t, "guarded race")
	other := dbfx.Issue(t, "not in the race")
	cleanupRunGroups(t, issue)
	cleanupRunGroups(t, other)

	group := startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{{"agent_id": agentA}, {"agent_id": agentB}}}, http.StatusCreated)
	strayTask := dbfx.Task(t, agentA, testutil.Cols{"issue_id": other, "runtime_id": runtime})

	testutil.Call(t, testHandler.SettleRunGroup, testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+group.ID+"/settle", map[string]any{"winner_task_id": strayTask}), "id", group.ID)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.SettleRunGroup, testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+group.ID+"/settle", map[string]any{"winner_task_id": "not-a-uuid"}), "id", group.ID)).Want(http.StatusBadRequest)

	// Same group id, a workspace the caller is a member of but which does not
	// own the group: not found, not someone else's race to settle.
	foreign := dbfx.Workspace(t, "Race foreign", "race-foreign-"+uuid.NewString())
	dbfx.Member(t, foreign, testUserID, "owner")
	req := testutil.WithURLParams(newRequest(http.MethodPost, "/api/run-groups/"+group.ID+"/settle", map[string]any{"winner_task_id": group.Attempts[0].TaskID}), "id", group.ID)
	req.Header.Set("X-Workspace-ID", foreign)
	testutil.Call(t, testHandler.SettleRunGroup, req).Want(http.StatusNotFound)

	// The race is still running after every refusal.
	if got := mustTask(t, group.Attempts[0].TaskID).Status; got != "queued" {
		t.Fatalf("attempt after refused settles = %q", got)
	}
}
