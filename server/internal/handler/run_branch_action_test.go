package handler

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// JEF-255: promoting or discarding the branch a terminal run delivered.
//
// The endpoint's whole job is to refuse the cases that would lose or leak work
// and to enqueue exactly one request for the rest. The git side is pinned in
// internal/daemon/execenv/branch_action_test.go.

// branchActionFixture builds a project with a worktree local_directory, a
// runtime advertising the branch-action capability, and one terminal run with
// a branch on an issue.
type branchActionFixture struct {
	IssueID   string
	TaskID    string
	RuntimeID string
	AgentID   string
	DaemonID  string
}

func newBranchActionFixture(t *testing.T, over ...testutil.Cols) branchActionFixture {
	t.Helper()
	suffix := time.Now().UnixNano()
	daemonID := fmt.Sprintf("daemon-branch-action-%d", suffix)

	runtime := dbfx.Runtime(t, fmt.Sprintf("branch-action-runtime-%d", suffix), testutil.Cols{
		"daemon_id":    daemonID,
		"runtime_mode": "local",
		"metadata": testutil.Raw(fmt.Sprintf(`'{"capabilities":["%s"]}'::jsonb`,
			protocol.DaemonCapabilityBranchActionV1)),
	})
	agent := dbfx.Agent(t, fmt.Sprintf("branch-action-agent-%d", suffix), runtime)
	project := dbfx.Project(t, fmt.Sprintf("branch-action-project-%d", suffix))
	dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id":    project,
		"workspace_id":  testWorkspaceID,
		"resource_type": "local_directory",
		"resource_ref": testutil.Raw(fmt.Sprintf(
			`'{"local_path":"/tmp/branch-action-repo","daemon_id":%s,"execution_mode":"worktree"}'::jsonb`,
			quoteJSONString(daemonID))),
		"position": 0,
	})
	issue := dbfx.Issue(t, fmt.Sprintf("branch action issue %d", suffix), testutil.Cols{"project_id": project})

	cols := testutil.Cols{
		"issue_id":    issue,
		"runtime_id":  runtime,
		"status":      "completed",
		"branch_name": "agent/j/run",
	}
	for _, o := range over {
		for k, v := range o {
			cols[k] = v
		}
	}
	task := dbfx.Task(t, agent, cols)
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM run_branch_action_request WHERE task_id = $1`, task)
	})
	return branchActionFixture{IssueID: issue, TaskID: task, RuntimeID: runtime, AgentID: agent, DaemonID: daemonID}
}

func requestBranchAction(t *testing.T, issueID, taskID, action string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/runs/"+taskID+"/"+action, nil)
	var h http.HandlerFunc
	switch action {
	case "promote":
		h = testHandler.PromoteIssueRun
	case "discard":
		h = testHandler.DiscardIssueRun
	}
	return testutil.Call(t, h, testutil.WithURLParams(req, "id", issueID, "taskId", taskID))
}

func reportBranchAction(t *testing.T, runtimeID, requestID string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost,
		fmt.Sprintf("/api/daemon/runtimes/%s/branch-action/%s/result", runtimeID, requestID), body)
	return testutil.Call(t, testHandler.ReportBranchActionResult,
		testutil.WithURLParams(req, "runtimeId", runtimeID, "requestId", requestID))
}

func TestRunBranchActionPromoteEnqueuesOnePendingRequest(t *testing.T) {
	f := newBranchActionFixture(t)

	var out RunBranchActionResponse
	requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusCreated).JSON(&out)

	if out.RequestID == "" {
		t.Fatal("no request id returned; the client has nothing to follow")
	}
	if out.Status != runBranchActionStatusPending {
		t.Fatalf("status = %q, want %q", out.Status, runBranchActionStatusPending)
	}
	var action, branch string
	dbfx.QueryRow(t, `SELECT action, branch_name FROM run_branch_action_request WHERE id = $1`, out.RequestID).Scan(&action, &branch)
	if action != "promote" || branch != "agent/j/run" {
		t.Fatalf("request row = (%q, %q), want (promote, agent/j/run)", action, branch)
	}
}

func TestRunBranchActionGuardMatrix(t *testing.T) {
	for _, tc := range []struct {
		name string
		over testutil.Cols
		code string
	}{
		{"non-terminal run", testutil.Cols{"status": "running"}, ErrCodeRunNotPromotable},
		{"no branch", testutil.Cols{"branch_name": nil}, ErrCodeRunNotPromotable},
		{"already promoted", testutil.Cols{"promoted_at": testutil.Raw("now()")}, ErrCodeRunNotPromotable},
		{"already discarded", testutil.Cols{"discarded_at": testutil.Raw("now()")}, ErrCodeRunNotPromotable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBranchActionFixture(t, tc.over)
			res := requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusConflict)
			if got := res.Map()["code"]; got != tc.code {
				t.Fatalf("code = %v, want %q", got, tc.code)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM run_branch_action_request WHERE task_id = $1`, f.TaskID); n != 0 {
				t.Fatalf("a refused promote queued %d request(s)", n)
			}
		})
	}
}

