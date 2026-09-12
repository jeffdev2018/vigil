package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// The producer half of F11/JEF-255: the daemon measures a run's diff and the
// /complete and /fail callbacks are where it lands. Everything downstream (the
// compare view's stat, its "truncated" badge, the run-diff endpoint) reads the
// two columns written here.

// completeWithDiff drives the real /complete handler for taskID with an
// optional diff payload.
func completeWithDiff(t *testing.T, taskID string, body map[string]any) {
	t.Helper()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete", body, testWorkspaceID, "run-group-diff-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testutil.Call(t, testHandler.CompleteTask, req).Want(http.StatusOK)
}

// failWithDiff drives the real /fail handler for taskID with an optional
// diff payload — the losing-attempt half of F11: a run that failed can still
// have delivered a branch, and its diff travels on this callback too.
func failWithDiff(t *testing.T, taskID string, body map[string]any) {
	t.Helper()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/fail", body, testWorkspaceID, "run-group-diff-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testutil.Call(t, testHandler.FailTask, req).Want(http.StatusOK)
}

// readTaskDiff returns the two diff columns of one task row.
func readTaskDiff(t *testing.T, fx *testutil.Fixture, taskID string) (stat []byte, unified *string) {
	t.Helper()
	fx.QueryRow(t, `SELECT diff_stat, diff_unified FROM agent_task_queue WHERE id = $1`, taskID).Scan(&stat, &unified)
	return stat, unified
}

// seedAttempt creates one racing attempt (or, with groupID "", an ordinary run)
// in the running state the terminal callback expects.
func seedAttempt(t *testing.T, fx *testutil.Fixture, label, groupID, issueID string) string {
	t.Helper()
	runtimeID := fx.Runtime(t, label+"-runtime")
	agentID := fx.Agent(t, label+"-agent", runtimeID)
	cols := testutil.Cols{
		"issue_id":   issueID,
		"runtime_id": runtimeID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	}
	if groupID != "" {
		cols["run_group_id"] = groupID
	}
	return fx.Task(t, agentID, cols)
}

func TestCompleteTaskRecordsRunGroupAttemptDiff(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	issueID := fx.Issue(t, "attempt diff lands on the row")
	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 2,
	})
	taskID := seedAttempt(t, fx, "diff-recorded", groupID, issueID)

	patch := "diff --git a/a.txt b/a.txt\n+one\n"
	completeWithDiff(t, taskID, map[string]any{
		"output":       "done",
		"diff_stat":    map[string]any{"files": 2, "insertions": 7, "deletions": 3},
		"diff_unified": patch,
	})

	stat, unified := readTaskDiff(t, fx, taskID)
	var got map[string]int
	if err := json.Unmarshal(stat, &got); err != nil {
		t.Fatalf("decode diff_stat %q: %v", stat, err)
	}
	if got["files"] != 2 || got["insertions"] != 7 || got["deletions"] != 3 {
		t.Errorf("diff_stat = %v, want files 2 / insertions 7 / deletions 3", got)
	}
	if unified == nil || *unified != patch {
		t.Errorf("diff_unified = %v, want the patch verbatim", unified)
	}

	// The patch is stored once. Keeping it in the result JSONB too would double
	// every attempt's cost and make every reader of result parse past it.
	var result []byte
	fx.QueryRow(t, `SELECT result FROM agent_task_queue WHERE id = $1`, taskID).Scan(&result)
	var stored TaskCompleteRequest
	if err := json.Unmarshal(result, &stored); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if stored.DiffUnified != "" || stored.DiffStat != nil {
		t.Errorf("result still carries the diff: %+v", stored)
	}
}

// A stat with no patch is not an incomplete write: it is exactly how the daemon
// reports "the patch exceeded the bound", and the compare view derives its
// truncated badge from it.
func TestCompleteTaskRecordsTruncatedAttemptDiff(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	issueID := fx.Issue(t, "attempt diff was too large")
	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 2,
	})
	taskID := seedAttempt(t, fx, "diff-truncated", groupID, issueID)

	completeWithDiff(t, taskID, map[string]any{
		"output":    "done",
		"diff_stat": map[string]any{"files": 40, "insertions": 90000, "deletions": 12},
	})

	stat, unified := readTaskDiff(t, fx, taskID)
	if len(stat) == 0 {
		t.Fatal("diff_stat is empty; the truncated case must still record the stat")
	}
	if unified != nil {
		t.Errorf("diff_unified = %q, want NULL for a truncated attempt", *unified)
	}
}

