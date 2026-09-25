package handler

import (
	"fmt"
	"net/http"
	"sort"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// JEF-388: dead run-branch garbage collection — the dry-run plan, the
// human-confirmed batch discard, and the branch_gc_tick sweep. All three ride
// the JEF-255 branch-action channel pinned in run_branch_action_test.go.

func listDeadBranches(t *testing.T, req *http.Request) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.ListDeadBranches, req)
}

// deadBranchEntryByTask indexes one plan response by task id.
func deadBranchEntryByTask(t *testing.T, resp *testutil.Response) map[string]DeadBranchEntry {
	t.Helper()
	var body struct {
		Entries []DeadBranchEntry `json:"entries"`
	}
	resp.JSON(&body)
	byTask := make(map[string]DeadBranchEntry, len(body.Entries))
	for _, e := range body.Entries {
		byTask[e.TaskID] = e
	}
	return byTask
}

// The plan of a workspace with no dead branches is an empty list, not null.
func TestListDeadBranchesEmptyWorkspace(t *testing.T) {
	ws := dbfx.Workspace(t, fmt.Sprintf("branch-gc-empty-%d", time.Now().UnixNano()),
		fmt.Sprintf("branch-gc-empty-%d", time.Now().UnixNano()))
	dbfx.Member(t, ws, testUserID, "member")

	req := newRequest(http.MethodGet, "/api/runs/dead-branches", nil)
	req.Header.Set("X-Workspace-ID", ws)
	resp := listDeadBranches(t, req).Want(http.StatusOK)
	if got := resp.Map()["entries"]; got == nil {
		t.Fatalf("entries = null, want an empty array: %s", resp.Text())
	}
	if entries, ok := resp.Map()["entries"].([]any); !ok || len(entries) != 0 {
		t.Fatalf("entries = %v, want empty", resp.Map()["entries"])
	}
}

// One plan row per dead branch, each marked actionable or carrying the reason
// it cannot be collected right now; runs already promoted leave the set
// entirely.
func TestListDeadBranchesMixOfActionableAndSkipped(t *testing.T) {
	actionable := newBranchActionFixture(t)

	offline := newBranchActionFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, offline.RuntimeID)

	stale := newBranchActionFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET last_seen_at = now() - interval '1 hour' WHERE id = $1`, stale.RuntimeID)

	noCapability := newBranchActionFixture(t)
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata = '{"capabilities":[]}'::jsonb WHERE id = $1`, noCapability.RuntimeID)

	pending := newBranchActionFixture(t)
	requestBranchAction(t, pending.IssueID, pending.TaskID, "discard").Want(http.StatusCreated)

	promoted := newBranchActionFixture(t, testutil.Cols{"promoted_at": testutil.Raw("now()")})
	active := newBranchActionFixture(t, testutil.Cols{"status": "running"})

	entries := deadBranchEntryByTask(t, listDeadBranches(t,
		newRequest(http.MethodGet, "/api/runs/dead-branches", nil)).Want(http.StatusOK))

	entry, ok := entries[actionable.TaskID]
	if !ok {
		t.Fatalf("the actionable run is missing from the plan: %v", entries)
	}
	if !entry.Actionable || entry.SkipReason != nil {
		t.Fatalf("actionable entry = %+v, want actionable with no reason", entry)
	}
	if entry.BranchName != "agent/j/run" || entry.RuntimeID != actionable.RuntimeID || entry.RuntimeName == "" {
		t.Fatalf("entry = %+v, want branch/runtime carried", entry)
	}
	if entry.IssueID == nil || *entry.IssueID != actionable.IssueID {
		t.Fatalf("issue_id = %v, want %q", entry.IssueID, actionable.IssueID)
	}
	if entry.IssueIdentifier == nil || entry.IssueTitle == nil {
		t.Fatalf("identifier/title = %v/%v, want both set for an issue run", entry.IssueIdentifier, entry.IssueTitle)
	}

	wantReasons := map[string]string{
		offline.TaskID:      DeadBranchSkipRuntimeOffline,
		stale.TaskID:        DeadBranchSkipRuntimeOffline,
		noCapability.TaskID: DeadBranchSkipCapabilityMissing,
		pending.TaskID:      DeadBranchSkipActionPending,
	}
	for taskID, want := range wantReasons {
		entry, ok := entries[taskID]
		if !ok {
			t.Fatalf("run %s is missing from the plan", taskID)
		}
		if entry.Actionable || entry.SkipReason == nil || *entry.SkipReason != want {
			t.Fatalf("entry %s = %+v, want skip_reason %q", taskID, entry, want)
		}
	}

	if _, ok := entries[promoted.TaskID]; ok {
		t.Fatal("an already-promoted run showed up as a dead branch")
	}
	if _, ok := entries[active.TaskID]; ok {
		t.Fatal("a running run showed up as a dead branch")
	}
}

