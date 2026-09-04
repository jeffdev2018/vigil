package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Standup and retro (K34): a stale blocked issue asks its owner once a day;
// the weekly retro groups real runs, is generated once per week and
// regenerated at most hourly.

func setStandupSettings(t *testing.T, enabled bool, hours int, retro bool) {
	t.Helper()
	rememberSettings(t)
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || jsonb_build_object('standup', jsonb_build_object('enabled', $2::bool, 'blocked_hours', $3::int, 'weekly_retro', $4::bool)) WHERE id = $1`, testWorkspaceID, enabled, hours, retro)
}

func TestStandupAsksTheRightPersonOnceADay(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'standup_question'`, testWorkspaceID)
	})
	owner := dbfx.User(t, "Standup owner", "standup-"+uuid.NewString()[:8]+"@multica.ai")
	dbfx.Member(t, testWorkspaceID, owner, "member")
	stale := dbfx.Issue(t, "standup stale", testutil.Cols{"status": "blocked", "assignee_type": "member", "assignee_id": owner})
	fresh := dbfx.Issue(t, "standup fresh", testutil.Cols{"status": "blocked", "assignee_type": "member", "assignee_id": owner})
	orphan := dbfx.Issue(t, "standup orphan", testutil.Cols{"status": "blocked"})
	dbfx.Exec(t, `UPDATE issue SET updated_at = now() - interval '30 hours' WHERE id = ANY($1::uuid[])`, []string{stale, orphan})

	// Off: nothing.
	setStandupSettings(t, false, 24, false)
	if n, err := testHandler.RunStandup(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("disabled standup = %d, %v", n, err)
	}
	setStandupSettings(t, true, 24, false)
	n, err := testHandler.RunStandup(t.Context(), time.Now())
	if err != nil || n < 2 {
		t.Fatalf("standup = %d, %v; want the stale issue asked to its assignee and the orphan to its creator", n, err)
	}
	if c := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'standup_question' AND recipient_id = $2`, stale, owner); c != 1 {
		t.Fatalf("owner questions = %d, want 1", c)
	}
	if c := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'standup_question'`, fresh); c != 0 {
		t.Fatal("a freshly blocked issue must not be asked about")
	}
	if c := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'standup_question' AND recipient_id = $2`, orphan, testUserID); c != 1 {
		t.Fatalf("orphan questions to the creator = %d, want 1", c)
	}
	// Same day: nothing new.
	if n, err := testHandler.RunStandup(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("second standup = %d, %v; want 0", n, err)
	}
	// The question sits in the Attention Inbox of its recipient.
	var out struct {
		Items []AttentionInboxItem `json:"items"`
	}
	testutil.Call(t, inboxWorkspaceHandler(testHandler.ListAttentionInbox),
		inboxRequest(http.MethodGet, "/api/inbox/attention", testWorkspaceID)).Want(http.StatusOK).JSON(&out)
	found := false
	for _, it := range out.Items {
		found = found || (it.Type == "standup_question" && it.IssueID != nil && *it.IssueID == orphan)
	}
	if !found {
		t.Fatal("the standup question must be in the attention inbox")
	}
}

func TestWeeklyRetroGroupsRunsAndRateLimits(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM weekly_retro WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(context.Background(), `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'weekly_retro'`, testWorkspaceID)
	})
	setStandupSettings(t, false, 24, true)
	now := time.Now()
	lastWeek := weekStartOf(now, time.UTC).AddDate(0, 0, -7).Add(36 * time.Hour)
	_, done := completedAgentRun(t, "retro done")
	failedIssue, failed := completedAgentRun(t, "retro failed")
	dbfx.Exec(t, `UPDATE agent_task_queue SET created_at = $2::timestamptz, started_at = $2::timestamptz, completed_at = $2::timestamptz + interval '20 minutes' WHERE id = $1`, done, lastWeek)
	dbfx.Exec(t, `UPDATE agent_task_queue SET created_at = $2::timestamptz, started_at = $2::timestamptz, completed_at = $2::timestamptz + interval '5 minutes', status = 'failed', error = 'tests red' WHERE id = $1`, failed, lastWeek.Add(time.Hour))

	if n, err := testHandler.GenerateDueWeeklyRetros(t.Context(), now); err != nil || n != 1 {
		t.Fatalf("generate = %d, %v; want 1", n, err)
	}
	if n, err := testHandler.GenerateDueWeeklyRetros(t.Context(), now); err != nil || n != 0 {
		t.Fatalf("second generate = %d, %v; want 0 (one per week)", n, err)
	}
	if c := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'weekly_retro' AND recipient_id = $2`, testWorkspaceID, testUserID); c != 1 {
		t.Fatalf("lead retro items = %d, want 1", c)
	}
	var retro WeeklyRetroResponse
	briefingCall(t, testHandler.GetWeeklyRetro, http.MethodGet, "/api/retro/weekly").Want(http.StatusOK).JSON(&retro)
	if retro.RunsTotal < 2 || retro.RunsByStatus["failed"] < 1 || retro.RunsByStatus["completed"] < 1 || retro.GeneratedAt == nil {
		t.Fatalf("retro = %+v", retro)
	}
	hasFailed := false
	for _, f := range retro.Failed {
		hasFailed = hasFailed || (f.IssueID == failedIssue && f.Error == "tests red" && f.Identifier != "")
	}
	if !hasFailed || retro.MedianMinutes <= 0 {
		t.Fatalf("failed runs = %+v, median %v", retro.Failed, retro.MedianMinutes)
	}
	briefingCall(t, testHandler.GetWeeklyRetro, http.MethodGet, "/api/retro/weekly?week="+lastWeek.Format("2006-01-02")).Want(http.StatusOK)
	briefingCall(t, testHandler.GetWeeklyRetro, http.MethodGet, "/api/retro/weekly?week=2000-01-03").Want(http.StatusNotFound)

	// Regenerate: once an hour.
	regen := func() *testutil.Response {
		return testutil.Call(t, inboxWorkspaceHandler(testHandler.RegenerateWeeklyRetro),
			testutil.WithHeaders(newRequest(http.MethodPost, "/api/retro/weekly/regenerate", map[string]any{}), "X-Workspace-ID", testWorkspaceID))
	}
	if res := regen(); res.Code != http.StatusTooManyRequests || res.Map()["code"] != "retro_rate_limited" {
		t.Fatalf("regenerate within the hour: %d %s", res.Code, res.Text())
	}
	dbfx.Exec(t, `UPDATE weekly_retro SET generated_at = now() - interval '2 hours' WHERE workspace_id = $1`, testWorkspaceID)
	regen().Want(http.StatusOK)
	if c := dbfx.Count(t, `SELECT COUNT(*) FROM weekly_retro WHERE workspace_id = $1`, testWorkspaceID); c != 1 {
		t.Fatalf("retro rows = %d, want 1 (upsert)", c)
	}
}
