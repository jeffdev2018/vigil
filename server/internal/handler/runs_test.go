package handler

// Fleet page (OS plan, chantier 4): the workspace-wide run list with cost
// and blockers, keyset paging, the bulk cancel that reports every outcome,
// and the kill switch that halts and cancels in one move.

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func listRuns(t *testing.T, query string) RunsResponse {
	t.Helper()
	var out RunsResponse
	testutil.Call(t, testHandler.ListRuns, newRequest(http.MethodGet, "/api/runs"+query, nil)).Want(http.StatusOK).JSON(&out)
	return out
}

func findRun(runs []RunResponse, id string) *RunResponse {
	for i := range runs {
		if runs[i].ID == id {
			return &runs[i]
		}
	}
	return nil
}

func TestRunsListsTheFleetWithCostAndBlockers(t *testing.T) {
	rememberSettings(t)
	issue, running, agent, gate := openTestGate(t, "fleet gate")
	queued := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "queued"})
	done := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed", "started_at": testutil.Raw("now() - interval '2 minutes'"), "completed_at": testutil.Raw("now()")})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": done, "provider": "anthropic", "model": "claude-sonnet-5", "input_tokens": 1000, "output_tokens": 200, "cost_usd_ticks": 12_000_000})
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": done, "provider": "anthropic", "model": "claude-haiku-4-5-20251001", "input_tokens": 10, "output_tokens": 2, "cost_usd_ticks": 500_000})

	feed := listRuns(t, "?issue_id="+issue)
	if len(feed.Runs) != 3 {
		t.Fatalf("runs = %d, want 3: %+v", len(feed.Runs), feed.Runs)
	}
	// Newest first; the running run carries the gate as its blocker.
	r := findRun(feed.Runs, running)
	if r == nil || r.BlockedOn == nil || r.BlockedOn.Kind != RunBlockerGate || r.BlockedOn.DecisionID != *gate.DecisionID || r.AgentName == "" || r.Issue == nil || r.Issue.Identifier == "" {
		t.Fatalf("running row = %+v (blocked %+v)", r, r.BlockedOn)
	}
	q := findRun(feed.Runs, queued)
	if q == nil || q.BlockedOn != nil || q.Status != "queued" {
		t.Fatalf("queued row = %+v", q)
	}
	d := findRun(feed.Runs, done)
	if d == nil || d.CostUsdTicks != 12_500_000 || len(d.Usage) != 2 || d.DurationMs < 100_000 || d.BlockedOn != nil {
		t.Fatalf("done row = %+v", d)
	}
	if feed.Summary.Active < 2 || feed.Summary.Blocked < 1 || feed.Summary.CompletedSince < 1 || feed.Summary.CostSinceUsdTicks < 12_500_000 || feed.Summary.RunHalt.Halted {
		t.Fatalf("summary = %+v", feed.Summary)
	}

	// State filter and keyset paging.
	active := listRuns(t, "?issue_id="+issue+"&state=active")
	if len(active.Runs) != 2 || findRun(active.Runs, done) != nil {
		t.Fatalf("active = %+v", active.Runs)
	}
	page1 := listRuns(t, "?issue_id="+issue+"&limit=2")
	if len(page1.Runs) != 2 || page1.NextCursor == "" {
		t.Fatalf("page1 = %d runs, cursor %q", len(page1.Runs), page1.NextCursor)
	}
	page2 := listRuns(t, "?issue_id="+issue+"&limit=2&cursor="+page1.NextCursor)
	if len(page2.Runs) != 1 || page2.NextCursor != "" || findRun(page1.Runs, page2.Runs[0].ID) != nil {
		t.Fatalf("page2 = %+v cursor %q", page2.Runs, page2.NextCursor)
	}
	testutil.Call(t, testHandler.ListRuns, newRequest(http.MethodGet, "/api/runs?state=nope", nil)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ListRuns, newRequest(http.MethodGet, "/api/runs?cursor=garbage", nil)).Want(http.StatusBadRequest)
}

