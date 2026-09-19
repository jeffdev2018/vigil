package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type recurrenceEnvelope struct {
	Recurrence  IssueRecurrenceResponse     `json:"recurrence"`
	Source      map[string]any              `json:"source"`
	Occurrences []IssueRecurrenceOccurrence `json:"occurrences"`
	NextRuns    []string                    `json:"next_runs"`
}

func recurrenceCall(t *testing.T, fn http.HandlerFunc, method, issue string, body any, want int) recurrenceEnvelope {
	t.Helper()
	var out recurrenceEnvelope
	res := testutil.Call(t, fn, testutil.WithURLParams(newRequest(method, "/api/issues/"+issue+"/recurrence", body), "id", issue)).Want(want)
	if want < 300 && method != http.MethodDelete {
		res.JSON(&out)
	}
	return out
}

func TestIssueRecurrenceSpawnsCopiesOfTheLatestOccurrence(t *testing.T) {
	agent := dbfx.Agent(t, "recurring agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	label := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": testWorkspaceID, "name": "monthly-" + uuid.NewString()[:6], "color": "#123456"})
	source := dbfx.Issue(t, "Clôture mensuelle", testutil.Cols{
		"description": "Steps:\n- [x] export\n- [ ] review", "priority": "high", "status": "in_progress",
		"assignee_type": "agent", "assignee_id": agent, "properties": `{"team":"finance"}`, "acceptance_criteria": `[{"id":"c1","text":"ledger balanced","status":"pending"}]`,
		"due_date": time.Now().Add(72 * time.Hour).Format("2006-01-02"),
	})
	dbfx.Exec(t, `INSERT INTO issue_to_label (issue_id, label_id) VALUES ($1, $2)`, source, label)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_to_label WHERE label_id = $1`, label)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE origin_type = 'recurrence' AND workspace_id = $1`, testWorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_recurrence WHERE issue_id = $1`, source)
	})
	// Validation: bad mode, missing cron, invalid cron.
	recurrenceCall(t, testHandler.SetIssueRecurrence, http.MethodPut, source, map[string]any{"mode": "weekly"}, http.StatusBadRequest)
	recurrenceCall(t, testHandler.SetIssueRecurrence, http.MethodPut, source, map[string]any{"mode": "schedule"}, http.StatusBadRequest)
	recurrenceCall(t, testHandler.SetIssueRecurrence, http.MethodPut, source, map[string]any{"cron_expression": "monthly"}, http.StatusBadRequest)
	// Nothing yet.
	recurrenceCall(t, testHandler.GetIssueRecurrence, http.MethodGet, source, nil, http.StatusNotFound)
	// Set: the first of the month at 9, Paris.
	out := recurrenceCall(t, testHandler.SetIssueRecurrence, http.MethodPut, source, map[string]any{"cron_expression": "0 9 1 * *", "timezone": "Europe/Paris"}, http.StatusOK)
	if out.Recurrence.Mode != "schedule" || !out.Recurrence.Enabled || out.Recurrence.NextRunAt == nil || len(out.NextRuns) != 3 || out.Recurrence.OccurrenceCount != 0 {
		t.Fatalf("set = %+v next=%v", out.Recurrence, out.NextRuns)
	}
	var linked *string
	if err := testPool.QueryRow(context.Background(), `SELECT recurrence_id::text FROM issue WHERE id = $1`, source).Scan(&linked); err != nil || linked == nil || *linked != out.Recurrence.ID {
		t.Fatalf("source not linked to its rule: %v %v", linked, err)
	}
	// Fixtures number issues by MAX+1 while the service allocates from the
	// workspace counter: bring the counter up to date before the service creates.
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1) WHERE id = $1`, testWorkspaceID)
	// Make it due and tick.
	dbfx.Exec(t, `UPDATE issue_recurrence SET next_run_at = now() - interval '1 minute' WHERE id = $1`, out.Recurrence.ID)
	if n := testHandler.Recurrence.Tick(context.Background()); n != 1 {
		t.Fatalf("tick created %d, want 1", n)
	}
	after := recurrenceCall(t, testHandler.GetIssueRecurrence, http.MethodGet, source, nil, http.StatusOK)
	// The series lists the source and the new occurrence.
	if after.Recurrence.OccurrenceCount != 1 || after.Recurrence.LastOccurrence == nil || len(after.Occurrences) != 2 || after.Recurrence.NextRunAt == nil {
		t.Fatalf("after tick = %+v occurrences=%d", after.Recurrence, len(after.Occurrences))
	}
	next, err := time.Parse(time.RFC3339, *after.Recurrence.NextRunAt)
	if err != nil || !next.After(time.Now()) {
		t.Errorf("next run %v not in the future", next)
	}
	occ, err := testHandler.Queries.GetIssueInWorkspace(context.Background(), db.GetIssueInWorkspaceParams{ID: parseUUID(*after.Recurrence.LastOccurrence), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	if occ.Title != "Clôture mensuelle" || occ.Status != "todo" || occ.Priority != "high" || occ.AssigneeType.String != "agent" || uuidToString(occ.AssigneeID) != agent || occ.OriginType.String != "recurrence" || uuidToString(occ.RecurrenceID) != out.Recurrence.ID {
		t.Errorf("occurrence = title %q status %q priority %q assignee %s/%s origin %q series %s", occ.Title, occ.Status, occ.Priority, occ.AssigneeType.String, uuidToString(occ.AssigneeID), occ.OriginType.String, uuidToString(occ.RecurrenceID))
	}
	if occ.Description.String != "Steps:\n- [ ] export\n- [ ] review" {
		t.Errorf("checklist not reset: %q", occ.Description.String)
	}
	if string(occ.Properties) != `{"team": "finance"}` && string(occ.Properties) != `{"team":"finance"}` {
		t.Errorf("properties = %s", occ.Properties)
	}
	if len(occ.AcceptanceCriteria) < 10 {
		t.Errorf("criteria not copied: %s", occ.AcceptanceCriteria)
	}
	if !occ.DueDate.Valid {
		t.Errorf("due date not carried")
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM issue_to_label WHERE issue_id = $1 AND label_id = $2`, uuidToString(occ.ID), label); n != 1 {
		t.Errorf("label not copied")
	}
	// The assigned agent got its run like any other new issue.
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, uuidToString(occ.ID)); n != 1 {
		t.Errorf("occurrence enqueued %d runs, want 1", n)
	}
	// A GET from the occurrence resolves the same series.
	fromOcc := recurrenceCall(t, testHandler.GetIssueRecurrence, http.MethodGet, uuidToString(occ.ID), nil, http.StatusOK)
	if fromOcc.Recurrence.ID != out.Recurrence.ID || fromOcc.Source["id"] != source {
		t.Errorf("series from occurrence = %+v", fromOcc.Recurrence)
	}
	// Switch to on_close: nothing spawns while the occurrence is open, one spawns once it is done.
	sw := recurrenceCall(t, testHandler.SetIssueRecurrence, http.MethodPut, source, map[string]any{"mode": "on_close"}, http.StatusOK)
	if sw.Recurrence.Mode != service.RecurrenceModeOnClose || sw.Recurrence.NextRunAt != nil {
		t.Fatalf("on_close = %+v", sw.Recurrence)
	}
	if n := testHandler.Recurrence.Tick(context.Background()); n != 0 {
		t.Fatalf("on_close ticked %d with the occurrence still open", n)
	}
	dbfx.Exec(t, `UPDATE issue SET status = 'done' WHERE id = $1`, uuidToString(occ.ID))
	if n := testHandler.Recurrence.Tick(context.Background()); n != 1 {
		t.Fatalf("on_close tick created %d, want 1", n)
	}
	if n := testHandler.Recurrence.Tick(context.Background()); n != 0 {
		t.Fatalf("on_close ticked again %d with the new occurrence open", n)
	}
	// Disabled rules never fire; clearing removes the links.
	recurrenceCall(t, testHandler.SetIssueRecurrence, http.MethodPut, source, map[string]any{"mode": "schedule", "cron_expression": "* * * * *", "enabled": false}, http.StatusOK)
	if n := testHandler.Recurrence.Tick(context.Background()); n != 0 {
		t.Fatalf("disabled rule ticked %d", n)
	}
	recurrenceCall(t, testHandler.DeleteIssueRecurrence, http.MethodDelete, source, nil, http.StatusNoContent)
	recurrenceCall(t, testHandler.GetIssueRecurrence, http.MethodGet, source, nil, http.StatusNotFound)
	if n := dbfx.Count(t, `SELECT count(*) FROM issue WHERE recurrence_id = $1`, out.Recurrence.ID); n != 0 {
		t.Errorf("%d issues still linked after clear", n)
	}
}