// The plan is a workspace read: a non-member gets the same 404 as every other
// workspace-scoped read.
func TestListDeadBranchesRequiresMembership(t *testing.T) {
	listDeadBranches(t, newRequestAsUser("00000000-0000-0000-0000-000000000042",
		http.MethodGet, "/api/runs/dead-branches", nil)).Want(http.StatusNotFound)
}

// The batch enqueues a real discard per confirmed run — same request row,
// same audit, same daemon wake-up as the single-run endpoint.
func TestDiscardDeadBranchesHappyPath(t *testing.T) {
	a := newBranchActionFixture(t)
	b := newBranchActionFixture(t)

	recorder := &runtimeLocalSkillPendingWorkRecorder{}
	h := *testHandler
	h.DaemonPendingWork = recorder

	req := newRequest(http.MethodPost, "/api/runs/dead-branches/discard",
		map[string]any{"task_ids": []string{a.TaskID, b.TaskID}})
	var out struct {
		Enqueued int                        `json:"enqueued"`
		Skipped  []DeadBranchDiscardSkipped `json:"skipped"`
	}
	testutil.Call(t, h.DiscardDeadBranches, req).Want(http.StatusOK).JSON(&out)

	if out.Enqueued != 2 || len(out.Skipped) != 0 {
		t.Fatalf("batch = %+v, want 2 enqueued and no skips", out)
	}
	for _, f := range []branchActionFixture{a, b} {
		var action, status string
		dbfx.QueryRow(t, `SELECT action, status FROM run_branch_action_request WHERE task_id = $1`, f.TaskID).
			Scan(&action, &status)
		if action != "discard" || status != runBranchActionStatusPending {
			t.Fatalf("request row for %s = (%q, %q), want (discard, pending)", f.TaskID, action, status)
		}
		if n := dbfx.Count(t,
			`SELECT count(*) FROM audit_log_entry WHERE action = $1 AND entity_id = $2`,
			AuditRunBranchAction, f.TaskID); n != 1 {
			t.Fatalf("audit entries for %s = %d, want 1", f.TaskID, n)
		}
	}
	wantHints := []string{
		a.RuntimeID + ":" + protocol.PendingWorkKindBranchAction,
		b.RuntimeID + ":" + protocol.PendingWorkKindBranchAction,
	}
	sortStrings := func(s []string) []string { sort.Strings(s); return s }
	if got := sortStrings(append([]string{}, recorder.hints...)); fmt.Sprint(got) != fmt.Sprint(sortStrings(wantHints)) {
		t.Fatalf("pending-work hints = %v, want %v", got, wantHints)
	}
}

