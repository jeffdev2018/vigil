package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Dated cycles (F29). The pure past/today/future rules of the burndown series
// live in cycle_burndown_test.go; this file covers what only a database can
// answer: project scoping, capacity split, rollover, close and purge.

// cycleIDOf reads a nullable uuid column as a string, "" when NULL.
func cycleIDOf(t *testing.T, sql string, args ...any) string {
	t.Helper()
	var id *string
	dbfx.QueryRow(t, sql, args...).Scan(&id)
	if id == nil {
		return ""
	}
	return *id
}

func cycleDate(offsetDays int) string {
	return time.Now().UTC().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

// newCycleProject makes a project the fixture user leads, so project-write
// gates pass without the test restating the role model.
func newCycleProject(t *testing.T, title string) string {
	t.Helper()
	return dbfx.Insert(t, "project", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"title":        title,
		"status":       "planned",
		"priority":     "none",
		"lead_type":    "member",
		"lead_id":      testUserID,
	})
}

func createCycle(t *testing.T, body map[string]any) *testutil.Response {
	t.Helper()
	resp := testutil.Call(t, testHandler.CreateCycle, newRequest(http.MethodPost, "/api/cycles", body))
	if resp.Code == http.StatusCreated {
		var out CycleResponse
		resp.JSON(&out)
		dbfx.Cleanup(t, `DELETE FROM cycle_snapshot WHERE cycle_id = $1`, out.ID)
		dbfx.Cleanup(t, `DELETE FROM cycle WHERE id = $1`, out.ID)
	}
	return resp
}

func getCycle(t *testing.T, id string) CycleResponse {
	t.Helper()
	var out CycleResponse
	testutil.Call(t, testHandler.GetCycle, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/cycles/"+id, nil), "id", id)).Want(http.StatusOK).JSON(&out)
	return out
}

func setIssueCycle(t *testing.T, issueID, cycleID string) *testutil.Response {
	t.Helper()
	body := map[string]any{"cycle_id": cycleID}
	if cycleID == "" {
		body = map[string]any{"cycle_id": nil}
	}
	return testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(
		newRequest(http.MethodPut, "/api/issues/"+issueID, body), "id", issueID))
}

// Acceptance 1: a cycle accepts its own project's issues and refuses others.
func TestCycleAcceptsOwnProjectAndRefusesForeignIssues(t *testing.T) {
	mine := newCycleProject(t, "cycle project "+uuid.NewString()[:8])
	theirs := newCycleProject(t, "other project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": mine, "name": "Sprint 1", "start_date": cycleDate(-2), "end_date": cycleDate(5),
	}).Want(http.StatusCreated).JSON(&cycle)

	own := dbfx.Issue(t, "in the cycle", testutil.Cols{"project_id": mine})
	setIssueCycle(t, own, cycle.ID).Want(http.StatusOK)

	foreign := dbfx.Issue(t, "another project's issue", testutil.Cols{"project_id": theirs})
	body := setIssueCycle(t, foreign, cycle.ID).Want(http.StatusConflict).Map()
	if body["code"] != ErrCodeCycleProjectMismatch {
		t.Fatalf("code = %v, want %s", body["code"], ErrCodeCycleProjectMismatch)
	}
	// An issue with no project at all is the same refusal, not a silent accept.
	orphan := dbfx.Issue(t, "no project")
	setIssueCycle(t, orphan, cycle.ID).Want(http.StatusConflict)

	if got := getCycle(t, cycle.ID); got.IssueCount != 1 {
		t.Fatalf("issue_count = %d, want 1 (the refusals must not have landed)", got.IssueCount)
	}
	// Clearing is always allowed.
	setIssueCycle(t, own, "").Want(http.StatusOK)
	if got := getCycle(t, cycle.ID); got.IssueCount != 0 {
		t.Fatalf("issue_count after clear = %d, want 0", got.IssueCount)
	}
}

