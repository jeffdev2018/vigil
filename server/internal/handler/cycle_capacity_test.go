package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Per-actor cycle capacity and velocity (JEF-246). Cycle lifecycle itself is
// covered by cycle_test.go; this file covers the declared-capacity CRUD, its
// validation and auth, and the velocity report's actor/other/history rules.

func putCycleCapacities(t *testing.T, cycleID string, capacities any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.PutCycleCapacities, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/cycles/"+cycleID+"/capacities", map[string]any{"capacities": capacities}),
		"id", cycleID))
}

func getCycleCapacities(t *testing.T, cycleID string) []CycleActorCapacityEntry {
	t.Helper()
	var out struct {
		Capacities []CycleActorCapacityEntry `json:"capacities"`
	}
	testutil.Call(t, testHandler.GetCycleCapacities, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/cycles/"+cycleID+"/capacities", nil), "id", cycleID)).
		Want(http.StatusOK).JSON(&out)
	return out.Capacities
}

func getCycleVelocity(t *testing.T, cycleID string) CycleVelocityResponse {
	t.Helper()
	var out CycleVelocityResponse
	testutil.Call(t, testHandler.GetCycleVelocity, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/cycles/"+cycleID+"/velocity", nil), "id", cycleID)).
		Want(http.StatusOK).JSON(&out)
	return out
}

// cleanupCycleCapacities removes a cycle's capacity rows at test end; the
// cycle row itself is already cleaned by createCycle, and no FK cascades.
func cleanupCycleCapacities(t *testing.T, cycleID string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM cycle_actor_capacity WHERE cycle_id = $1`, cycleID)
}

// Acceptance: PUT is a full replace and GET/PUT answer the frozen contract
// shape — members before agents, then by name.
func TestCycleCapacitiesFullReplaceRoundTrip(t *testing.T) {
	project := newCycleProject(t, "capacity project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Caps", "start_date": cycleDate(0), "end_date": cycleDate(7),
	}).Want(http.StatusCreated).JSON(&cycle)
	cleanupCycleCapacities(t, cycle.ID)

	if got := getCycleCapacities(t, cycle.ID); len(got) != 0 {
		t.Fatalf("initial capacities = %d, want 0", len(got))
	}

	agentID := createHandlerTestAgent(t, "Cap Agent "+uuid.NewString()[:8], nil)
	memberB := dbfx.User(t, "Cap Member B", "cap-b-"+uuid.NewString()[:8]+"@multica.ai")
	dbfx.Member(t, testWorkspaceID, memberB, "member")

	var put struct {
		Capacities []CycleActorCapacityEntry `json:"capacities"`
	}
	putCycleCapacities(t, cycle.ID, []map[string]any{
		{"actor_type": "agent", "actor_id": agentID, "points": 4},
		{"actor_type": "member", "actor_id": memberB, "points": 7},
		{"actor_type": "member", "actor_id": testUserID, "points": 10},
	}).Want(http.StatusOK).JSON(&put)

	if len(put.Capacities) != 3 {
		t.Fatalf("capacities = %d, want 3", len(put.Capacities))
	}
	// Members first (by name), then agents — the order the contract freezes.
	if put.Capacities[0].ActorType != "member" || put.Capacities[0].ActorID != memberB ||
		put.Capacities[1].ActorType != "member" || put.Capacities[1].ActorID != testUserID ||
		put.Capacities[2].ActorType != "agent" || put.Capacities[2].ActorID != agentID {
		t.Fatalf("order = %v, want members by name then agents", put.Capacities)
	}
	if put.Capacities[0].Name != "Cap Member B" || put.Capacities[0].Points != 7 {
		t.Fatalf("first entry = %+v, want Cap Member B with 7 points", put.Capacities[0])
	}
	if put.Capacities[2].Name == "" {
		t.Fatal("agent entry has no resolved display name")
	}

	// Full replace: the second PUT is the whole new set, not a merge.
	putCycleCapacities(t, cycle.ID, []map[string]any{
		{"actor_type": "member", "actor_id": testUserID, "points": 3},
	}).Want(http.StatusOK)
	got := getCycleCapacities(t, cycle.ID)
	if len(got) != 1 || got[0].ActorID != testUserID || got[0].Points != 3 {
		t.Fatalf("after replace = %+v, want only the test user with 3 points", got)
	}

	// The change is audited like the cycle's other writes.
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE action = 'cycle.capacities_updated' AND entity_id = $1`, cycle.ID); n != 2 {
		t.Fatalf("audit rows = %d, want 2 (one per PUT)", n)
	}

	// An empty list clears the set.
	putCycleCapacities(t, cycle.ID, []map[string]any{}).Want(http.StatusOK)
	if got := getCycleCapacities(t, cycle.ID); len(got) != 0 {
		t.Fatalf("after clear = %d rows, want 0", len(got))
	}
}

// The write refuses anything that would render as a plan nobody can read.
func TestCycleCapacitiesValidation(t *testing.T) {
	project := newCycleProject(t, "capacity validation project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Caps validation", "start_date": cycleDate(0), "end_date": cycleDate(7),
	}).Want(http.StatusCreated).JSON(&cycle)
	cleanupCycleCapacities(t, cycle.ID)

	stranger := dbfx.User(t, "No Member", "no-member-"+uuid.NewString()[:8]+"@multica.ai")
	for name, row := range map[string]map[string]any{
		"bad actor_type":  {"actor_type": "squad", "actor_id": testUserID, "points": 1},
		"bad actor_id":    {"actor_type": "member", "actor_id": "not-a-uuid", "points": 1},
		"unknown member":  {"actor_type": "member", "actor_id": uuid.NewString(), "points": 1},
		"non-member user": {"actor_type": "member", "actor_id": stranger, "points": 1},
		"unknown agent":   {"actor_type": "agent", "actor_id": uuid.NewString(), "points": 1},
		"points over max": {"actor_type": "member", "actor_id": testUserID, "points": 10001},
		"points negative": {"actor_type": "member", "actor_id": testUserID, "points": -1},
		"points missing":  {"actor_type": "member", "actor_id": testUserID},
	} {
		t.Run(name, func(t *testing.T) {
			putCycleCapacities(t, cycle.ID, []map[string]any{row}).Want(http.StatusBadRequest)
		})
	}
	t.Run("duplicate actor", func(t *testing.T) {
		putCycleCapacities(t, cycle.ID, []map[string]any{
			{"actor_type": "member", "actor_id": testUserID, "points": 1},
			{"actor_type": "member", "actor_id": testUserID, "points": 2},
		}).Want(http.StatusBadRequest)
	})
	t.Run("too many rows", func(t *testing.T) {
		rows := make([]map[string]any, 0, 201)
		for i := 0; i < 201; i++ {
			rows = append(rows, map[string]any{"actor_type": "member", "actor_id": uuid.NewString(), "points": 1})
		}
		putCycleCapacities(t, cycle.ID, rows).Want(http.StatusBadRequest)
	})
	t.Run("unknown cycle", func(t *testing.T) {
		putCycleCapacities(t, uuid.NewString(), []map[string]any{}).Want(http.StatusNotFound)
	})

	// None of the refused writes may have landed.
	if got := getCycleCapacities(t, cycle.ID); len(got) != 0 {
		t.Fatalf("capacities after refused writes = %d, want 0", len(got))
	}
}

