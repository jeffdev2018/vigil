package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F02: last_activity_at is the run-level heartbeat. These tests pin the three
// places that stamp it and the read surface that exposes it.

func taskLastActivity(t *testing.T, taskID string) pgtype.Timestamptz {
	t.Helper()
	var ts pgtype.Timestamptz
	dbfx.QueryRow(t, `SELECT last_activity_at FROM agent_task_queue WHERE id = $1`, taskID).Scan(&ts)
	return ts
}

func progressRequest(t *testing.T, taskID string) *http.Request {
	t.Helper()
	req := testutil.JSONRequest(http.MethodPost,
		"/api/daemon/tasks/"+taskID+"/progress", map[string]any{"summary": "still here", "step": 1, "total": 3})
	req = testutil.WithURLParams(req, "taskId", taskID)
	return req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, "activity-daemon"))
}

func TestReportTaskMessagesStampsLastActivity(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "activity-messages")
	if got := taskLastActivity(t, taskID); got.Valid {
		t.Fatalf("fixture already carries last_activity_at = %v", got.Time)
	}

	before := time.Now().Add(-time.Second)
	testutil.Call(t, testHandler.ReportTaskMessages, batchMessagesRequest(t, taskID, []any{
		map[string]any{"seq": 1, "type": "text", "content": "hello"},
	})).Want(http.StatusOK)

	got := taskLastActivity(t, taskID)
	if !got.Valid || got.Time.Before(before) {
		t.Fatalf("last_activity_at after messages = %+v, want a fresh stamp", got)
	}
}

func TestReportTaskProgressStampsLastActivity(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "activity-progress")

	before := time.Now().Add(-time.Second)
	testutil.Call(t, testHandler.ReportTaskProgress, progressRequest(t, taskID)).Want(http.StatusOK)

	got := taskLastActivity(t, taskID)
	if !got.Valid || got.Time.Before(before) {
		t.Fatalf("last_activity_at after progress = %+v, want a fresh stamp", got)
	}
}

// A callback that lands after the run settled must not revive its liveness:
// the stamp is guarded by the active statuses.
func TestTouchActivityIgnoresSettledRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "activity-settled")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskID)

	if err := testHandler.Queries.TouchAgentTaskActivity(context.Background(), parseUUID(taskID)); err != nil {
		t.Fatal(err)
	}
	if got := taskLastActivity(t, taskID); got.Valid {
		t.Fatalf("settled run got last_activity_at = %v, want NULL", got.Time)
	}
}

func TestStartAgentTaskStampsLastActivity(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "activity-start")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'dispatched', started_at = NULL WHERE id = $1`, taskID)

	before := time.Now().Add(-time.Second)
	started, err := testHandler.Queries.StartAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if !started.LastActivityAt.Valid || started.LastActivityAt.Time.Before(before) {
		t.Fatalf("last_activity_at after start = %+v, want a fresh stamp", started.LastActivityAt)
	}
}

func TestListTasksByIssueExposesLastActivity(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	taskID := seedBatchTask(t, "activity-expose")
	var issueID string
	dbfx.QueryRow(t, `SELECT issue_id FROM agent_task_queue WHERE id = $1`, taskID).Scan(&issueID)

	// Absent before any callback, so older rows keep the pre-F02 shape.
	var runs []AgentTaskResponse
	testutil.Call(t, testHandler.ListTasksByIssue, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/task-runs", nil), "id", issueID,
	)).Want(http.StatusOK).JSON(&runs)
	if len(runs) != 1 || runs[0].ID != taskID {
		t.Fatalf("runs = %+v, want the seeded run", runs)
	}
	if runs[0].LastActivityAt != nil {
		t.Fatalf("last_activity_at before any callback = %q, want omitted", *runs[0].LastActivityAt)
	}

	testutil.Call(t, testHandler.ReportTaskProgress, progressRequest(t, taskID)).Want(http.StatusOK)

	runs = nil
	testutil.Call(t, testHandler.ListTasksByIssue, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/task-runs", nil), "id", issueID,
	)).Want(http.StatusOK).JSON(&runs)
	if len(runs) != 1 || runs[0].LastActivityAt == nil {
		t.Fatalf("runs after progress = %+v, want last_activity_at exposed", runs)
	}
	if _, err := time.Parse(time.RFC3339Nano, *runs[0].LastActivityAt); err != nil {
		t.Fatalf("last_activity_at = %q is not RFC3339: %v", *runs[0].LastActivityAt, err)
	}
}

// Compile-time proof the response carries the column with the documented name.
var _ = db.AgentTaskQueue{}.LastActivityAt
