package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Fleet reads (JEF-12): /api/fleet/status|cost|history aggregate existing
// dashboard/agent queries and fold restricted agents onto the
// __restricted_agents__ sentinel rather than dropping them, so the folded
// totals keep reconciling with the privileged view.

// fleetFixture seeds a private agent (invisible to the plain member) and a
// workspace-visible one, each with a running task and terminal history, plus
// usage rows with provider-reported cost for the cost endpoint.
type fleetFixture struct {
	privateAgentID string
	publicAgentID  string
	memberID       string
	// Seeded per-agent numbers, asserted against the owner view.
	publicTicks  int64
	privateTicks int64
}

func newFleetFixture(t *testing.T) fleetFixture {
	t.Helper()
	ctx := context.Background()

	privateAgentID, _, memberID := privateAgentTestFixture(t)
	publicAgentID := createHandlerTestAgent(t, "fleet-public-agent", nil)
	runtimeID := handlerTestRuntimeID(t)

	seedTask := func(agentID, status string, completedAt time.Time) string {
		cols := testutil.Cols{
			"runtime_id": runtimeID,
			"status":     status,
			"started_at": testutil.Raw("now() - interval '10 minutes'"),
		}
		if status == "running" {
			cols["started_at"] = testutil.Raw("now()")
		} else {
			cols["completed_at"] = completedAt
		}
		return dbfx.Task(t, agentID, cols)
	}

	// Live workload: one running task each.
	seedTask(privateAgentID, "running", time.Time{})
	seedTask(publicAgentID, "running", time.Time{})

	// Windowed history: one completed + one failed task for the public agent,
	// one completed for the private one, all today.
	completedPublic := seedTask(publicAgentID, "completed", time.Now())
	seedTask(publicAgentID, "failed", time.Now())
	completedPrivate := seedTask(privateAgentID, "completed", time.Now())

	// Older than any reasonable ?since= window but inside the feeder's fixed
	// 30d window: proves since trimming works.
	seedTask(publicAgentID, "completed", time.Now().Add(-10*24*time.Hour))

	fx := fleetFixture{
		privateAgentID: privateAgentID,
		publicAgentID:  publicAgentID,
		memberID:       memberID,
		publicTicks:    5_000_000_000, // $0.50
		privateTicks:   2_000_000_000, // $0.20
	}

	// Usage with provider-reported cost. The rollup only picks rows whose task
	// has a runtime_id, which seedTask sets.
	for _, seed := range []struct {
		taskID string
		ticks  int64
		input  int64
	}{
		{completedPublic, fx.publicTicks, 1000},
		{completedPrivate, fx.privateTicks, 400},
	} {
		dbfx.Exec(t, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cost_usd_ticks, created_at)
			VALUES ($1, 'claude', 'fleet-test-model', $2, 100, $3, now())
			ON CONFLICT (task_id, provider, model) DO UPDATE SET cost_usd_ticks = EXCLUDED.cost_usd_ticks
		`, seed.taskID, seed.input, seed.ticks)
	}
	if _, err := testPool.Exec(ctx, `
		SELECT rollup_task_usage_hourly_window('1970-01-01'::timestamptz, now() + interval '1 hour')
	`); err != nil {
		t.Fatalf("rollup window: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM task_usage_hourly WHERE model = 'fleet-test-model'`)
	})

	return fx
}

func fleetRowsByAgent[T any](rows []T, idOf func(T) string) map[string]T {
	out := make(map[string]T, len(rows))
	for _, row := range rows {
		out[idOf(row)] = row
	}
	return out
}