func TestRunBranchActionDiscardGuardMatrix(t *testing.T) {
	for _, tc := range []struct {
		name string
		over testutil.Cols
	}{
		{"non-terminal run", testutil.Cols{"status": "queued"}},
		{"no branch", testutil.Cols{"branch_name": nil}},
		{"already discarded", testutil.Cols{"discarded_at": testutil.Raw("now()")}},
		{"already promoted", testutil.Cols{"promoted_at": testutil.Raw("now()")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBranchActionFixture(t, tc.over)
			res := requestBranchAction(t, f.IssueID, f.TaskID, "discard").Want(http.StatusConflict)
			if got := res.Map()["code"]; got != ErrCodeRunNotDiscardable {
				t.Fatalf("code = %v, want %q", got, ErrCodeRunNotDiscardable)
			}
		})
	}
}

// A second action while one is in flight is refused regardless of direction:
// two daemons pushes racing a delete on one branch is the case that loses work.
func TestRunBranchActionRefusesASecondInFlightRequest(t *testing.T) {
	f := newBranchActionFixture(t)

	requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusCreated)

	res := requestBranchAction(t, f.IssueID, f.TaskID, "discard").Want(http.StatusConflict)
	if got := res.Map()["code"]; got != ErrCodeRunBranchActionInFlight {
		t.Fatalf("code = %v, want %q", got, ErrCodeRunBranchActionInFlight)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM run_branch_action_request WHERE task_id = $1`, f.TaskID); n != 1 {
		t.Fatalf("run_branch_action_request rows = %d, want 1 — a second request was queued", n)
	}
}

// Without the capability nothing may be emitted: an old daemon would claim the
// request, skip the field it does not know, and never report.
func TestRunBranchActionRefusesARuntimeWithoutTheCapability(t *testing.T) {
	f := newBranchActionFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata = '{"capabilities":[]}'::jsonb WHERE id = $1`, f.RuntimeID)

	requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusConflict)

	if n := dbfx.Count(t, `SELECT count(*) FROM run_branch_action_request WHERE task_id = $1`, f.TaskID); n != 0 {
		t.Fatalf("a runtime without the capability queued %d request(s)", n)
	}
}

// The taskId path segment must not be a way to reach a run on another issue.
func TestRunBranchActionRejectsARunFromAnotherIssue(t *testing.T) {
	f := newBranchActionFixture(t)
	other := dbfx.Issue(t, "an unrelated issue")

	requestBranchAction(t, other, f.TaskID, "promote").Want(http.StatusNotFound)
}

// The heartbeat is the only delivery channel, and the claim has to carry
// everything the daemon needs in one payload — and only the runtime that owns
// the request may see it.
func TestRunBranchActionHeartbeatClaimCarriesTheWholePayload(t *testing.T) {
	f := newBranchActionFixture(t)
	otherRuntime := dbfx.Runtime(t, fmt.Sprintf("branch-action-other-%d", time.Now().UnixNano()))

	requestBranchAction(t, f.IssueID, f.TaskID, "discard").Want(http.StatusCreated)

	if none := testHandler.claimBranchActionForHeartbeat(t.Context(), parseUUID(otherRuntime)); none != nil {
		t.Fatalf("another runtime claimed the request: %+v", none)
	}

	pending := testHandler.claimBranchActionForHeartbeat(t.Context(), parseUUID(f.RuntimeID))
	if pending == nil {
		t.Fatal("the heartbeat claimed nothing for a runtime with a pending branch action")
	}
	if pending.Action != "discard" || pending.Branch != "agent/j/run" || pending.TaskID != f.TaskID {
		t.Fatalf("payload = %+v", pending)
	}
	if pending.LocalPath != "/tmp/branch-action-repo" {
		t.Fatalf("local_path = %q, want the project's local_directory path", pending.LocalPath)
	}

	// The claim is exactly-once: a second heartbeat must not hand the same
	// destructive request to a second worker.
	if again := testHandler.claimBranchActionForHeartbeat(t.Context(), parseUUID(f.RuntimeID)); again != nil {
		t.Fatalf("a second heartbeat re-claimed the same request: %+v", again)
	}
}

