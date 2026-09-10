package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Runtime-pinned attempts (JEF-234): an attempt may name the runtime (CLI) it
// races on. The pin is stamped onto the task row, only that runtime can claim
// it, and the comparison response carries runtime + cost + duration per
// attempt.

// Happy path: the override is echoed, the task row is stamped and pinned, and
// the attempt without an override keeps the agent's bound runtime unpinned.
func TestRunGroupRuntimeOverrideHappyPath(t *testing.T) {
	bound := dbfx.Runtime(t, "race-bound-runtime")
	pinned := dbfx.Runtime(t, "race-pinned-runtime", testutil.Cols{"custom_name": "Race Box"})
	agent := dbfx.Agent(t, "race-pinned agent", bound)
	issue := dbfx.Issue(t, "race two runtimes")
	cleanupRunGroups(t, issue)

	group := startRunGroup(t, issue, map[string]any{
		"attempts": []map[string]any{
			{"agent_id": agent, "runtime_id": pinned},
			{"agent_id": agent},
		},
	}, http.StatusCreated)

	got := group.Attempts[0]
	if got.RuntimeID != pinned {
		t.Fatalf("pinned attempt runtime_id = %q, want %q", got.RuntimeID, pinned)
	}
	if got.RuntimeName != "Race Box" {
		t.Fatalf("pinned attempt runtime_name = %q, want the runtime's custom name", got.RuntimeName)
	}
	if got.CostUsdTicks != 0 || got.DurationSeconds != 0 {
		t.Fatalf("fresh attempt metrics = cost %d / duration %d, want 0/0", got.CostUsdTicks, got.DurationSeconds)
	}
	var runtimePinned bool
	var stampedRuntime string
	dbfx.QueryRow(t, `SELECT runtime_pinned, runtime_id FROM agent_task_queue WHERE id = $1`, got.TaskID).Scan(&runtimePinned, &stampedRuntime)
	if !runtimePinned || stampedRuntime != pinned {
		t.Fatalf("task row = runtime_pinned %v / runtime_id %q, want true / %q", runtimePinned, stampedRuntime, pinned)
	}

	plain := group.Attempts[1]
	if plain.RuntimeID != bound {
		t.Fatalf("unpinned attempt runtime_id = %q, want the agent's bound runtime %q", plain.RuntimeID, bound)
	}
	dbfx.QueryRow(t, `SELECT runtime_pinned FROM agent_task_queue WHERE id = $1`, plain.TaskID).Scan(&runtimePinned)
	if runtimePinned {
		t.Fatal("an attempt without runtime_id must not be pinned")
	}
}

// A malformed runtime_id is a 400; an unknown one and one from another
// workspace are both 404 — and none of them may half-create a race.
func TestRunGroupRuntimeOverrideRefusals(t *testing.T) {
	runtime := handlerTestRuntimeID(t)
	agentA := dbfx.Agent(t, "race-refuse agent a", runtime)
	agentB := dbfx.Agent(t, "race-refuse agent b", runtime)
	issue := dbfx.Issue(t, "refused runtime pins")
	cleanupRunGroups(t, issue)

	startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{
		{"agent_id": agentA, "runtime_id": "not-a-uuid"}, {"agent_id": agentB},
	}}, http.StatusBadRequest)

	startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{
		{"agent_id": agentA, "runtime_id": uuid.NewString()}, {"agent_id": agentB},
	}}, http.StatusNotFound)

	foreign := dbfx.Workspace(t, "Race runtime foreign", "race-rt-foreign-"+uuid.NewString())
	foreignRuntime := dbfx.Runtime(t, "foreign runtime", testutil.Cols{"workspace_id": foreign})
	startRunGroup(t, issue, map[string]any{"attempts": []map[string]any{
		{"agent_id": agentA, "runtime_id": foreignRuntime}, {"agent_id": agentB},
	}}, http.StatusNotFound)

	if n := dbfx.Count(t, `SELECT COUNT(*) FROM run_group WHERE issue_id = $1`, issue); n != 0 {
		t.Fatalf("refused races left %d group rows", n)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issue); n != 0 {
		t.Fatalf("refused races left %d task rows", n)
	}
}