// Acceptance 3 + 4 + 5: done is decided by the status CATALOGUE, human and
// agent load are separate, and without a load property the unit is issues.
func TestCycleCountsCatalogueDoneAndSplitsHumanFromAgentLoad(t *testing.T) {
	project := newCycleProject(t, "capacity project "+uuid.NewString()[:8])
	dbfx.Insert(t, "issue_status", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"key":          "shipped_f29",
		"name":         "Shipped",
		"category":     "done",
		"color":        "#22c55e",
		"is_system":    false,
		"position":     40,
	})
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Capacity", "start_date": cycleDate(-1), "end_date": cycleDate(6),
		"human_capacity": 3, "agent_capacity": 1,
	}).Want(http.StatusCreated).JSON(&cycle)

	agentID := cycleIDOf(t, `SELECT id::text FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID)
	for _, spec := range []testutil.Cols{
		{"project_id": project, "cycle_id": cycle.ID, "assignee_type": "member", "assignee_id": testUserID, "status": "todo"},
		{"project_id": project, "cycle_id": cycle.ID, "assignee_type": "member", "assignee_id": testUserID, "status": "shipped_f29"},
		{"project_id": project, "cycle_id": cycle.ID, "assignee_type": "agent", "assignee_id": agentID, "status": "todo"},
		{"project_id": project, "cycle_id": cycle.ID, "status": "todo"},
	} {
		dbfx.Issue(t, "cycle load "+uuid.NewString()[:8], spec)
	}

	got := getCycle(t, cycle.ID)
	if got.IssueCount != 4 || got.DoneCount != 1 {
		t.Fatalf("counts = %d/%d, want 4/1 — a custom done-category status must count as done", got.IssueCount, got.DoneCount)
	}
	if got.LoadUnit != cycleLoadUnitIssues {
		t.Fatalf("load_unit = %q, want %q so the UI can say the unit is issues", got.LoadUnit, cycleLoadUnitIssues)
	}
	if got.Capacity.Human.Load != 2 || got.Capacity.Agent.Load != 1 || got.Capacity.UnassignedLoad != 1 {
		t.Fatalf("loads human/agent/unassigned = %v/%v/%v, want 2/1/1",
			got.Capacity.Human.Load, got.Capacity.Agent.Load, got.Capacity.UnassignedLoad)
	}
	if got.Capacity.Human.Capacity == nil || *got.Capacity.Human.Capacity != 3 ||
		got.Capacity.Agent.Capacity == nil || *got.Capacity.Agent.Capacity != 1 {
		t.Fatalf("capacities = %v/%v, want 3/1 declared separately", got.Capacity.Human.Capacity, got.Capacity.Agent.Capacity)
	}
}

// A number property makes the load unit the property's value, not a count.
func TestCycleLoadUsesTheNumberPropertyWhenOneIsChosen(t *testing.T) {
	project := newCycleProject(t, "points project "+uuid.NewString()[:8])
	prop := dbfx.Insert(t, "issue_property", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "Points " + uuid.NewString()[:8],
		"type":         "number",
		"config":       testutil.Raw("'{}'::jsonb"),
		"position":     1,
	})
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Points", "start_date": cycleDate(0), "end_date": cycleDate(7),
		"load_property_id": prop,
	}).Want(http.StatusCreated).JSON(&cycle)
	if cycle.LoadUnit != cycleLoadUnitProperty {
		t.Fatalf("load_unit = %q, want %q", cycle.LoadUnit, cycleLoadUnitProperty)
	}

	dbfx.Issue(t, "five points", testutil.Cols{
		"project_id": project, "cycle_id": cycle.ID, "assignee_type": "member", "assignee_id": testUserID,
		"properties": testutil.Raw(fmt.Sprintf(`'{"%s": 5}'::jsonb`, prop)),
	})
	// A value of the wrong jsonb type must not fail the query, only count 0.
	dbfx.Issue(t, "unusable points", testutil.Cols{
		"project_id": project, "cycle_id": cycle.ID, "assignee_type": "member", "assignee_id": testUserID,
		"properties": testutil.Raw(fmt.Sprintf(`'{"%s": "not a number"}'::jsonb`, prop)),
	})

	got := getCycle(t, cycle.ID)
	if got.Capacity.Human.Load != 5 {
		t.Fatalf("human load = %v, want 5 (the property's value, ignoring the non-numeric one)", got.Capacity.Human.Load)
	}

	// A non-number property is refused rather than silently loading nothing.
	text := dbfx.Insert(t, "issue_property", testutil.Cols{
		"workspace_id": testWorkspaceID, "name": "Note " + uuid.NewString()[:8], "type": "text",
		"config": testutil.Raw("'{}'::jsonb"), "position": 2,
	})
	createCycle(t, map[string]any{
		"project_id": project, "name": "Bad unit", "start_date": cycleDate(0), "end_date": cycleDate(7),
		"load_property_id": text,
	}).Want(http.StatusBadRequest)
}

// Acceptance 2 + 8: the burndown has one entry per day with no gaps, and
// closing freezes it — a later snapshot pass must not extend it.
func TestCycleBurndownIsGaplessAndClosingFreezesIt(t *testing.T) {
	project := newCycleProject(t, "burndown project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Burndown", "start_date": cycleDate(-3), "end_date": cycleDate(3),
	}).Want(http.StatusCreated).JSON(&cycle)
	dbfx.Issue(t, "burn me", testutil.Cols{"project_id": project, "cycle_id": cycle.ID})
	dbfx.Issue(t, "burn me too", testutil.Cols{"project_id": project, "cycle_id": cycle.ID, "status": "done"})

	// One historical day, deliberately NOT contiguous with today, so the
	// carry-forward rule is what fills the gap between them.
	dbfx.Exec(t, `INSERT INTO cycle_snapshot (cycle_id, snapshot_date, workspace_id, total_count, done_count, total_load, done_load, human_load, agent_load)
	              VALUES ($1, $2::date, $3, 2, 0, 2, 0, 0, 0)`, cycle.ID, cycleDate(-3), testWorkspaceID)

	var burn CycleBurndownResponse
	testutil.Call(t, testHandler.GetCycleBurndown, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/cycles/"+cycle.ID+"/burndown", nil), "id", cycle.ID)).Want(http.StatusOK).JSON(&burn)

	if len(burn.Days) != 7 {
		t.Fatalf("days = %d, want 7 (start..end inclusive)", len(burn.Days))
	}
	for i, d := range burn.Days {
		if d.Date != cycleDate(i-3) {
			t.Fatalf("day %d = %s, want %s — the series must have no gaps", i, d.Date, cycleDate(i-3))
		}
	}
	if burn.Days[0].RemainingCount == nil || *burn.Days[0].RemainingCount != 2 {
		t.Fatalf("first day remaining = %v, want the snapshot's 2", burn.Days[0].RemainingCount)
	}
	// The day between the snapshot and today has no row of its own: it carries
	// the last observation forward rather than reading as zero.
	if burn.Days[1].RemainingCount == nil || *burn.Days[1].RemainingCount != 2 {
		t.Fatalf("carried day = %v, want 2", burn.Days[1].RemainingCount)
	}
	if burn.Days[3].RemainingCount == nil || *burn.Days[3].RemainingCount != 1 {
		t.Fatalf("today remaining = %v, want the live 1", burn.Days[3].RemainingCount)
	}
	if burn.Days[4].RemainingCount != nil {
		t.Fatalf("future remaining = %v, want null — nothing has happened yet", *burn.Days[4].RemainingCount)
	}
	if burn.Days[len(burn.Days)-1].IdealCount != 0 {
		t.Fatalf("ideal on the last day = %v, want 0", burn.Days[len(burn.Days)-1].IdealCount)
	}

	testutil.Call(t, testHandler.CloseCycle, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/cycles/"+cycle.ID+"/close", nil), "id", cycle.ID)).Want(http.StatusOK)

	frozen := dbfx.Count(t, `SELECT count(*) FROM cycle_snapshot WHERE cycle_id = $1`, cycle.ID)
	if _, err := testHandler.SnapshotCycles(context.Background()); err != nil {
		t.Fatalf("snapshot pass: %v", err)
	}
	if after := dbfx.Count(t, `SELECT count(*) FROM cycle_snapshot WHERE cycle_id = $1`, cycle.ID); after != frozen {
		t.Fatalf("snapshots after close = %d, want %d frozen", after, frozen)
	}
	// Closing twice is refused, so a second call cannot roll work over again.
	testutil.Call(t, testHandler.CloseCycle, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/cycles/"+cycle.ID+"/close", nil), "id", cycle.ID)).Want(http.StatusConflict)
}

// The snapshot job is idempotent on (cycle_id, snapshot_date).
func TestCycleSnapshotJobIsIdempotentPerDay(t *testing.T) {
	project := newCycleProject(t, "snapshot project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Snap", "start_date": cycleDate(-1), "end_date": cycleDate(4),
	}).Want(http.StatusCreated).JSON(&cycle)
	dbfx.Issue(t, "snapshot me", testutil.Cols{"project_id": project, "cycle_id": cycle.ID})

	for i := 0; i < 3; i++ {
		if _, err := testHandler.SnapshotCycles(context.Background()); err != nil {
			t.Fatalf("snapshot pass %d: %v", i, err)
		}
	}
	rows := dbfx.Count(t, `SELECT count(*) FROM cycle_snapshot WHERE cycle_id = $1`, cycle.ID)
	if rows != 1 {
		t.Fatalf("snapshot rows = %d, want 1 — three passes on one day is one row", rows)
	}
	var total, done int64
	dbfx.QueryRow(t, `SELECT total_count, done_count FROM cycle_snapshot WHERE cycle_id = $1`, cycle.ID).Scan(&total, &done)
	if total != 1 || done != 0 {
		t.Fatalf("snapshot = %d/%d, want 1/0", total, done)
	}
}

// Acceptance 6: rollover moves unfinished work to the next cycle and journals
// it; finished work stays where it was.
func TestCycleRolloverMovesUnfinishedWorkAndJournalsIt(t *testing.T) {
	project := newCycleProject(t, "rollover project "+uuid.NewString()[:8])
	var ended, next CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Ended", "start_date": cycleDate(-10), "end_date": cycleDate(-2),
	}).Want(http.StatusCreated).JSON(&ended)
	createCycle(t, map[string]any{
		"project_id": project, "name": "Next", "start_date": cycleDate(1), "end_date": cycleDate(8),
	}).Want(http.StatusCreated).JSON(&next)

	open := dbfx.Issue(t, "still open", testutil.Cols{"project_id": project, "cycle_id": ended.ID})
	finished := dbfx.Issue(t, "finished", testutil.Cols{"project_id": project, "cycle_id": ended.ID, "status": "done"})

	if _, err := testHandler.RolloverCycles(context.Background()); err != nil {
		t.Fatalf("rollover: %v", err)
	}
	if got := cycleIDOf(t, `SELECT cycle_id::text FROM issue WHERE id = $1`, open); got != next.ID {
		t.Fatalf("open issue cycle = %s, want the next cycle %s", got, next.ID)
	}
	if got := cycleIDOf(t, `SELECT cycle_id::text FROM issue WHERE id = $1`, finished); got != ended.ID {
		t.Fatalf("finished issue cycle = %s, want to stay on %s", got, ended.ID)
	}
	journal := dbfx.Count(t, `SELECT count(*) FROM activity_log WHERE issue_id = $1 AND action = $2`, open, activityCycleRolledOver)
	if journal != 1 {
		t.Fatalf("journal rows = %d, want 1 %s entry", journal, activityCycleRolledOver)
	}
	if dbfx.Count(t, `SELECT count(*) FROM cycle WHERE id = $1 AND closed_at IS NOT NULL`, ended.ID) != 1 {
		t.Fatal("the ended cycle stayed open after its rollover")
	}
	// A second pass finds nothing to do: the cycle is closed.
	if _, err := testHandler.RolloverCycles(context.Background()); err != nil {
		t.Fatalf("second rollover: %v", err)
	}
	if got := cycleIDOf(t, `SELECT cycle_id::text FROM issue WHERE id = $1`, open); got != next.ID {
		t.Fatalf("issue moved twice: cycle = %s", got)
	}
}

// Acceptance 7: with no next cycle the work is detached and the project lead
// is told, rather than left attached to a plan that is over.
func TestCycleRolloverWithoutNextCycleOrphansAndNotifiesTheLead(t *testing.T) {
	project := newCycleProject(t, "orphan project "+uuid.NewString()[:8])
	var ended CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Last", "start_date": cycleDate(-9), "end_date": cycleDate(-1),
	}).Want(http.StatusCreated).JSON(&ended)
	open := dbfx.Issue(t, "nowhere to go", testutil.Cols{"project_id": project, "cycle_id": ended.ID})
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = $2 AND details->>'cycle_id' = $3`, testWorkspaceID, inboxTypeCycleOrphaned, ended.ID)

	if _, err := testHandler.RolloverCycles(context.Background()); err != nil {
		t.Fatalf("rollover: %v", err)
	}
	var cycleID *string
	dbfx.QueryRow(t, `SELECT cycle_id::text FROM issue WHERE id = $1`, open).Scan(&cycleID)
	if cycleID != nil {
		t.Fatalf("issue cycle = %v, want NULL", *cycleID)
	}
	// Scoped to THIS cycle: other tests in the package legitimately orphan
	// their own work, and a workspace-wide count would drift with them.
	inbox := dbfx.Count(t,
		`SELECT count(*) FROM inbox_item
		  WHERE workspace_id = $1 AND type = $2 AND recipient_id = $3
		    AND details->>'cycle_id' = $4`,
		testWorkspaceID, inboxTypeCycleOrphaned, testUserID, ended.ID)
	if inbox != 1 {
		t.Fatalf("inbox items for the lead = %d, want 1", inbox)
	}
}

