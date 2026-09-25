package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Code health autopilot (K22): a scheduled read-only scan reports maintenance
// opportunities in one fenced block; the server keeps the findings it trusts
// and opens one maintenance issue per finding, assigned to the same agent.
//
// The settings shape itself (defaults, clamping, cron due-ness) is covered
// canonically in service/code_health_test.go; this file covers the endpoints,
// the completion hook and what lands on the board.

type codeHealthSettingsEnvelope struct {
	service.CodeHealthSettings
	MinIssuesAllowed int `json:"min_issues_allowed"`
	MaxIssuesAllowed int `json:"max_issues_allowed"`
}

type codeHealthScanEnvelope struct {
	Scan CodeHealthScanResponse `json:"scan"`
}

type codeHealthScanListEnvelope struct {
	Scans []CodeHealthScanResponse `json:"scans"`
}

// codeHealthCleanup removes everything a scan leaves behind: the scan rows,
// the housekeeping and maintenance issues, and the runs hanging off them.
func codeHealthCleanup(t *testing.T) {
	t.Helper()
	// IssueService.Create numbers from workspace.issue_counter, which dbfx.Issue
	// deliberately does not move: without this the issues opened here collide
	// on uq_issue_workspace_number (see org_test.go).
	syncIssueCounter(t)
	t.Cleanup(func() {
		// NOT context.Background(): Go cancels it just before cleanups run.
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM code_health_scan WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1 AND origin_type = 'code_health')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue_to_label WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1 AND origin_type = 'code_health')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND origin_type = 'code_health'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue_label WHERE workspace_id = $1 AND name = 'maintenance'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'code_health_report'`, testWorkspaceID)
		testPool.Exec(ctx, `UPDATE workspace SET settings = settings - 'code_health' WHERE id = $1`, testWorkspaceID)
	})
}

func putCodeHealthSettings(t *testing.T, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.PutCodeHealthSettings, newRequest(http.MethodPut, "/api/code-health/settings", body))
}

func triggerCodeHealthScan(t *testing.T) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.TriggerCodeHealthScan, newRequest(http.MethodPost, "/api/code-health/scans/trigger", nil))
}

// codeHealthAgent returns an agent of the test workspace usable as the
// maintenance agent.
func codeHealthAgent(t *testing.T) string {
	t.Helper()
	return dbfx.Agent(t, "maintenance agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
}

func TestCodeHealthSettingsAreAdminOnlyAndValidated(t *testing.T) {
	codeHealthCleanup(t)
	agentID := codeHealthAgent(t)

	// Disabled by default, with the documented defaults.
	var out codeHealthSettingsEnvelope
	testutil.Call(t, testHandler.GetCodeHealthSettings, newRequest(http.MethodGet, "/api/code-health/settings", nil)).
		Want(http.StatusOK).JSON(&out)
	if out.Enabled || out.Cron != service.CodeHealthDefaultCron || out.MaxIssuesPerScan != service.CodeHealthDefaultMaxIssues || out.MinConfidence != service.CodeHealthDefaultMinConfidence {
		t.Fatalf("defaults: %+v", out)
	}

	// A plain member reads the settings but cannot write them.
	member := dbfx.User(t, "code health member", "code-health-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "member")
	testutil.Call(t, testHandler.GetCodeHealthSettings, newRequestAs(member, http.MethodGet, "/api/code-health/settings", nil)).Want(http.StatusOK)
	testutil.Call(t, testHandler.PutCodeHealthSettings, newRequestAs(member, http.MethodPut, "/api/code-health/settings",
		map[string]any{"enabled": true, "agent_id": agentID})).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.TriggerCodeHealthScan, newRequestAs(member, http.MethodPost, "/api/code-health/scans/trigger", nil)).Want(http.StatusForbidden)

	// Validation: cron, timezone, bounds, and the agent must be this
	// workspace's.
	putCodeHealthSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "cron": "not a cron"}).Want(http.StatusBadRequest)
	putCodeHealthSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "timezone": "Mars/Olympus"}).Want(http.StatusBadRequest)
	putCodeHealthSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "max_issues_per_scan": 99}).Want(http.StatusBadRequest)
	putCodeHealthSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "min_confidence": 101}).Want(http.StatusBadRequest)
	putCodeHealthSettings(t, map[string]any{"enabled": true}).Want(http.StatusBadRequest)
	putCodeHealthSettings(t, map[string]any{"enabled": true, "agent_id": uuid.NewString()}).Want(http.StatusBadRequest)

	putCodeHealthSettings(t, map[string]any{
		"enabled": true, "agent_id": agentID, "cron": "0 4 * * 2", "timezone": "Europe/Paris",
		"max_issues_per_scan": 3, "min_confidence": 60,
	}).Want(http.StatusOK).JSON(&out)
	if !out.Enabled || out.Cron != "0 4 * * 2" || out.MaxIssuesPerScan != 3 || out.MinConfidence != 60 || out.AgentID != agentID {
		t.Fatalf("saved settings: %+v", out)
	}
	// The cron anchor is server-owned and stamped on enable, so the schedule
	// cannot be made to fire immediately by a client.
	if out.EnabledAt.IsZero() {
		t.Fatalf("enabling stamps the cron anchor: %+v", out)
	}
}

func TestCodeHealthScanTriggerOpensOneIssuePerKeptFinding(t *testing.T) {
	codeHealthCleanup(t)
	agentID := codeHealthAgent(t)
	putCodeHealthSettings(t, map[string]any{
		"enabled": false, "agent_id": agentID, "max_issues_per_scan": 5, "min_confidence": 70,
	}).Want(http.StatusOK)

	// A manual scan is allowed even with the schedule off.
	var started codeHealthScanEnvelope
	triggerCodeHealthScan(t).Want(http.StatusCreated).JSON(&started)
	if started.Scan.Status != "running" || started.Scan.AgentID != agentID || started.Scan.TaskID == "" {
		t.Fatalf("scan started: %+v", started.Scan)
	}
	// The run is queued against the housekeeping issue, and its brief is the
	// read-only one.
	var brief, issueOrigin string
	dbfx.QueryRow(t, `SELECT COALESCE(a.handoff_note, ''), COALESCE(i.origin_type, '') FROM agent_task_queue a JOIN issue i ON i.id = a.issue_id WHERE a.id = $1`,
		started.Scan.TaskID).Scan(&brief, &issueOrigin)
	if issueOrigin != "code_health" {
		t.Fatalf("the scan run hangs off the housekeeping issue, got origin %q", issueOrigin)
	}
	if !strings.Contains(brief, "code_health_findings") || !strings.Contains(brief, "FORBIDDEN") {
		t.Fatalf("the brief carries the read-only contract: %q", brief)
	}

	// One scan at a time.
	triggerCodeHealthScan(t).Want(http.StatusConflict)

	// An issue this autopilot already opened and nobody closed is not opened
	// again, whatever the agent reports.
	dupTitle := "Drop the dead migration runner"
	dbfx.Issue(t, codeHealthIssuePrefix+dupTitle, testutil.Cols{
		"origin_type": "code_health", "origin_id": started.Scan.ID,
	})
	syncIssueCounter(t)

	output := "Scan done.\n\n```code_health_findings\n" + `{"findings":[
      {"kind":"tests","title":"Cover the claim fence","summary":"The claim fence has no test.","paths":["server/internal/service/task.go"],"confidence":90,"effort":"M","evidence":"go test lists no case for it"},
      {"kind":"debt","title":"Maybe split the router","summary":"The router is long.","confidence":40,"effort":"L","evidence":"it is 3000 lines"},
      {"kind":"debt","title":"` + dupTitle + `","summary":"Already tracked.","confidence":95,"effort":"S","evidence":"dead code"}
    ],"nothing_found":""}` + "\n```\n"

	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, started.Scan.TaskID)
	if w := completeTaskViaHandler(t, started.Scan.TaskID, output); w.Code != http.StatusOK {
		t.Fatalf("complete the scan run: %d %s", w.Code, w.Body.String())
	}

	var list codeHealthScanListEnvelope
	testutil.Call(t, testHandler.ListCodeHealthScans, newRequest(http.MethodGet, "/api/code-health/scans", nil)).
		Want(http.StatusOK).JSON(&list)
	if len(list.Scans) != 1 {
		t.Fatalf("one scan in the history, got %d", len(list.Scans))
	}
	scan := list.Scans[0]
	if scan.Status != "completed" || scan.IssuesCreated != 1 {
		t.Fatalf("one kept finding out of three: %+v", scan)
	}
	if len(scan.Findings) != 3 {
		t.Fatalf("every reported finding stays on the record: %+v", scan.Findings)
	}
	skipped := map[string]string{}
	for _, f := range scan.Findings {
		skipped[f.Title] = f.Skipped
	}
	if skipped["Cover the claim fence"] != "" {
		t.Fatalf("the confident, new finding is kept: %+v", scan.Findings)
	}
	if skipped["Maybe split the router"] != "low_confidence" {
		t.Fatalf("a finding under min_confidence is skipped, got %q", skipped["Maybe split the router"])
	}
	if skipped[dupTitle] != "duplicate" {
		t.Fatalf("an already-open title is skipped, got %q", skipped[dupTitle])
	}

	// The one issue that was opened is a real maintenance issue: assigned to
	// the maintenance agent, stamped with the scan, labelled.
	var title, assigneeType, assignee, originID, label string
	dbfx.QueryRow(t, `
		SELECT i.title, COALESCE(i.assignee_type, ''), COALESCE(i.assignee_id::text, ''), COALESCE(i.origin_id::text, ''), COALESCE(l.name, '')
		FROM issue i
		LEFT JOIN issue_to_label il ON il.issue_id = i.id
		LEFT JOIN issue_label l ON l.id = il.label_id
		WHERE i.workspace_id = $1 AND i.origin_type = 'code_health' AND i.title LIKE '%Cover the claim fence%'`,
		testWorkspaceID).Scan(&title, &assigneeType, &assignee, &originID, &label)
	if title != codeHealthIssuePrefix+"Cover the claim fence" {
		t.Fatalf("issue title: %q", title)
	}
	if assigneeType != "agent" || assignee != agentID {
		t.Fatalf("the maintenance agent owns the issue: %s/%s", assigneeType, assignee)
	}
	if originID != started.Scan.ID {
		t.Fatalf("the issue is stamped with the scan: %q", originID)
	}
	if label != codeHealthLabelName {
		t.Fatalf("the issue carries the maintenance label, got %q", label)
	}

	// The settled scan releases the workspace for the next one.
	triggerCodeHealthScan(t).Want(http.StatusCreated)
}