// Per-run refusals land in skipped with the guard's own sentence; the batch
// itself still answers 200.
func TestDiscardDeadBranchesPartialSkips(t *testing.T) {
	good := newBranchActionFixture(t)
	running := newBranchActionFixture(t, testutil.Cols{"status": "running"})
	promoted := newBranchActionFixture(t, testutil.Cols{"promoted_at": testutil.Raw("now()")})

	missing := "00000000-0000-0000-0000-000000000099"

	req := newRequest(http.MethodPost, "/api/runs/dead-branches/discard", map[string]any{
		"task_ids": []string{good.TaskID, running.TaskID, promoted.TaskID, missing, "not-a-uuid", good.TaskID},
	})
	var out struct {
		Enqueued int                        `json:"enqueued"`
		Skipped  []DeadBranchDiscardSkipped `json:"skipped"`
	}
	testutil.Call(t, testHandler.DiscardDeadBranches, req).Want(http.StatusOK).JSON(&out)

	if out.Enqueued != 1 {
		t.Fatalf("enqueued = %d, want 1 (skipped = %+v)", out.Enqueued, out.Skipped)
	}
	if len(out.Skipped) != 5 {
		t.Fatalf("skipped = %+v, want 5 entries", out.Skipped)
	}
	reasons := map[string]string{}
	for _, s := range out.Skipped {
		reasons[s.TaskID] = s.Reason
	}
	if reasons[missing] != "run not found" || reasons["not-a-uuid"] != "invalid task_id" {
		t.Fatalf("lookup skips = %+v", out.Skipped)
	}
	if reasons[running.TaskID] == "" || reasons[promoted.TaskID] == "" {
		t.Fatalf("guard skips lost their reason: %+v", out.Skipped)
	}
	// The duplicate rides the in-flight guard: the first occurrence enqueued.
	if reasons[good.TaskID] == "" {
		t.Fatalf("the duplicate of an enqueued run was not reported: %+v", out.Skipped)
	}
}

// The batch is capped like the issue GC check batch it mirrors.
func TestDiscardDeadBranchesCapsAt200(t *testing.T) {
	ids := make([]string, maxDeadBranchDiscardBatch+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("00000000-0000-0000-0000-%012d", i)
	}
	req := newRequest(http.MethodPost, "/api/runs/dead-branches/discard", map[string]any{"task_ids": ids})
	testutil.Call(t, testHandler.DiscardDeadBranches, req).Want(http.StatusBadRequest)
}

// A run the caller's workspace does not own is "run not found", like the
// single-run endpoint's tenant guard.
func TestDiscardDeadBranchesRejectsRunsFromAnotherWorkspace(t *testing.T) {
	f := newBranchActionFixture(t)
	ws := dbfx.Workspace(t, fmt.Sprintf("branch-gc-other-%d", time.Now().UnixNano()),
		fmt.Sprintf("branch-gc-other-%d", time.Now().UnixNano()))
	dbfx.Member(t, ws, testUserID, "member")

	req := newRequest(http.MethodPost, "/api/runs/dead-branches/discard",
		map[string]any{"task_ids": []string{f.TaskID}})
	req.Header.Set("X-Workspace-ID", ws)
	var out struct {
		Enqueued int                        `json:"enqueued"`
		Skipped  []DeadBranchDiscardSkipped `json:"skipped"`
	}
	testutil.Call(t, testHandler.DiscardDeadBranches, req).Want(http.StatusOK).JSON(&out)
	if out.Enqueued != 0 || len(out.Skipped) != 1 || out.Skipped[0].Reason != "run not found" {
		t.Fatalf("batch = %+v, want the foreign run skipped as not found", out)
	}
}

func TestDiscardDeadBranchesRequiresMembership(t *testing.T) {
	testutil.Call(t, testHandler.DiscardDeadBranches,
		newRequestAsUser("00000000-0000-0000-0000-000000000042",
			http.MethodPost, "/api/runs/dead-branches/discard", map[string]any{"task_ids": []string{}})).
		Want(http.StatusNotFound)
}

// --- branch_gc_tick ---------------------------------------------------------

// setBranchGCSettings writes the workspace's branch_gc block for the duration
// of one test.
func setBranchGCSettings(t *testing.T, settings string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || $2::jsonb WHERE id = $1`,
		testWorkspaceID, settings)
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE workspace SET settings = settings - 'branch_gc' WHERE id = $1`, testWorkspaceID)
	})
}

func deadBranchDiscardRequests(t *testing.T, taskID string) int {
	t.Helper()
	return dbfx.Count(t, `SELECT count(*) FROM run_branch_action_request WHERE task_id = $1 AND action = 'discard'`, taskID)
}