// The pin is a claim rule: only the pinned runtime picks the attempt up. The
// agent's own bound runtime must not see it, and the pinned runtime must not
// see the unpinned sibling (stamped with the bound runtime) either.
func TestRunGroupRuntimeOverrideClaim(t *testing.T) {
	bound := dbfx.Runtime(t, "claim-bound-runtime")
	pinned := dbfx.Runtime(t, "claim-pinned-runtime")
	agent := dbfx.Agent(t, "race-claim agent", bound)
	issue := dbfx.Issue(t, "pinned claims")
	cleanupRunGroups(t, issue)

	group := startRunGroup(t, issue, map[string]any{
		"attempts": []map[string]any{
			{"agent_id": agent, "runtime_id": pinned},
			{"agent_id": agent},
		},
	}, http.StatusCreated)
	pinnedTask := group.Attempts[0].TaskID
	plainTask := group.Attempts[1].TaskID

	if got := claimOnce(t, agent, pinned); got != pinnedTask {
		t.Fatalf("claim by the pinned runtime = %q, want the pinned attempt %q", got, pinnedTask)
	}
	if got := claimOnce(t, agent, bound); got != plainTask {
		t.Fatalf("claim by the bound runtime = %q, want only the unpinned attempt %q (the pinned one must be invisible to it)", got, plainTask)
	}
}

// The comparison fields fill in once the run reports usage and completes:
// cost sums task_usage, duration is completed minus started.
func TestRunGroupAttemptMetrics(t *testing.T) {
	runtime := handlerTestRuntimeID(t)
	agentA := dbfx.Agent(t, "race-metrics agent a", runtime)
	agentB := dbfx.Agent(t, "race-metrics agent b", runtime)
	issue := dbfx.Issue(t, "measured race")
	cleanupRunGroups(t, issue)

	group := startRunGroup(t, issue, map[string]any{
		"attempts": []map[string]any{{"agent_id": agentA}, {"agent_id": agentB}},
	}, http.StatusCreated)
	done := group.Attempts[0].TaskID
	running := group.Attempts[1].TaskID

	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', started_at = TIMESTAMPTZ '2026-09-01 10:00:00Z', completed_at = TIMESTAMPTZ '2026-09-01 10:02:00Z' WHERE id = $1`, done)
	dbfx.Exec(t, `INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, cost_usd_ticks, updated_at)
		VALUES ($1, 'anthropic', 'sonnet', 10, 20, 0, 0, 5000, now()),
		       ($1, 'anthropic', 'opus', 1, 2, 0, 0, 700, now())`, done)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', dispatched_at = now(), started_at = now() WHERE id = $1`, running)

	var listed struct {
		Groups []RunGroupResponse `json:"groups"`
	}
	testutil.Call(t, testHandler.ListIssueRunGroups, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/run-groups", nil), "id", issue)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Groups) != 1 || len(listed.Groups[0].Attempts) != 2 {
		t.Fatalf("listed = %+v", listed.Groups)
	}
	byTask := map[string]RunGroupAttemptResponse{}
	for _, a := range listed.Groups[0].Attempts {
		byTask[a.TaskID] = a
	}
	if got := byTask[done]; got.CostUsdTicks != 5700 || got.DurationSeconds != 120 {
		t.Fatalf("completed attempt metrics = cost %d / duration %d, want 5700 / 120", got.CostUsdTicks, got.DurationSeconds)
	}
	if got := byTask[done]; got.RuntimeID != runtime || got.RuntimeName == "" {
		t.Fatalf("completed attempt runtime = %q / %q, want %q with a name", got.RuntimeID, got.RuntimeName, runtime)
	}
	if got := byTask[running]; got.CostUsdTicks != 0 || got.DurationSeconds != 0 {
		t.Fatalf("running attempt metrics = cost %d / duration %d, want 0/0 until it completes", got.CostUsdTicks, got.DurationSeconds)
	}
}