func TestRunsCancelManyReportsEveryOutcome(t *testing.T) {
	issue, running, agent := runningAgentRun(t, "fleet cancel")
	done := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()")})
	var out struct {
		Cancelled int                `json:"cancelled"`
		Results   []RunCancelOutcome `json:"results"`
	}
	testutil.Call(t, testHandler.CancelRuns, newRequest(http.MethodPost, "/api/runs/cancel", map[string]any{"task_ids": []string{running, done, "00000000-0000-0000-0000-00000000dead"}})).Want(http.StatusOK).JSON(&out)
	if out.Cancelled != 1 || len(out.Results) != 3 {
		t.Fatalf("out = %+v", out)
	}
	byID := map[string]string{}
	for _, r := range out.Results {
		byID[r.TaskID] = r.Outcome
	}
	if byID[running] != "cancelled" || byID[done] != "already_over" || byID["00000000-0000-0000-0000-00000000dead"] != "not_found" {
		t.Fatalf("outcomes = %v", byID)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, running).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("running task status after cancel = %q", status)
	}
	testutil.Call(t, testHandler.CancelRuns, newRequest(http.MethodPost, "/api/runs/cancel", map[string]any{"task_ids": []string{}})).Want(http.StatusBadRequest)
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2`, testWorkspaceID, AuditRunsCancelled); n < 1 {
		t.Fatalf("no audit entry")
	}
}

func TestKillSwitchHaltsTheFleetAndCancelsEverything(t *testing.T) {
	rememberSettings(t)
	issue, running, agent := runningAgentRun(t, "kill switch")
	queued := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "queued"})
	got := make(chan map[string]any, 4)
	testHandler.Bus.Subscribe(protocol.EventRunHaltChanged, func(e events.Event) {
		if p, ok := e.Payload.(map[string]any); ok {
			select {
			case got <- p:
			default:
			}
		}
	})

	dbfx.Exec(t, `UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	dbfx.Cleanup(t, `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	testutil.Call(t, testHandler.KillSwitch, newRequest(http.MethodPost, "/api/runs/kill-switch", map[string]any{"reason": "incident"})).Want(http.StatusForbidden)
	dbfx.Exec(t, `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)

	var out struct {
		Cancelled int                `json:"cancelled"`
		Results   []RunCancelOutcome `json:"results"`
		RunHalt   struct {
			Halted bool   `json:"halted"`
			Reason string `json:"reason"`
		} `json:"run_halt"`
	}
	testutil.Call(t, testHandler.KillSwitch, newRequest(http.MethodPost, "/api/runs/kill-switch", map[string]any{"reason": "incident"})).Want(http.StatusOK).JSON(&out)
	if !out.RunHalt.Halted || out.RunHalt.Reason != "incident" || out.Cancelled < 2 {
		t.Fatalf("kill switch = %+v", out)
	}
	for _, id := range []string{running, queued} {
		var status string
		dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, id).Scan(&status)
		if status != "cancelled" {
			t.Fatalf("task %s status = %q, want cancelled", id, status)
		}
	}
	feed := listRuns(t, "?issue_id="+issue)
	if !feed.Summary.RunHalt.Halted {
		t.Fatalf("summary halt = %+v", feed.Summary.RunHalt)
	}
	select {
	case p := <-got:
		if halt, ok := p["run_halt"].(service.RunHalt); !ok || !halt.Halted {
			t.Fatalf("event payload = %+v", p)
		}
	default:
		t.Fatalf("no run_halt:changed event")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2`, testWorkspaceID, AuditKillSwitch); n != 1 {
		t.Fatalf("kill switch audit entries = %d", n)
	}
	// Lifting is the ordinary halt endpoint.
	testutil.Call(t, testHandler.PutRunHalt, newRequest(http.MethodPut, "/api/run-halt", map[string]any{"halted": false})).Want(http.StatusOK)
	if feed := listRuns(t, "?issue_id="+issue); feed.Summary.RunHalt.Halted {
		t.Fatalf("halt not lifted")
	}
	_ = context.Background()
}