// Acceptance 9: a goal aggregates progress across the projects it links.
func TestGoalProgressAggregatesAcrossItsProjects(t *testing.T) {
	a := newCycleProject(t, "goal project a "+uuid.NewString()[:8])
	b := newCycleProject(t, "goal project b "+uuid.NewString()[:8])
	goal := dbfx.Insert(t, "goal", testutil.Cols{
		"id": uuid.NewString(), "workspace_id": testWorkspaceID,
		"title": "Initiative " + uuid.NewString()[:8], "status": "draft",
	})
	for _, p := range []string{a, b} {
		dbfx.InsertNoID(t, "project_goal",
			testutil.Cols{"workspace_id": testWorkspaceID, "project_id": p, "goal_id": goal},
			"project_id = $1 AND goal_id = $2", p, goal)
	}
	dbfx.Issue(t, "a open", testutil.Cols{"project_id": a})
	dbfx.Issue(t, "a done", testutil.Cols{"project_id": a, "status": "done"})
	dbfx.Issue(t, "b open", testutil.Cols{"project_id": b})

	var out struct {
		Projects   []GoalProjectProgress `json:"projects"`
		TotalCount int64                 `json:"total_count"`
		DoneCount  int64                 `json:"done_count"`
	}
	testutil.Call(t, testHandler.GetGoalProgress, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/goals/"+goal+"/progress", nil), "id", goal)).Want(http.StatusOK).JSON(&out)

	if len(out.Projects) != 2 {
		t.Fatalf("projects = %d, want one row per linked project", len(out.Projects))
	}
	if out.TotalCount != 3 || out.DoneCount != 1 {
		t.Fatalf("aggregate = %d/%d, want 3/1", out.TotalCount, out.DoneCount)
	}
	for _, p := range out.Projects {
		if p.Name == "" {
			t.Fatalf("project %s has no name; the section cannot render it", p.ProjectID)
		}
	}
}

// Acceptance 11: deleting a project takes its cycles, their history, their
// saved views and the issues' membership with it — and leaves the issues.
func TestDeleteProjectCleansItsCycles(t *testing.T) {
	project := newCycleProject(t, "doomed project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Doomed", "start_date": cycleDate(-1), "end_date": cycleDate(4),
	}).Want(http.StatusCreated).JSON(&cycle)
	issue := dbfx.Issue(t, "survives its project", testutil.Cols{"project_id": project, "cycle_id": cycle.ID})
	if _, err := testHandler.SnapshotCycles(context.Background()); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	view := dbfx.Insert(t, "issue_view", testutil.Cols{
		"workspace_id": testWorkspaceID, "owner_id": testUserID, "name": "Cycle view",
		"scope_type": "cycle", "scope_id": cycle.ID,
		"query": testutil.Raw("'{}'::jsonb"),
	})

	testutil.Call(t, testHandler.DeleteProject, testutil.WithURLParams(
		newRequest(http.MethodDelete, "/api/projects/"+project, nil), "id", project)).Want(http.StatusNoContent)

	if n := dbfx.Count(t, `SELECT count(*) FROM cycle WHERE id = $1`, cycle.ID); n != 0 {
		t.Fatal("the project's cycle outlived it")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM cycle_snapshot WHERE cycle_id = $1`, cycle.ID); n != 0 {
		t.Fatal("the cycle's snapshots outlived their cycle")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_view WHERE id = $1`, view); n != 0 {
		t.Fatal("a cycle-scoped view outlived its cycle and is now unreachable")
	}
	var cycleID *string
	dbfx.QueryRow(t, `SELECT cycle_id::text FROM issue WHERE id = $1`, issue).Scan(&cycleID)
	if cycleID != nil {
		t.Fatalf("issue still points at the deleted cycle %v", *cycleID)
	}
}