// Capacities are workspace-scoped: an outsider cannot even learn the cycle
// exists, on read or on write.
func TestCycleCapacitiesRequireWorkspaceMembership(t *testing.T) {
	project := newCycleProject(t, "capacity auth project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Caps auth", "start_date": cycleDate(0), "end_date": cycleDate(7),
	}).Want(http.StatusCreated).JSON(&cycle)
	cleanupCycleCapacities(t, cycle.ID)

	outsider := dbfx.User(t, "Outsider", "outsider-"+uuid.NewString()[:8]+"@multica.ai")
	testutil.Call(t, testHandler.GetCycleCapacities, testutil.WithURLParams(
		newRequestAs(outsider, http.MethodGet, "/api/cycles/"+cycle.ID+"/capacities", nil), "id", cycle.ID)).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.PutCycleCapacities, testutil.WithURLParams(
		newRequestAs(outsider, http.MethodPut, "/api/cycles/"+cycle.ID+"/capacities", map[string]any{"capacities": []any{}}), "id", cycle.ID)).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.GetCycleVelocity, testutil.WithURLParams(
		newRequestAs(outsider, http.MethodGet, "/api/cycles/"+cycle.ID+"/velocity", nil), "id", cycle.ID)).
		Want(http.StatusNotFound)
}