// Opt-in by default: a workspace that never configured branch_gc is never
// swept, however old its dead branches are.
func TestTickBranchGCDisabledWorkspaceIsNoOp(t *testing.T) {
	setBranchGCSettings(t, `{"branch_gc":{"enabled":false}}`)
	f := newBranchActionFixture(t)
	dbfx.Exec(t, `UPDATE agent_task_queue SET completed_at = now() - interval '90 days' WHERE id = $1`, f.TaskID)

	if n, err := testHandler.TickBranchGC(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("tick = (%d, %v), want (0, nil) for a disabled workspace", n, err)
	}
	if got := deadBranchDiscardRequests(t, f.TaskID); got != 0 {
		t.Fatalf("a disabled workspace queued %d discard(s)", got)
	}
}

// Enabled, but the run finished inside the TTL: nothing yet.
func TestTickBranchGCLeavesYoungRunsAlone(t *testing.T) {
	setBranchGCSettings(t, `{"branch_gc":{"enabled":true,"ttl_days":30}}`)
	f := newBranchActionFixture(t)
	dbfx.Exec(t, `UPDATE agent_task_queue SET completed_at = now() - interval '1 day' WHERE id = $1`, f.TaskID)

	if n, err := testHandler.TickBranchGC(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("tick = (%d, %v), want (0, nil) for a run inside the TTL", n, err)
	}
	if got := deadBranchDiscardRequests(t, f.TaskID); got != 0 {
		t.Fatalf("a run inside the TTL queued %d discard(s)", got)
	}
}

// Enabled and past the TTL: one discard request through the branch-action
// channel, audited as the system. A second tick over the unchanged workspace
// enqueues nothing — the in-flight guard is the idempotency.
func TestTickBranchGCEnqueuesOldRunsOnce(t *testing.T) {
	setBranchGCSettings(t, `{"branch_gc":{"enabled":true,"ttl_days":30}}`)
	f := newBranchActionFixture(t)
	dbfx.Exec(t, `UPDATE agent_task_queue SET completed_at = now() - interval '40 days' WHERE id = $1`, f.TaskID)
	t.Cleanup(func() {
		dbfx.Exec(t, `DELETE FROM run_branch_action_request WHERE task_id = $1`, f.TaskID)
	})

	if n, err := testHandler.TickBranchGC(t.Context(), time.Now()); err != nil || n != 1 {
		t.Fatalf("first tick = (%d, %v), want (1, nil)", n, err)
	}
	var action, status, actorType string
	dbfx.QueryRow(t, `SELECT r.action, r.status, a.actor_type
		FROM run_branch_action_request r, audit_log_entry a
		WHERE r.task_id = $1 AND a.entity_id = r.task_id AND a.action = $2`,
		f.TaskID, AuditRunBranchAction).Scan(&action, &status, &actorType)
	if action != "discard" || status != runBranchActionStatusPending {
		t.Fatalf("request row = (%q, %q), want (discard, pending)", action, status)
	}
	if actorType != "system" {
		t.Fatalf("the sweep's audit actor = %q, want system", actorType)
	}

	if n, err := testHandler.TickBranchGC(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("second tick = (%d, %v), want (0, nil) — no double enqueue", n, err)
	}
	if got := deadBranchDiscardRequests(t, f.TaskID); got != 1 {
		t.Fatalf("discard requests after the second tick = %d, want 1", got)
	}
}

// A dead branch whose daemon cannot act stays put: the sweep skips it instead
// of enqueueing a request nobody can claim.
func TestTickBranchGCSkipsUnactionableRuns(t *testing.T) {
	setBranchGCSettings(t, `{"branch_gc":{"enabled":true,"ttl_days":30}}`)
	f := newBranchActionFixture(t)
	dbfx.Exec(t, `UPDATE agent_task_queue SET completed_at = now() - interval '40 days' WHERE id = $1`, f.TaskID)
	dbfx.Exec(t, `UPDATE agent_runtime SET status = 'offline' WHERE id = $1`, f.RuntimeID)

	if n, err := testHandler.TickBranchGC(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("tick = (%d, %v), want (0, nil) with the runtime offline", n, err)
	}
	if got := deadBranchDiscardRequests(t, f.TaskID); got != 0 {
		t.Fatalf("an unactionable run queued %d discard(s)", got)
	}
}