// The cycle scope's issue surface is a `?cycle_id=` list, so the filter has to
// exist on both list paths.
func TestListIssuesFiltersByCycle(t *testing.T) {
	project := newCycleProject(t, "filter project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Filter", "start_date": cycleDate(0), "end_date": cycleDate(6),
	}).Want(http.StatusCreated).JSON(&cycle)
	inCycle := dbfx.Issue(t, "in cycle", testutil.Cols{"project_id": project, "cycle_id": cycle.ID})
	dbfx.Issue(t, "out of cycle", testutil.Cols{"project_id": project})

	for _, path := range []string{
		"/api/issues?cycle_id=" + cycle.ID,
		"/api/issues?open_only=true&cycle_id=" + cycle.ID,
	} {
		var out struct {
			Issues []IssueResponse `json:"issues"`
		}
		testutil.Call(t, testHandler.ListIssues, newRequest(http.MethodGet, path, nil)).Want(http.StatusOK).JSON(&out)
		if len(out.Issues) != 1 || out.Issues[0].ID != inCycle {
			t.Fatalf("%s returned %d issues, want only the one in the cycle", path, len(out.Issues))
		}
		if out.Issues[0].CycleID == nil || *out.Issues[0].CycleID != cycle.ID {
			t.Fatalf("%s: cycle_id missing from the row", path)
		}
	}
}