// Velocity credits direct assignees only, keeps declared-but-idle actors
// visible, and rolls squad/unassigned done load into the other bucket.
func TestCycleVelocityActorsAndOtherBucket(t *testing.T) {
	project := newCycleProject(t, "velocity project "+uuid.NewString()[:8])
	prop := dbfx.Insert(t, "issue_property", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "Points " + uuid.NewString()[:8],
		"type":         "number",
		"config":       testutil.Raw("'{}'::jsonb"),
		"position":     1,
	})
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Velocity", "start_date": cycleDate(0), "end_date": cycleDate(7),
		"load_property_id": prop,
	}).Want(http.StatusCreated).JSON(&cycle)
	cleanupCycleCapacities(t, cycle.ID)

	agentID := createHandlerTestAgent(t, "Vel Agent "+uuid.NewString()[:8], nil)
	idleMember := dbfx.User(t, "Idle Member", "idle-"+uuid.NewString()[:8]+"@multica.ai")
	dbfx.Member(t, testWorkspaceID, idleMember, "member")
	squadID := dbfx.Squad(t, "Vel Squad "+uuid.NewString()[:8], agentID)

	points := func(n int) testutil.Cols {
		return testutil.Cols{"properties": testutil.Raw(fmt.Sprintf(`'{"%s": %d}'::jsonb`, prop, n))}
	}
	mergeCols := func(base testutil.Cols, over testutil.Cols) testutil.Cols {
		for k, v := range over {
			base[k] = v
		}
		return base
	}
	base := testutil.Cols{"project_id": project, "cycle_id": cycle.ID}

	// Member with done work AND a declared capacity.
	dbfx.Issue(t, "member done", mergeCols(base, testutil.Cols{
		"assignee_type": "member", "assignee_id": testUserID, "status": "done",
		"properties": points(5)["properties"],
	}))
	// Agent with done work but NO declared capacity.
	dbfx.Issue(t, "agent done", mergeCols(base, testutil.Cols{
		"assignee_type": "agent", "assignee_id": agentID, "status": "done",
		"properties": points(3)["properties"],
	}))
	// Squad-assigned and unassigned done work: no single actor to credit.
	dbfx.Issue(t, "squad done", mergeCols(base, testutil.Cols{
		"assignee_type": "squad", "assignee_id": squadID, "status": "done",
		"properties": points(4)["properties"],
	}))
	dbfx.Issue(t, "unassigned done", mergeCols(base, testutil.Cols{
		"status": "done", "properties": points(2)["properties"],
	}))
	// Open work counts nowhere in a velocity report.
	dbfx.Issue(t, "still open", mergeCols(base, testutil.Cols{
		"assignee_type": "member", "assignee_id": testUserID, "status": "todo",
		"properties": points(100)["properties"],
	}))
	// A stale completed_at does not make an open issue done: done is the
	// CURRENT status's category.
	dbfx.Issue(t, "reopened", mergeCols(base, testutil.Cols{
		"assignee_type": "member", "assignee_id": testUserID, "status": "todo",
		"completed_at": testutil.Raw("now()"), "properties": points(50)["properties"],
	}))
	// Done work in ANOTHER cycle is not this cycle's velocity.
	var other CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Other", "start_date": cycleDate(0), "end_date": cycleDate(7),
		"load_property_id": prop,
	}).Want(http.StatusCreated).JSON(&other)
	dbfx.Issue(t, "other cycle done", testutil.Cols{
		"project_id": project, "cycle_id": other.ID, "assignee_type": "member", "assignee_id": testUserID,
		"status": "done", "properties": points(40)["properties"],
	})

	// Capacities: the test user declares 10; the idle member declares 7 and
	// does nothing; the agent declares nothing.
	putCycleCapacities(t, cycle.ID, []map[string]any{
		{"actor_type": "member", "actor_id": testUserID, "points": 10},
		{"actor_type": "member", "actor_id": idleMember, "points": 7},
	}).Want(http.StatusOK)

	got := getCycleVelocity(t, cycle.ID)
	if got.CycleID != cycle.ID {
		t.Fatalf("cycle_id = %s, want %s", got.CycleID, cycle.ID)
	}
	byActor := map[string]CycleVelocityActor{}
	for _, a := range got.Actors {
		byActor[a.ActorType+"/"+a.ActorID] = a
	}
	if len(got.Actors) != 3 {
		t.Fatalf("actors = %+v, want 3 (done member, done agent, idle declared member)", got.Actors)
	}
	member := byActor["member/"+testUserID]
	if member.DonePoints != 5 || member.DoneCount != 1 {
		t.Fatalf("member done = %v/%d, want 5/1 — open and foreign-cycle work must not count", member.DonePoints, member.DoneCount)
	}
	if member.CapacityPoints == nil || *member.CapacityPoints != 10 {
		t.Fatalf("member capacity = %v, want declared 10", member.CapacityPoints)
	}
	if member.Name == "" {
		t.Fatal("member actor has no resolved display name")
	}
	agent := byActor["agent/"+agentID]
	if agent.DonePoints != 3 || agent.DoneCount != 1 {
		t.Fatalf("agent done = %v/%d, want 3/1", agent.DonePoints, agent.DoneCount)
	}
	if agent.CapacityPoints != nil {
		t.Fatalf("agent capacity = %v, want null — undeclared is not zero", *agent.CapacityPoints)
	}
	idle := byActor["member/"+idleMember]
	if idle.DonePoints != 0 || idle.DoneCount != 0 || idle.CapacityPoints == nil || *idle.CapacityPoints != 7 {
		t.Fatalf("idle declared actor = %+v, want 0 done with capacity 7 — a declared capacity must always show", idle)
	}
	if got.OtherDonePoints != 6 {
		t.Fatalf("other_done_points = %v, want 6 (squad 4 + unassigned 2)", got.OtherDonePoints)
	}
}