type fakeBranchPRCreator struct {
	url string
	err error
	// captured
	remoteURL, baseBranch, head string
	calls                       int
}

func (f *fakeBranchPRCreator) CreateRunPullRequest(_ context.Context, _ db.Issue, req db.RunBranchActionRequest, remoteURL, baseBranch string) (string, error) {
	f.calls++
	f.remoteURL, f.baseBranch, f.head = remoteURL, baseBranch, req.BranchName
	return f.url, f.err
}

// The push happened on the daemon; the server opens the PR and records both
// facts on the task row.
func TestRunBranchActionPromoteResultOpensThePRAndMarksTheRun(t *testing.T) {
	f := newBranchActionFixture(t)
	creator := &fakeBranchPRCreator{url: "https://gitlab.example.test/org/repo/-/merge_requests/7"}
	prev := testHandler.BranchPRCreator
	testHandler.BranchPRCreator = creator
	t.Cleanup(func() { testHandler.BranchPRCreator = prev })

	var enqueued RunBranchActionResponse
	requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusCreated).JSON(&enqueued)
	dbfx.Exec(t, `UPDATE run_branch_action_request SET status = 'claimed', claimed_at = now() WHERE id = $1`, enqueued.RequestID)

	reportBranchAction(t, f.RuntimeID, enqueued.RequestID, map[string]any{
		"status":         "completed",
		"head_sha":       "aaaa1111bbbb2222cccc3333dddd4444eeee5555",
		"remote_url":     "git@gitlab.example.test:org/repo.git",
		"default_branch": "main",
	}).Want(http.StatusOK)

	if creator.calls != 1 {
		t.Fatalf("PR creator calls = %d, want 1", creator.calls)
	}
	if creator.remoteURL != "git@gitlab.example.test:org/repo.git" || creator.baseBranch != "main" || creator.head != "agent/j/run" {
		t.Fatalf("PR creator got remote=%q base=%q head=%q", creator.remoteURL, creator.baseBranch, creator.head)
	}
	var prURL string
	var promotedAt *time.Time
	dbfx.QueryRow(t, `SELECT promote_pr_url, promoted_at FROM agent_task_queue WHERE id = $1`, f.TaskID).Scan(&prURL, &promotedAt)
	if promotedAt == nil {
		t.Fatal("promoted_at is NULL after a completed promote")
	}
	if prURL != creator.url {
		t.Fatalf("promote_pr_url = %q, want %q", prURL, creator.url)
	}
	var status string
	dbfx.QueryRow(t, `SELECT status FROM run_branch_action_request WHERE id = $1`, enqueued.RequestID).Scan(&status)
	if status != runBranchActionStatusCompleted {
		t.Fatalf("request status = %q, want %q", status, runBranchActionStatusCompleted)
	}
}

// No provider covering the remote: the push alone satisfies promote, so the
// run is still marked and pr_url stays empty.
func TestRunBranchActionPromoteResultWithoutAProviderStillMarksTheRun(t *testing.T) {
	f := newBranchActionFixture(t)

	var enqueued RunBranchActionResponse
	requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusCreated).JSON(&enqueued)
	dbfx.Exec(t, `UPDATE run_branch_action_request SET status = 'claimed', claimed_at = now() WHERE id = $1`, enqueued.RequestID)

	reportBranchAction(t, f.RuntimeID, enqueued.RequestID, map[string]any{
		"status":     "completed",
		"remote_url": "git@codehost-no-connection.example:org/repo.git",
	}).Want(http.StatusOK)

	var prURL string
	var promotedAt *time.Time
	dbfx.QueryRow(t, `SELECT promote_pr_url, promoted_at FROM agent_task_queue WHERE id = $1`, f.TaskID).Scan(&prURL, &promotedAt)
	if promotedAt == nil {
		t.Fatal("promoted_at is NULL after a provider-less promote")
	}
	if prURL != "" {
		t.Fatalf("promote_pr_url = %q, want empty without a provider", prURL)
	}
}