func TestFleetStatus(t *testing.T) {
	fx := newFleetFixture(t)

	var ownerRows, memberRows []FleetStatusRow
	testutil.Call(t, testHandler.GetFleetStatus, newRequest(http.MethodGet, "/api/fleet/status", nil)).
		Want(http.StatusOK).JSON(&ownerRows)

	memberReq := newRequest(http.MethodGet, "/api/fleet/status", nil)
	memberReq.Header.Set("X-User-ID", fx.memberID)
	w := testutil.Call(t, testHandler.GetFleetStatus, memberReq).Want(http.StatusOK)
	w.JSON(&memberRows)
	if strings.Contains(w.Body.String(), fx.privateAgentID) {
		t.Fatalf("member response leaked private agent id: %s", w.Body.String())
	}

	owner := fleetRowsByAgent(ownerRows, func(r FleetStatusRow) string { return r.AgentID })
	pub := owner[fx.publicAgentID]
	// 3 terminal tasks in the 30d window: today's completed + failed, plus
	// the 10-day-old completion. failed is a subset of task_count.
	if pub.RunningTaskCount != 1 || pub.TaskCount != 3 || pub.FailedCount != 1 {
		t.Fatalf("owner public row = %+v, want 1 running / 3 tasks / 1 failed", pub)
	}
	if pub.Name != "fleet-public-agent" {
		t.Fatalf("owner public row name = %q", pub.Name)
	}
	priv := owner[fx.privateAgentID]
	if priv.RunningTaskCount != 1 || priv.TaskCount != 1 || priv.FailedCount != 0 {
		t.Fatalf("owner private row = %+v, want 1 running / 1 task / 0 failed", priv)
	}
	if _, folded := owner[restrictedAgentsRowID]; folded {
		t.Fatalf("owner sees everything; no sentinel bucket expected: %+v", ownerRows)
	}

	member := fleetRowsByAgent(memberRows, func(r FleetStatusRow) string { return r.AgentID })
	if _, leaked := member[fx.privateAgentID]; leaked {
		t.Fatalf("member view named the private agent: %+v", memberRows)
	}
	sentinel, ok := member[restrictedAgentsRowID]
	if !ok {
		t.Fatalf("member view missing the folded bucket: %+v", memberRows)
	}
	if sentinel.RunningTaskCount != 1 || sentinel.TaskCount != 1 || sentinel.Name != "" {
		t.Fatalf("folded bucket = %+v, want the private agent's counts and no name", sentinel)
	}
	if mp := member[fx.publicAgentID]; mp.RunningTaskCount != 1 || mp.TaskCount != 3 {
		t.Fatalf("member public row = %+v", mp)
	}
}

func TestFleetStatusSinceAndAgentFilter(t *testing.T) {
	fx := newFleetFixture(t)

	// since=3d trims the 10-day-old completion out of the counts, leaving
	// today's completed + failed pair.
	since := time.Now().Add(-3 * 24 * time.Hour).UTC().Format(time.RFC3339)
	var rows []FleetStatusRow
	testutil.Call(t, testHandler.GetFleetStatus,
		newRequest(http.MethodGet, "/api/fleet/status?since="+since, nil)).
		Want(http.StatusOK).JSON(&rows)
	byAgent := fleetRowsByAgent(rows, func(r FleetStatusRow) string { return r.AgentID })
	if got := byAgent[fx.publicAgentID].TaskCount; got != 2 {
		t.Fatalf("public task count with since=3d = %d, want 2 (old task trimmed)", got)
	}

	// agent_id narrows to the one agent.
	testutil.Call(t, testHandler.GetFleetStatus,
		newRequest(http.MethodGet, "/api/fleet/status?agent_id="+fx.publicAgentID, nil)).
		Want(http.StatusOK).JSON(&rows)
	if len(rows) != 1 || rows[0].AgentID != fx.publicAgentID {
		t.Fatalf("agent filter = %+v, want only the public agent", rows)
	}

	// A member naming an agent they may not see gets an empty list, not that
	// agent's numbers under the sentinel.
	memberReq := newRequest(http.MethodGet, "/api/fleet/status?agent_id="+fx.privateAgentID, nil)
	memberReq.Header.Set("X-User-ID", fx.memberID)
	testutil.Call(t, testHandler.GetFleetStatus, memberReq).Want(http.StatusOK).JSON(&rows)
	if len(rows) != 0 {
		t.Fatalf("member filtering to a restricted agent = %+v, want empty", rows)
	}

	// Malformed filters are 400s.
	testutil.Call(t, testHandler.GetFleetStatus,
		newRequest(http.MethodGet, "/api/fleet/status?since=not-a-date", nil)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.GetFleetStatus,
		newRequest(http.MethodGet, "/api/fleet/status?agent_id=not-a-uuid", nil)).Want(http.StatusBadRequest)
}

