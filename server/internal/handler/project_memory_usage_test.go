package handler

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestProjectMemoryUsageCoverageVersionsAndPrivacy(t *testing.T) {
	project := dbfx.Project(t, "Project memory usage")
	otherProject := dbfx.Project(t, "Other project memory usage")
	agent := agentMemoryFixture(t, "Project memory usage agent")
	runtime := dbfx.Runtime(t, "Project memory usage runtime")
	now := time.Now().UTC().Truncate(time.Microsecond)
	dispatch := now.Add(-2 * time.Hour)
	started := now.Add(-time.Hour)
	issue := dbfx.Issue(t, "Usage issue", testutil.Cols{"project_id": project})
	foreignIssue := dbfx.Issue(t, "Foreign project issue", testutil.Cols{"project_id": otherProject})

	receipt := func(projectVersion map[string]any) string {
		data, err := json.Marshal(map[string]any{
			"dispatched_at": dispatch.Format(time.RFC3339Nano), "agent_status": "loaded",
			"agent_versions": []any{}, "project_version": projectVersion, "is_chat": false,
		})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	version := func(id string, revision int) map[string]any {
		return map[string]any{"id": id, "revision": revision}
	}
	task := func(issueID string, context any, overrides ...testutil.Cols) string {
		cols := testutil.Cols{
			"runtime_id": runtime, "issue_id": issueID, "status": "completed",
			"dispatched_at": dispatch, "started_at": started, "completed_at": now.Add(-30 * time.Minute),
			"memory_context": context, "trigger_evidence_kind": "issue_assignment",
		}
		for _, over := range overrides {
			for k, v := range over {
				cols[k] = v
			}
		}
		return dbfx.Task(t, agent, cols)
	}

	task(issue, receipt(version(project, 1)))
	task(issue, receipt(version(project, 1)))
	task(issue, receipt(version(project, 2)))
	task(issue, receipt(nil))
	task(issue, nil)
	task(issue, receipt(version(project, 1)), testutil.Cols{"dispatched_at": dispatch.Add(time.Second)})
	task(issue, `{"agent_status":"loaded","agent_versions":{}}`)
	task(issue, nil, testutil.Cols{"trigger_evidence_kind": nil})
	task(issue, receipt(version(project, 1)), testutil.Cols{"status": "queued", "started_at": nil, "completed_at": nil})
	task(issue, receipt(version(project, 1)), testutil.Cols{"started_at": now.Add(-31 * 24 * time.Hour)})
	task(issue, receipt(version(project, 1)), testutil.Cols{"chat_session_id": dbfx.ChatSession(t, agent)})
	task(foreignIssue, receipt(version(otherProject, 1)))

	req := func() *http.Request {
		return projectMemoryRequest("GET", project, nil)
	}
	var result ProjectMemoryUsageResponse
	testutil.Call(t, testHandler.GetProjectMemoryUsage, withURLParam(req(), "id", project)).Want(http.StatusOK).JSON(&result)
	if result.StartedRuns != 7 || result.RecordedRuns != 4 || result.UnrecordedRuns != 3 || result.RunsWithProjectMemory != 3 {
		t.Fatalf("misleading coverage: %+v", result)
	}
	since, err := time.Parse(time.RFC3339Nano, result.Since)
	if err != nil {
		t.Fatal(err)
	}
	until, err := time.Parse(time.RFC3339Nano, result.Until)
	if err != nil {
		t.Fatal(err)
	}
	if until.Sub(since) != 30*24*time.Hour {
		t.Fatalf("wrong window: %+v", result)
	}
	if len(result.Versions) != 2 {
		t.Fatalf("wrong versions: %+v", result.Versions)
	}
	for _, row := range result.Versions {
		if row.ProjectID != project {
			t.Fatalf("foreign project leaked: %+v", row)
		}
		expected := int64(1)
		if row.Revision == 1 {
			expected = 2
		}
		if row.PreparedRuns != expected {
			t.Fatalf("wrong per-version count: %+v", row)
		}
		date, err := time.Parse(time.RFC3339Nano, row.LastStartedAt)
		if err != nil || !date.Equal(started) {
			t.Fatalf("wrong last start: %+v %v", row, err)
		}
	}

	foreign := withURLParam(projectMemoryRequest("GET", project, nil), "id", project)
	foreign.Header.Set("X-Workspace-ID", uuid.NewString())
	testutil.Call(t, testHandler.GetProjectMemoryUsage, foreign).Want(http.StatusNotFound)

	var empty ProjectMemoryUsageResponse
	emptyProject := dbfx.Project(t, "Unused project memory")
	testutil.Call(t, testHandler.GetProjectMemoryUsage, withURLParam(projectMemoryRequest("GET", emptyProject, nil), "id", emptyProject)).
		Want(http.StatusOK).JSON(&empty)
	if empty.StartedRuns != 0 || empty.Versions == nil || len(empty.Versions) != 0 {
		t.Fatalf("empty became unknown: %+v", empty)
	}
}