// The batch toolbar plans a selection into a cycle. Issues outside the cycle's
// project come back refused rather than aborting the whole batch.
func TestBatchUpdateIssuesSetsCycleAndRefusesForeignProjects(t *testing.T) {
	project := newCycleProject(t, "batch project "+uuid.NewString()[:8])
	other := newCycleProject(t, "batch other "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Batch", "start_date": cycleDate(0), "end_date": cycleDate(6),
	}).Want(http.StatusCreated).JSON(&cycle)
	mine := dbfx.Issue(t, "batch mine", testutil.Cols{"project_id": project})
	foreign := dbfx.Issue(t, "batch foreign", testutil.Cols{"project_id": other})

	var out struct {
		Updated int              `json:"updated"`
		Refused []map[string]any `json:"refused"`
	}
	testutil.Call(t, testHandler.BatchUpdateIssues, newRequest(http.MethodPatch, "/api/issues/batch", map[string]any{
		"issue_ids": []string{mine, foreign},
		"updates":   map[string]any{"cycle_id": cycle.ID},
	})).Want(http.StatusOK).JSON(&out)

	if out.Updated != 1 || len(out.Refused) != 1 || out.Refused[0]["code"] != ErrCodeCycleProjectMismatch {
		t.Fatalf("updated=%d refused=%v, want one applied and one %s", out.Updated, out.Refused, ErrCodeCycleProjectMismatch)
	}
	if got := cycleIDOf(t, `SELECT cycle_id::text FROM issue WHERE id = $1`, mine); got != cycle.ID {
		t.Fatalf("mine cycle = %s, want %s", got, cycle.ID)
	}
	var foreignCycle *string
	dbfx.QueryRow(t, `SELECT cycle_id::text FROM issue WHERE id = $1`, foreign).Scan(&foreignCycle)
	if foreignCycle != nil {
		t.Fatalf("refused issue got the cycle anyway: %v", *foreignCycle)
	}
}