func TestFleetCost(t *testing.T) {
	fx := newFleetFixture(t)

	var ownerRows, memberRows []FleetCostRow
	testutil.Call(t, testHandler.GetFleetCost, newRequest(http.MethodGet, "/api/fleet/cost", nil)).
		Want(http.StatusOK).JSON(&ownerRows)

	memberReq := newRequest(http.MethodGet, "/api/fleet/cost", nil)
	memberReq.Header.Set("X-User-ID", fx.memberID)
	w := testutil.Call(t, testHandler.GetFleetCost, memberReq).Want(http.StatusOK)
	w.JSON(&memberRows)
	if strings.Contains(w.Body.String(), fx.privateAgentID) {
		t.Fatalf("member response leaked private agent id: %s", w.Body.String())
	}

	owner := fleetRowsByAgent(ownerRows, func(r FleetCostRow) string { return r.AgentID })
	if got := owner[fx.publicAgentID].CostUSDTicks; got != fx.publicTicks {
		t.Fatalf("owner public cost = %d ticks, want %d", got, fx.publicTicks)
	}
	if got := owner[fx.privateAgentID].CostUSDTicks; got != fx.privateTicks {
		t.Fatalf("owner private cost = %d ticks, want %d", got, fx.privateTicks)
	}

	member := fleetRowsByAgent(memberRows, func(r FleetCostRow) string { return r.AgentID })
	if _, leaked := member[fx.privateAgentID]; leaked {
		t.Fatalf("member view named the private agent: %+v", memberRows)
	}
	sentinel, ok := member[restrictedAgentsRowID]
	if !ok || sentinel.CostUSDTicks < fx.privateTicks {
		t.Fatalf("folded bucket = %+v (ok=%v), want at least the private agent's %d ticks",
			sentinel, ok, fx.privateTicks)
	}

	// Folding must not change what the whole workspace spent: the member's
	// folded view sums to exactly the owner's unfiltered view.
	var ownerTotal, memberTotal int64
	for _, r := range ownerRows {
		ownerTotal += r.CostUSDTicks
	}
	for _, r := range memberRows {
		memberTotal += r.CostUSDTicks
	}
	if ownerTotal != memberTotal {
		t.Fatalf("folding changed the totals — owner %d ticks, member %d", ownerTotal, memberTotal)
	}

	// agent_id narrows; since in the future zeroes the window.
	testutil.Call(t, testHandler.GetFleetCost,
		newRequest(http.MethodGet, "/api/fleet/cost?agent_id="+fx.publicAgentID, nil)).
		Want(http.StatusOK).JSON(&ownerRows)
	if len(ownerRows) != 1 || ownerRows[0].CostUSDTicks != fx.publicTicks {
		t.Fatalf("agent-filtered cost = %+v", ownerRows)
	}
	future := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	testutil.Call(t, testHandler.GetFleetCost,
		newRequest(http.MethodGet, "/api/fleet/cost?since="+future, nil)).
		Want(http.StatusOK).JSON(&ownerRows)
	if len(ownerRows) != 0 {
		t.Fatalf("cost with future since = %+v, want empty", ownerRows)
	}
}

func TestFleetHistory(t *testing.T) {
	fx := newFleetFixture(t)
	today := time.Now().UTC().Format("2006-01-02")

	var ownerRows, memberRows []FleetHistoryRow
	testutil.Call(t, testHandler.GetFleetHistory, newRequest(http.MethodGet, "/api/fleet/history", nil)).
		Want(http.StatusOK).JSON(&ownerRows)

	memberReq := newRequest(http.MethodGet, "/api/fleet/history", nil)
	memberReq.Header.Set("X-User-ID", fx.memberID)
	w := testutil.Call(t, testHandler.GetFleetHistory, memberReq).Want(http.StatusOK)
	w.JSON(&memberRows)
	if strings.Contains(w.Body.String(), fx.privateAgentID) {
		t.Fatalf("member response leaked private agent id: %s", w.Body.String())
	}

	// Owner: today's bucket per agent; the public agent's bucket holds both of
	// today's terminal tasks (1 completed + 1 failed).
	var ownerPublicToday, ownerPrivateToday *FleetHistoryRow
	for i := range ownerRows {
		r := &ownerRows[i]
		if r.Date != today {
			continue
		}
		switch r.AgentID {
		case fx.publicAgentID:
			ownerPublicToday = r
		case fx.privateAgentID:
			ownerPrivateToday = r
		}
	}
	if ownerPublicToday == nil || ownerPublicToday.TaskCount != 2 || ownerPublicToday.FailedCount != 1 {
		t.Fatalf("owner public today = %+v, want 2 tasks / 1 failed", ownerPublicToday)
	}
	if ownerPrivateToday == nil || ownerPrivateToday.TaskCount != 1 {
		t.Fatalf("owner private today = %+v, want 1 task", ownerPrivateToday)
	}

	// Member: the private agent's bucket folds onto the sentinel BY DATE.
	var sentinelToday *FleetHistoryRow
	for i := range memberRows {
		r := &memberRows[i]
		if r.AgentID == fx.privateAgentID {
			t.Fatalf("member history named the private agent: %+v", memberRows)
		}
		if r.AgentID == restrictedAgentsRowID && r.Date == today {
			sentinelToday = r
		}
	}
	if sentinelToday == nil || sentinelToday.TaskCount != 1 {
		t.Fatalf("member sentinel today = %+v, want the private agent's single task", sentinelToday)
	}

	// since=3d drops the 10-day-old bucket; the unfiltered history keeps it.
	since := time.Now().Add(-3 * 24 * time.Hour).UTC().Format(time.RFC3339)
	var trimmed []FleetHistoryRow
	testutil.Call(t, testHandler.GetFleetHistory,
		newRequest(http.MethodGet, "/api/fleet/history?since="+since+"&agent_id="+fx.publicAgentID, nil)).
		Want(http.StatusOK).JSON(&trimmed)
	if len(trimmed) != 1 || trimmed[0].Date != today {
		t.Fatalf("trimmed public history = %+v, want only today's bucket", trimmed)
	}
}
