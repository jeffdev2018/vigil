package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Scorecards (K25): a rollup that recomputes days idempotently, and the
// rates read from it.

func scorecardTask(t *testing.T, agentID, issueID, status string, endedAgo time.Duration, cost int64) string {
	t.Helper()
	ended := time.Now().Add(-endedAgo)
	task := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t), "issue_id": issueID, "status": status,
		"created_at": ended.Add(-time.Hour), "started_at": ended.Add(-time.Hour), "completed_at": ended,
	})
	if cost > 0 {
		dbfx.Exec(t, `INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cost_usd_ticks, created_at) VALUES ($1, 'openai', 'm', 1, 1, $2, now())`, task, cost)
		t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM task_usage WHERE task_id = $1`, task) })
	}
	return task
}

func TestAgentScorecardRollupAndRates(t *testing.T) {
	agent := dbfx.Agent(t, "scorecard agent", handlerTestRuntimeID(t))
	t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM agent_scorecard_daily WHERE agent_id = $1`, agent) })

	accepted := dbfx.Issue(t, "sc accepted", testutil.Cols{"status": "done"})
	scorecardTask(t, agent, accepted, "completed", 2*time.Hour, 2_000_000_000)
	// Reopened: was done, left done, back to done.
	reopened := dbfx.Issue(t, "sc reopened", testutil.Cols{"status": "done"})
	dbfx.Exec(t, `UPDATE issue SET status = 'in_progress' WHERE id = $1`, reopened)
	dbfx.Exec(t, `UPDATE issue SET status = 'done' WHERE id = $1`, reopened)
	var reopenCount int32
	dbfx.QueryRow(t, `SELECT reopen_count FROM issue WHERE id = $1`, reopened).Scan(&reopenCount)
	if reopenCount != 1 {
		t.Fatalf("reopen_count = %d, want 1", reopenCount)
	}
	scorecardTask(t, agent, reopened, "completed", 3*time.Hour, 1_000_000_000)
	// Human stepped in during the run: a member comment inside the window.
	helped := dbfx.Issue(t, "sc helped", testutil.Cols{"status": "done"})
	scorecardTask(t, agent, helped, "completed", 4*time.Hour, 0)
	dbfx.Comment(t, helped, "nudge", testutil.Cols{"author_type": "member", "author_id": testUserID, "created_at": time.Now().Add(-4*time.Hour - 30*time.Minute)})
	// Not done yet: completed run, issue still open → not accepted.
	open := dbfx.Issue(t, "sc open", testutil.Cols{"status": "in_review"})
	scorecardTask(t, agent, open, "completed", 5*time.Hour, 0)
	failed := dbfx.Issue(t, "sc failed")
	scorecardTask(t, agent, failed, "failed", 6*time.Hour, 500_000_000)
	scorecardTask(t, agent, failed, "cancelled", 7*time.Hour, 0)
	// Older than the rollup window: never counted.
	scorecardTask(t, agent, failed, "failed", 10*24*time.Hour, 0)

	if _, err := testHandler.RollupAgentScorecards(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	// Idempotent: a second rollup writes the same numbers.
	if _, err := testHandler.RollupAgentScorecards(t.Context(), time.Now()); err != nil {
		t.Fatal(err)
	}
	var out AgentScorecardResponse
	req := testutil.WithURLParams(testutil.WithHeaders(newRequest(http.MethodGet, "/api/agents/"+agent+"/scorecard?days=7", nil), "X-Workspace-ID", testWorkspaceID), "id", agent)
	testutil.Call(t, testHandler.GetAgentScorecard, req).Want(http.StatusOK).JSON(&out)
	tot := out.Totals
	if tot.RunsTotal != 6 || tot.RunsFailed != 1 || tot.RunsCancelled != 1 || tot.RunsAccepted != 3 || tot.RunsReopened != 1 || tot.RunsNoIntervention != 3 || tot.CostUsdTicksTotal != 3_500_000_000 || !tot.LowSample {
		t.Fatalf("totals = %+v", tot)
	}
	if len(out.Series) == 0 || out.Days != 7 {
		t.Fatalf("series = %+v days = %d", out.Series, out.Days)
	}

	var ws struct {
		Rows []WorkspaceScorecardRow `json:"rows"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListWorkspaceScorecards), testutil.WithHeaders(newRequest(http.MethodGet, "/api/scorecards?days=7", nil), "X-Workspace-ID", testWorkspaceID)).Want(http.StatusOK).JSON(&ws)
	var mine *WorkspaceScorecardRow
	for i := range ws.Rows {
		if ws.Rows[i].AgentID == agent {
			mine = &ws.Rows[i]
		}
	}
	if mine == nil || mine.RunsTotal != 6 || mine.RuntimeID != handlerTestRuntimeID(t) {
		t.Fatalf("workspace row = %+v", mine)
	}

	// Purge with the workspace.
	other := dbfx.Workspace(t, "Scorecard purge", "scorecard-purge-"+uuid.NewString())
	dbfx.Insert(t, "agent_scorecard_daily", testutil.Cols{"workspace_id": other, "agent_id": agent, "day": "2026-09-01"})
	if err := testHandler.Queries.DeleteWorkspaceIssueRoots(t.Context(), parseUUID(other)); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_scorecard_daily WHERE workspace_id = $1`, other); n != 0 {
		t.Fatalf("rows after purge = %d", n)
	}
}