func TestCodeHealthMalformedFenceFailsTheScan(t *testing.T) {
	codeHealthCleanup(t)
	agentID := codeHealthAgent(t)
	putCodeHealthSettings(t, map[string]any{"enabled": false, "agent_id": agentID}).Want(http.StatusOK)

	var started codeHealthScanEnvelope
	triggerCodeHealthScan(t).Want(http.StatusCreated).JSON(&started)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, started.Scan.TaskID)

	// A block that is not the contract is not a report.
	output := "I had a look.\n\n```code_health_findings\nnot json at all\n```\n"
	if w := completeTaskViaHandler(t, started.Scan.TaskID, output); w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}

	var list codeHealthScanListEnvelope
	testutil.Call(t, testHandler.ListCodeHealthScans, newRequest(http.MethodGet, "/api/code-health/scans", nil)).
		Want(http.StatusOK).JSON(&list)
	if len(list.Scans) != 1 || list.Scans[0].Status != "failed" || list.Scans[0].Error == "" {
		t.Fatalf("a malformed report fails the scan: %+v", list.Scans)
	}
	var opened int
	dbfx.QueryRow(t, `SELECT count(*) FROM issue WHERE workspace_id = $1 AND origin_type = 'code_health' AND origin_id IS NOT NULL`, testWorkspaceID).Scan(&opened)
	if opened != 0 {
		t.Fatalf("a failed scan opens nothing, got %d issues", opened)
	}
}