// A failure marks the request and stores the daemon's cause — and leaves the
// task row untouched, so the user can retry.
func TestRunBranchActionFailureLeavesTheTaskRetryable(t *testing.T) {
	f := newBranchActionFixture(t)

	var enqueued RunBranchActionResponse
	requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusCreated).JSON(&enqueued)
	dbfx.Exec(t, `UPDATE run_branch_action_request SET status = 'claimed', claimed_at = now() WHERE id = $1`, enqueued.RequestID)

	var out RunBranchActionResponse
	reportBranchAction(t, f.RuntimeID, enqueued.RequestID, map[string]any{
		"status": "failed",
		"error":  "no_remote: /tmp/branch-action-repo has no origin remote; add one and promote again",
	}).Want(http.StatusOK).JSON(&out)

	if out.Status != runBranchActionStatusFailed || out.Error == "" {
		t.Fatalf("settled = %+v, want failed with the daemon's cause", out)
	}
	var promotedAt, discardedAt *time.Time
	dbfx.QueryRow(t, `SELECT promoted_at, discarded_at FROM agent_task_queue WHERE id = $1`, f.TaskID).Scan(&promotedAt, &discardedAt)
	if promotedAt != nil || discardedAt != nil {
		t.Fatal("a failed action marked the task row")
	}

	// Retry: a settled-failed request must not block a fresh one.
	requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusCreated)
}

func TestRunBranchActionDiscardResultMarksTheRun(t *testing.T) {
	f := newBranchActionFixture(t)

	var enqueued RunBranchActionResponse
	requestBranchAction(t, f.IssueID, f.TaskID, "discard").Want(http.StatusCreated).JSON(&enqueued)
	dbfx.Exec(t, `UPDATE run_branch_action_request SET status = 'claimed', claimed_at = now() WHERE id = $1`, enqueued.RequestID)

	reportBranchAction(t, f.RuntimeID, enqueued.RequestID, map[string]any{
		"status":   "completed",
		"head_sha": "bbbb1111bbbb2222cccc3333dddd4444eeee5555",
	}).Want(http.StatusOK)

	var discardedAt *time.Time
	dbfx.QueryRow(t, `SELECT discarded_at FROM agent_task_queue WHERE id = $1`, f.TaskID).Scan(&discardedAt)
	if discardedAt == nil {
		t.Fatal("discarded_at is NULL after a completed discard")
	}

	// A discarded run may never be promoted afterwards: its branch is gone.
	res := requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusConflict)
	if got := res.Map()["code"]; got != ErrCodeRunNotPromotable {
		t.Fatalf("code = %v, want %q", got, ErrCodeRunNotPromotable)
	}
}

// The daemon retries terminal reports. A second delivery of a destructive
// action must change nothing.
func TestRunBranchActionResultIsIdempotent(t *testing.T) {
	f := newBranchActionFixture(t)

	var enqueued RunBranchActionResponse
	requestBranchAction(t, f.IssueID, f.TaskID, "discard").Want(http.StatusCreated).JSON(&enqueued)
	dbfx.Exec(t, `UPDATE run_branch_action_request SET status = 'claimed', claimed_at = now() WHERE id = $1`, enqueued.RequestID)

	reportBranchAction(t, f.RuntimeID, enqueued.RequestID, map[string]any{"status": "completed"}).Want(http.StatusOK)

	var out RunBranchActionResponse
	reportBranchAction(t, f.RuntimeID, enqueued.RequestID, map[string]any{
		"status": "failed", "error": "a retry that must not overwrite the outcome",
	}).Want(http.StatusOK).JSON(&out)

	if out.Status != runBranchActionStatusCompleted {
		t.Fatalf("a retried report rewrote the outcome: status = %q, want %q", out.Status, runBranchActionStatusCompleted)
	}
}