// Without a load property the unit is the issue count, and the history lists
// the project's other cycles most recent first, each counted in ITS own unit.
func TestCycleVelocityHistoryOrderingAndUnits(t *testing.T) {
	project := newCycleProject(t, "history project "+uuid.NewString()[:8])
	prop := dbfx.Insert(t, "issue_property", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "Hist Points " + uuid.NewString()[:8],
		"type":         "number",
		"config":       testutil.Raw("'{}'::jsonb"),
		"position":     1,
	})

	// Oldest sibling, counted in issues (no load property).
	var oldest CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Oldest", "start_date": cycleDate(-30), "end_date": cycleDate(-23),
	}).Want(http.StatusCreated).JSON(&oldest)
	dbfx.Issue(t, "old done 1", testutil.Cols{"project_id": project, "cycle_id": oldest.ID, "status": "done"})
	dbfx.Issue(t, "old done 2", testutil.Cols{"project_id": project, "cycle_id": oldest.ID, "status": "done"})
	dbfx.Issue(t, "old open", testutil.Cols{"project_id": project, "cycle_id": oldest.ID, "status": "todo"})

	// Newer sibling, counted in the points property.
	var middle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Middle", "start_date": cycleDate(-20), "end_date": cycleDate(-13),
		"load_property_id": prop,
	}).Want(http.StatusCreated).JSON(&middle)
	dbfx.Issue(t, "mid done", testutil.Cols{
		"project_id": project, "cycle_id": middle.ID, "status": "done",
		"properties": testutil.Raw(fmt.Sprintf(`'{"%s": 8}'::jsonb`, prop)),
	})

	var current CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Current", "start_date": cycleDate(0), "end_date": cycleDate(7),
	}).Want(http.StatusCreated).JSON(&current)
	cleanupCycleCapacities(t, current.ID)

	got := getCycleVelocity(t, current.ID)
	if len(got.History) != 2 {
		t.Fatalf("history = %+v, want the 2 sibling cycles (self excluded)", got.History)
	}
	if got.History[0].CycleID != middle.ID || got.History[1].CycleID != oldest.ID {
		t.Fatalf("history order = %s, %s — want most recent first", got.History[0].CycleID, got.History[1].CycleID)
	}
	if got.History[0].DonePoints != 8 || got.History[0].DoneCount != 1 {
		t.Fatalf("middle history = %v/%d, want 8/1 in ITS OWN points unit", got.History[0].DonePoints, got.History[0].DoneCount)
	}
	if got.History[1].DonePoints != 2 || got.History[1].DoneCount != 2 {
		t.Fatalf("oldest history = %v/%d, want 2/2 — no load property means issue count", got.History[1].DonePoints, got.History[1].DoneCount)
	}
	if got.History[0].StartDate != cycleDate(-20) || got.History[0].EndDate != cycleDate(-13) {
		t.Fatalf("history dates = %s..%s, want the sibling's dates", got.History[0].StartDate, got.History[0].EndDate)
	}
}