// Acceptance 12 in its narrowest form: another workspace's cycle is a 404, not
// a readable row.
func TestCycleIsScopedToItsWorkspace(t *testing.T) {
	project := newCycleProject(t, "scope project "+uuid.NewString()[:8])
	var cycle CycleResponse
	createCycle(t, map[string]any{
		"project_id": project, "name": "Scoped", "start_date": cycleDate(0), "end_date": cycleDate(3),
	}).Want(http.StatusCreated).JSON(&cycle)

	otherUser := dbfx.User(t, "cycle outsider", "cycle-outsider-"+uuid.NewString()[:8]+"@example.com")
	otherWs := dbfx.Workspace(t, "Other cycles", "other-cycles-"+uuid.NewString()[:8])
	dbfx.Member(t, otherWs, otherUser, "owner")

	req := testutil.WithHeaders(newRequest(http.MethodGet, "/api/cycles/"+cycle.ID, nil),
		"X-User-ID", otherUser, "X-Workspace-ID", otherWs)
	testutil.Call(t, testHandler.GetCycle, testutil.WithURLParams(req, "id", cycle.ID)).Want(http.StatusNotFound)

	var list struct {
		Cycles []CycleResponse `json:"cycles"`
	}
	testutil.Call(t, testHandler.ListCycles, testutil.WithHeaders(
		newRequest(http.MethodGet, "/api/cycles", nil), "X-User-ID", otherUser, "X-Workspace-ID", otherWs),
	).Want(http.StatusOK).JSON(&list)
	for _, c := range list.Cycles {
		if c.ID == cycle.ID {
			t.Fatal("another workspace's cycle leaked into the list")
		}
	}
}