// The scheduled sweep only touches workspaces that asked for it, and only when
// their cron came due. The due-ness rule itself is unit-tested in
// service/code_health_test.go; this checks the sweep honours it.
func TestCodeHealthScheduledSweepSkipsDisabledAndNotDue(t *testing.T) {
	codeHealthCleanup(t)
	agentID := codeHealthAgent(t)

	// Disabled: never scanned, however far in the future the clock is.
	putCodeHealthSettings(t, map[string]any{"enabled": false, "agent_id": agentID}).Want(http.StatusOK)
	if n, err := testHandler.ScanCodeHealth(t.Context(), time.Now().Add(365*24*time.Hour)); err != nil || n != 0 {
		t.Fatalf("a disabled workspace is never scanned: started=%d err=%v", n, err)
	}

	// Enabled but before the first occurrence: still nothing.
	putCodeHealthSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "cron": "0 3 * * 1"}).Want(http.StatusOK)
	if n, err := testHandler.ScanCodeHealth(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("not due yet: started=%d err=%v", n, err)
	}

	// Past the next occurrence: exactly one scan.
	n, err := testHandler.ScanCodeHealth(t.Context(), time.Now().Add(8*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("due: started=%d err=%v", n, err)
	}
	// And not a second one while it runs.
	if n, err := testHandler.ScanCodeHealth(t.Context(), time.Now().Add(30*24*time.Hour)); err != nil || n != 0 {
		t.Fatalf("one scan at a time: started=%d err=%v", n, err)
	}
}
