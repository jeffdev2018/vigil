package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// CI auto-fix (K49): a red status on a pull request whose branch belongs to
// an agent's run queues one correction run with the failing checks in its
// brief; one run per failing head; a cap per pull request that, once
// reached, files an inbox item instead of a run; a manual retry goes past
// the cap; a human branch is never touched; the switch is off by default;
// a correction run is stopped by its own budget.

func TestCIAutoFixOnRedChecks(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	rememberSettings(t)
	agent := dbfx.Agent(t, "ci fix agent", handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "CI auto-fix issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	var number int32
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id = $1`, issue).Scan(&number)
	identifier := testHandler.getIssuePrefix(ctx, parseUUID(testWorkspaceID)) + "-" + itoa32(number)
	dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()"), "branch_name": "feat/agent-fix"})
	t.Cleanup(func() {
		cleanupVCS(ctx, issue)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE type = $1 AND workspace_id = $2`, InboxTypeCIAutoFixLimit, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM ci_auto_fix_run WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
	})
	post := func(event string, body map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{"X-Gitea-Event": event, "X-Gitea-Signature": giteaSig(raw)}, raw))
		if w.Code != http.StatusAccepted {
			t.Fatalf("%s: status %d", event, w.Code)
		}
	}
	prBody := func(sha string) map[string]any {
		return map[string]any{"action": "synchronized", "pull_request": map[string]any{
			"number": 21, "html_url": "https://forgejo.test/acme/widget/pulls/21", "title": identifier + " agent work", "state": "open",
			"created_at": "2026-09-04T00:00:00Z", "updated_at": "2026-09-04T00:00:00Z",
			"head": map[string]any{"ref": "feat/agent-fix", "sha": sha}, "user": map[string]any{"username": "multica-bot"},
		}, "repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}}}
	}
	status := func(sha, state string) map[string]any {
		return map[string]any{"sha": sha, "context": "ci/woodpecker", "state": state, "target_url": "https://ci.test/run/" + sha, "description": "3 tests failed"}
	}
	runs := func() int {
		return int(dbfx.Count(t, `SELECT COUNT(*) FROM ci_auto_fix_run WHERE issue_id = $1`, issue))
	}

	// Off by default: a red status changes nothing.
	post("pull_request", prBody("sha-0001"))
	post("status", status("sha-0001", "failure"))
	if runs() != 0 {
		t.Fatal("disabled workspace must not auto-fix")
	}
	testutil.Call(t, testHandler.PutCIAutoFixSettings, newRequest(http.MethodPut, "/api/ci-auto-fix-settings", map[string]any{"enabled": true, "max_attempts": 0})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutCIAutoFixSettings, newRequest(http.MethodPut, "/api/ci-auto-fix-settings", map[string]any{"enabled": true, "max_attempts": 2, "budget_usd_ticks": 100})).Want(http.StatusOK)
	// Red on the agent's branch: one correction run with the failing check in its brief.
	post("status", status("sha-0001", "failure"))
	if runs() != 1 {
		t.Fatalf("runs after first red = %d", runs())
	}
	var taskID string
	dbfx.QueryRow(t, `SELECT task_id FROM ci_auto_fix_run WHERE issue_id = $1`, issue).Scan(&taskID)
	fix := mustTask(t, taskID)
	if fix.Status != "queued" || uuidToString(fix.AgentID) != agent || !strings.Contains(fix.HandoffNote.String, "ci/woodpecker: 3 tests failed — https://ci.test/run/sha-0001") || !strings.Contains(fix.HandoffNote.String, "branch `feat/agent-fix`") {
		t.Fatalf("fix run = %s %q", fix.Status, fix.HandoffNote.String)
	}
	// Same head, red again (redelivery, second context): no second run.
	post("status", map[string]any{"sha": "sha-0001", "context": "ci/lint", "state": "failure"})
	if runs() != 1 {
		t.Fatalf("runs after redelivery = %d", runs())
	}
	// While the correction run is in flight, a new red head waits for it.
	post("pull_request", prBody("sha-0002"))
	post("status", status("sha-0002", "failure"))
	if runs() != 1 {
		t.Fatalf("runs while a fix is in flight = %d", runs())
	}
	// The correction run finished and pushed: red again → attempt 2 (the cap).
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, taskID)
	post("status", status("sha-0002", "failure"))
	if runs() != 2 {
		t.Fatalf("runs after second head = %d", runs())
	}
	// Third red head after the second fix pushed: cap reached → inbox item for the managers, no run.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE issue_id = $1 AND status = 'queued'`, issue)
	post("pull_request", prBody("sha-0003"))
	post("status", status("sha-0003", "failure"))
	if runs() != 2 {
		t.Fatalf("runs past the cap = %d", runs())
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2 AND recipient_id = $3`, InboxTypeCIAutoFixLimit, issue, testUserID); n != 1 {
		t.Fatalf("exhausted inbox items = %d", n)
	}
	post("status", map[string]any{"sha": "sha-0003", "context": "ci/lint", "state": "failure"})
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2 AND recipient_id = $3`, InboxTypeCIAutoFixLimit, issue, testUserID); n != 1 {
		t.Fatalf("exhausted inbox items after redelivery = %d", n)
	}
	// The listing shows the attempts and the policy.
	var listed struct {
		Runs        []CIAutoFixRunResponse `json:"runs"`
		Enabled     bool                   `json:"enabled"`
		MaxAttempts int                    `json:"max_attempts"`
	}
	testutil.Call(t, testHandler.ListIssueCIAutoFix, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issue+"/ci-auto-fix", nil), "id", issue)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Runs) != 2 || !listed.Enabled || listed.MaxAttempts != 2 || listed.Runs[0].Attempt != 2 || listed.Runs[0].TaskStatus != "completed" {
		t.Fatalf("listing = %+v", listed)
	}
	// A human asks for one more: past the cap, once per head.
	prID := listed.Runs[0].PullRequestID
	testutil.Call(t, testHandler.RetryCIAutoFix, testutil.WithURLParams(newRequest(http.MethodPost, "/api/pull-requests/"+prID+"/ci-auto-fix/retry", nil), "id", prID)).Want(http.StatusCreated)
	if runs() != 3 {
		t.Fatalf("runs after manual retry = %d", runs())
	}
	testutil.Call(t, testHandler.RetryCIAutoFix, testutil.WithURLParams(newRequest(http.MethodPost, "/api/pull-requests/"+prID+"/ci-auto-fix/retry", nil), "id", prID)).Want(http.StatusConflict)
	// The correction run has its own budget: spending past it stops the run.
	var manualTask string
	dbfx.QueryRow(t, `SELECT task_id FROM ci_auto_fix_run WHERE issue_id = $1 AND manual = true`, issue).Scan(&manualTask)
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, manualTask)
	dbfx.Insert(t, "task_usage", testutil.Cols{"task_id": manualTask, "model": "stub", "cost_usd_ticks": 150})
	testHandler.enforceCIAutoFixBudget(ctx, mustTask(t, manualTask))
	if got := mustTask(t, manualTask); got.Status != "failed" || got.FailureReason.String != "budget_exceeded" {
		t.Fatalf("budget stop = %s %s", got.Status, got.FailureReason.String)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2 AND details->>'budget_exceeded' = 'true'`, AuditCIAutoFix, issue); n != 1 {
		t.Fatalf("budget audit rows = %d", n)
	}
}

func TestCIAutoFixIgnoresHumanPullRequests(t *testing.T) {
	ctx := context.Background()
	box := withVCSBox(t)
	connID := seedVCSConnection(t, ctx, box, "forgejo", "https://forgejo.test")
	rememberSettings(t)
	testutil.Call(t, testHandler.PutCIAutoFixSettings, newRequest(http.MethodPut, "/api/ci-auto-fix-settings", map[string]any{"enabled": true, "max_attempts": 3, "budget_usd_ticks": 0})).Want(http.StatusOK)
	agent := dbfx.Agent(t, "ci fix idle agent", handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "CI human PR issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	var number int32
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id = $1`, issue).Scan(&number)
	identifier := testHandler.getIssuePrefix(ctx, parseUUID(testWorkspaceID)) + "-" + itoa32(number)
	dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "completed", "completed_at": testutil.Raw("now()"), "branch_name": "feat/agent-branch"})
	t.Cleanup(func() {
		cleanupVCS(ctx, issue)
		testPool.Exec(ctx, `DELETE FROM ci_auto_fix_run WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = $1`, agent)
	})
	raw, _ := json.Marshal(map[string]any{"action": "opened", "pull_request": map[string]any{
		"number": 22, "html_url": "https://forgejo.test/acme/widget/pulls/22", "title": identifier + " human fix", "state": "open",
		"created_at": "2026-09-04T00:00:00Z", "updated_at": "2026-09-04T00:00:00Z",
		"head": map[string]any{"ref": "jeff/manual-fix", "sha": "sha-human"}, "user": map[string]any{"username": "jeff"},
	}, "repository": map[string]any{"name": "widget", "owner": map[string]any{"username": "acme"}}})
	w := httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{"X-Gitea-Event": "pull_request", "X-Gitea-Signature": giteaSig(raw)}, raw))
	st, _ := json.Marshal(map[string]any{"sha": "sha-human", "context": "ci/woodpecker", "state": "failure"})
	w = httptest.NewRecorder()
	testHandler.HandleVCSWebhook(w, vcsWebhookReq(connID, map[string]string{"X-Gitea-Event": "status", "X-Gitea-Signature": giteaSig(st)}, st))
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM ci_auto_fix_run WHERE issue_id = $1`, issue); n != 0 {
		t.Fatal("a human branch must never be auto-fixed")
	}
	var prID string
	dbfx.QueryRow(t, `SELECT id FROM vcs_pull_request WHERE connection_id = $1 AND pr_number = 22`, connID).Scan(&prID)
	testutil.Call(t, testHandler.RetryCIAutoFix, testutil.WithURLParams(newRequest(http.MethodPost, "/api/pull-requests/"+prID+"/ci-auto-fix/retry", nil), "id", prID)).Want(http.StatusUnprocessableEntity)
}

func itoa32(n int32) string {
	return strings.TrimSpace(strings.Replace(strings.Replace(json.Number(itoaJSON(n)).String(), "\"", "", -1), " ", "", -1))
}

func itoaJSON(n int32) string {
	b, _ := json.Marshal(n)
	return string(b)
}
