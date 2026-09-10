package handler

import (
	"context"
	"testing"
)

// The native runtime's doctrine tool files the same row a person would: the
// reporter is the agent, and the report binds to the run's task and, through
// it, to the task's issue.
func TestDoctrineToolAdapterFilesReportBoundToTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	issueID, taskID, agentID := runningAgentRun(t, "doctrine tool")
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace_doctrine_report WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
	})

	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	agent, err := testHandler.Queries.GetAgent(ctx, parseUUID(agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}

	adapter := doctrineToolAdapter{h: testHandler}
	reportID, err := adapter.Report(ctx, task, agent, "conflict", "Rule 3 forbids force-pushing but the task asks for it.", "Never force-push a shared branch.")
	if err != nil {
		t.Fatalf("file report: %v", err)
	}
	if reportID == "" {
		t.Fatal("adapter returned no report id")
	}

	var kind, reporterType, reporterID, gotTask, gotIssue, passage string
	dbfx.QueryRow(t, `SELECT kind, reporter_type, reporter_id::text, task_id::text, COALESCE(issue_id::text, ''), passage
		FROM workspace_doctrine_report WHERE id = $1`, reportID).
		Scan(&kind, &reporterType, &reporterID, &gotTask, &gotIssue, &passage)
	if kind != "conflict" || reporterType != "agent" || reporterID != agentID {
		t.Errorf("report = kind %q reporter %s/%s, want conflict agent/%s", kind, reporterType, reporterID, agentID)
	}
	if gotTask != taskID {
		t.Errorf("report task_id = %s, want %s", gotTask, taskID)
	}
	if gotIssue != issueID {
		t.Errorf("report issue_id = %s, want the task's issue %s", gotIssue, issueID)
	}
	if passage != "Never force-push a shared branch." {
		t.Errorf("report passage = %q", passage)
	}

	// A kind the API refuses comes back as its error, not a silent success.
	if _, err := adapter.Report(ctx, task, agent, "vibes", "nope", ""); err == nil {
		t.Error("an invalid kind must be refused")
	}
}