// JEF-255: every terminal run carries its diff, not just racing attempts — the
// run-diff endpoint serves it for any run, so a diff on an ordinary run's
// callback is recorded, not ignored.
func TestCompleteTaskRecordsDiffOutsideARunGroup(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	issueID := fx.Issue(t, "ordinary run sending a diff")
	taskID := seedAttempt(t, fx, "diff-ungrouped", "", issueID)

	patch := "diff --git a/a.txt b/a.txt\n"
	completeWithDiff(t, taskID, map[string]any{
		"output":       "done",
		"diff_stat":    map[string]any{"files": 1, "insertions": 1, "deletions": 0},
		"diff_unified": patch,
	})

	stat, unified := readTaskDiff(t, fx, taskID)
	var got map[string]int
	if err := json.Unmarshal(stat, &got); err != nil {
		t.Fatalf("decode diff_stat %q: %v", stat, err)
	}
	if got["files"] != 1 || got["insertions"] != 1 {
		t.Errorf("diff_stat = %v, want files 1 / insertions 1", got)
	}
	if unified == nil || *unified != patch {
		t.Errorf("diff_unified = %v, want the patch verbatim", unified)
	}
}

// A losing attempt that still delivered a branch reports its diff on the
// /fail callback exactly like a winner does on /complete (F11 fail path).
func TestFailTaskRecordsRunGroupAttemptDiff(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	issueID := fx.Issue(t, "failed attempt diff lands on the row")
	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 2,
	})
	taskID := seedAttempt(t, fx, "diff-recorded-on-fail", groupID, issueID)

	patch := "diff --git a/a.txt b/a.txt\n+partial\n"
	failWithDiff(t, taskID, map[string]any{
		"error":        "agent crashed mid-run",
		"diff_stat":    map[string]any{"files": 1, "insertions": 4, "deletions": 0},
		"diff_unified": patch,
	})

	stat, unified := readTaskDiff(t, fx, taskID)
	var got map[string]int
	if err := json.Unmarshal(stat, &got); err != nil {
		t.Fatalf("decode diff_stat %q: %v", stat, err)
	}
	if got["files"] != 1 || got["insertions"] != 4 || got["deletions"] != 0 {
		t.Errorf("diff_stat = %v, want files 1 / insertions 4 / deletions 0", got)
	}
	if unified == nil || *unified != patch {
		t.Errorf("diff_unified = %v, want the patch verbatim", unified)
	}
}

// JEF-255: an ordinary failed run's diff lands exactly like an attempt's —
// same rule as /complete.
func TestFailTaskRecordsDiffOutsideARunGroup(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	issueID := fx.Issue(t, "ordinary failed run sending a diff")
	taskID := seedAttempt(t, fx, "diff-ungrouped-fail", "", issueID)

	patch := "diff --git a/a.txt b/a.txt\n"
	failWithDiff(t, taskID, map[string]any{
		"error":        "agent crashed mid-run",
		"diff_stat":    map[string]any{"files": 1, "insertions": 1, "deletions": 0},
		"diff_unified": patch,
	})

	stat, unified := readTaskDiff(t, fx, taskID)
	var got map[string]int
	if err := json.Unmarshal(stat, &got); err != nil {
		t.Fatalf("decode diff_stat %q: %v", stat, err)
	}
	if got["files"] != 1 || got["insertions"] != 1 {
		t.Errorf("diff_stat = %v, want files 1 / insertions 1", got)
	}
	if unified == nil || *unified != patch {
		t.Errorf("diff_unified = %v, want the patch verbatim", unified)
	}
}

// run_group_id on the claim wire still marks the task as an attempt for the
// daemon (attempt bookkeeping); since JEF-255 the diff no longer depends on it
// — every terminal run with a branch is measured.
func TestClaimResponseCarriesRunGroupID(t *testing.T) {
	fx := testutil.New(testPool, testWorkspaceID, testUserID)
	issueID := fx.Issue(t, "claim payload names the group")
	groupID := fx.Insert(t, "run_group", testutil.Cols{
		"workspace_id":  testWorkspaceID,
		"issue_id":      issueID,
		"created_by":    testUserID,
		"attempt_count": 2,
	})
	runtimeID := fx.Runtime(t, "claim-payload-runtime")
	agentID := fx.Agent(t, "claim-payload-agent", runtimeID)
	attemptID := fx.Task(t, agentID, testutil.Cols{
		"issue_id":     issueID,
		"runtime_id":   runtimeID,
		"run_group_id": groupID,
	})
	ordinaryID := fx.Task(t, agentID, testutil.Cols{
		"issue_id":   issueID,
		"runtime_id": runtimeID,
	})

	for _, tc := range []struct {
		name   string
		taskID string
		want   string
	}{
		{name: "attempt", taskID: attemptID, want: groupID},
		{name: "ordinary run", taskID: ordinaryID, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(tc.taskID))
			if err != nil {
				t.Fatalf("load task: %v", err)
			}
			if got := taskToResponse(task, testWorkspaceID).RunGroupID; got != tc.want {
				t.Errorf("run_group_id = %q, want %q", got, tc.want)
			}
		})
	}
}