// The issue's run list surfaces the in-flight action in one batched read.
func TestListTasksByIssueCarriesPendingBranchAction(t *testing.T) {
	f := newBranchActionFixture(t)

	requestBranchAction(t, f.IssueID, f.TaskID, "promote").Want(http.StatusCreated)

	var out []AgentTaskResponse
	testutil.Call(t, testHandler.ListTasksByIssue,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+f.IssueID+"/task-runs", nil), "id", f.IssueID)).
		Want(http.StatusOK).JSON(&out)

	var found *AgentTaskResponse
	for i := range out {
		if out[i].ID == f.TaskID {
			found = &out[i]
		}
	}
	if found == nil {
		t.Fatal("the run is missing from its issue's task list")
	}
	if found.PendingBranchAction != "promote" {
		t.Fatalf("pending_branch_action = %q, want promote", found.PendingBranchAction)
	}
	if found.PromotedAt != nil || found.DiscardedAt != nil || found.PromotePRURL != "" {
		t.Fatalf("terminal facts leaked onto an untouched run: %+v", found)
	}
}

// GET /api/tasks/{taskId}/diff serves the stored diff; a run whose daemon never
// recorded one (or that has no branch) is a 404 with a stable code, not a null
// document.
func TestGetTaskDiff(t *testing.T) {
	f := newBranchActionFixture(t)
	patch := "diff --git a/a.txt b/a.txt\n+one\n"
	dbfx.Exec(t, `UPDATE agent_task_queue SET diff_stat = '{"files":1,"insertions":1,"deletions":0}'::jsonb, diff_unified = $2 WHERE id = $1`, f.TaskID, patch)

	var out RunDiffResponse
	req := newRequest(http.MethodGet, "/api/tasks/"+f.TaskID+"/diff", nil)
	testutil.Call(t, testHandler.GetTaskDiff, testutil.WithURLParams(req, "taskId", f.TaskID)).
		Want(http.StatusOK).JSON(&out)

	if out.DiffUnified == nil || *out.DiffUnified != patch {
		t.Fatalf("diff_unified = %v, want the patch verbatim", out.DiffUnified)
	}
	if out.DiffTruncated {
		t.Fatal("diff_truncated = true for a run that stored its patch")
	}
	if string(out.DiffStat) == "" {
		t.Fatal("diff_stat is empty")
	}

	// Truncated: a stat with no patch.
	dbfx.Exec(t, `UPDATE agent_task_queue SET diff_unified = NULL WHERE id = $1`, f.TaskID)
	req = newRequest(http.MethodGet, "/api/tasks/"+f.TaskID+"/diff", nil)
	testutil.Call(t, testHandler.GetTaskDiff, testutil.WithURLParams(req, "taskId", f.TaskID)).
		Want(http.StatusOK).JSON(&out)
	if !out.DiffTruncated || out.DiffUnified != nil {
		t.Fatalf("truncated case = %+v, want diff_truncated and a null patch", out)
	}

	// Nothing recorded: 404 with the contract's code.
	noDiff := newBranchActionFixture(t)
	req = newRequest(http.MethodGet, "/api/tasks/"+noDiff.TaskID+"/diff", nil)
	res := testutil.Call(t, testHandler.GetTaskDiff, testutil.WithURLParams(req, "taskId", noDiff.TaskID)).
		Want(http.StatusNotFound)
	if got := res.Map()["code"]; got != ErrCodeRunDiffNotFound {
		t.Fatalf("code = %v, want %q", got, ErrCodeRunDiffNotFound)
	}
}

// Regression (caught by the JEF-255 live e2e): the HTTP heartbeat writer
// mapped every pending-work field except PendingBranchAction, so a daemon on
// the HTTP fallback claimed the request (status flipped to claimed) but never
// received the payload. The WS path returns the full ack and was unaffected.
func TestRunBranchActionHeartbeatResponseCarriesTheClaim(t *testing.T) {
	f := newBranchActionFixture(t)
	requestBranchAction(t, f.IssueID, f.TaskID, "discard").Want(http.StatusCreated)

	req := newDaemonTokenRequest("POST", "/api/daemon/heartbeat", map[string]any{
		"runtime_id": f.RuntimeID,
	}, testWorkspaceID, f.DaemonID)
	resp := testutil.Call(t, testHandler.DaemonHeartbeat, req).Want(http.StatusOK)

	var body struct {
		PendingBranchAction *protocol.DaemonHeartbeatPendingBranchAction `json:"pending_branch_action"`
	}
	resp.JSON(&body)
	if body.PendingBranchAction == nil {
		t.Fatalf("heartbeat response = %s, want pending_branch_action", resp.Text())
	}
	if body.PendingBranchAction.TaskID != f.TaskID || body.PendingBranchAction.Action != "discard" {
		t.Fatalf("pending_branch_action = %+v", body.PendingBranchAction)
	}
}