// The history is capped at the 12 most recent sibling cycles.
func TestCycleVelocityHistoryIsCappedAtTwelve(t *testing.T) {
	project := newCycleProject(t, "capped history project "+uuid.NewString()[:8])
	for i := 0; i < 14; i++ {
		var c CycleResponse
		createCycle(t, map[string]any{
			"project_id": project, "name": fmt.Sprintf("C%02d", i),
			"start_date": cycleDate(-100 + i*7), "end_date": cycleDate(-94 + i*7),
		}).Want(http.StatusCreated).JSON(&c)
	}
	var current CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Current", "start_date": cycleDate(0), "end_date": cycleDate(7),
	}).Want(http.StatusCreated).JSON(&current)
	cleanupCycleCapacities(t, current.ID)

	got := getCycleVelocity(t, current.ID)
	if len(got.History) != 12 {
		t.Fatalf("history = %d entries, want the cap of 12", len(got.History))
	}
	if got.History[0].Name != "C13" {
		t.Fatalf("most recent history entry = %q, want C13", got.History[0].Name)
	}
}

// Cycle deletion takes its declared capacities with it — no FK does that job.
func TestCycleDeleteRemovesCapacities(t *testing.T) {
	project := newCycleProject(t, "capacity delete project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Caps delete", "start_date": cycleDate(0), "end_date": cycleDate(7),
	}).Want(http.StatusCreated).JSON(&cycle)

	putCycleCapacities(t, cycle.ID, []map[string]any{
		{"actor_type": "member", "actor_id": testUserID, "points": 5},
	}).Want(http.StatusOK)

	testutil.Call(t, testHandler.DeleteCycle, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/api/cycles/"+cycle.ID, nil), "id", cycle.ID)).Want(http.StatusNoContent)
	if n := dbfx.Count(t, `SELECT count(*) FROM cycle_actor_capacity WHERE cycle_id = $1`, cycle.ID); n != 0 {
		t.Fatalf("capacity rows after cycle delete = %d, want 0", n)
	}
}
