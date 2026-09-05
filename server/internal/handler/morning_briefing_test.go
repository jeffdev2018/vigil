package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Morning briefing (K30): three sections from live data, one send per
// workspace and day, gated by the workspace's local hour.

func setBriefingSettings(t *testing.T, enabled bool, hour int, tz string) {
	t.Helper()
	rememberSettings(t)
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || jsonb_build_object('morning_briefing', jsonb_build_object('enabled', $2::bool, 'hour', $3::int, 'timezone', $4::text)) WHERE id = $1`, testWorkspaceID, enabled, hour, tz)
}

func briefingCall(t *testing.T, h http.HandlerFunc, method, path string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, inboxWorkspaceHandler(h), testutil.WithHeaders(newRequest(method, path, nil), "X-Workspace-ID", testWorkspaceID))
}

func TestMorningBriefingComposesSectionsAndSendsOnce(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM morning_briefing_sent WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(t.Context(), `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'morning_briefing'`, testWorkspaceID)
	})
	setBriefingSettings(t, true, 0, "UTC")
	done := dbfx.Issue(t, "briefing done", testutil.Cols{"status": "done"})
	old := dbfx.Issue(t, "briefing old done", testutil.Cols{"status": "done"})
	dbfx.Exec(t, `UPDATE issue SET completed_at = now() - interval '3 days' WHERE id = $1`, old)
	review := dbfx.Issue(t, "briefing review", testutil.Cols{"status": "in_review"})
	blockedIssue, task := completedAgentRun(t, "briefing blocked")
	dbfx.Exec(t, `UPDATE issue SET status = 'blocked' WHERE id = $1`, blockedIssue)
	dbfx.Exec(t, `UPDATE agent_task_queue SET error = 'tests failed on CI' WHERE id = $1`, task)
	asked := dbfx.Issue(t, "briefing asked", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_decision WHERE issue_id = $1`, asked)
		testPool.Exec(t.Context(), `DELETE FROM inbox_item WHERE issue_id = $1`, asked)
	})
	askDecision(t, asked, decisionBody()).Want(http.StatusCreated)

	var b MorningBriefingResponse
	briefingCall(t, testHandler.GetMorningBriefingToday, http.MethodGet, "/api/morning-briefing/today").Want(http.StatusOK).JSON(&b)
	has := func(items []BriefingItem, id string) *BriefingItem {
		for i := range items {
			if items[i].IssueID == id {
				return &items[i]
			}
		}
		return nil
	}
	if has(b.Merged, done) == nil || has(b.Merged, old) != nil {
		t.Fatalf("merged = %+v, want the issue done today and not the 3-day-old one", b.Merged)
	}
	if has(b.AwaitingReview, review) == nil {
		t.Fatalf("awaiting review = %+v", b.AwaitingReview)
	}
	if bl := has(b.Blocked, blockedIssue); bl == nil || bl.Reason != "tests failed on CI" {
		t.Fatalf("blocked = %+v, want the run's error as the reason", b.Blocked)
	}
	if bl := has(b.Blocked, asked); bl == nil || bl.PendingDecisions != 1 || bl.Reason != "Drop the legacy table?" {
		t.Fatalf("blocked by a card = %+v", has(b.Blocked, asked))
	}
	if b.SentAt != nil {
		t.Fatal("nothing sent yet")
	}

	// Trigger sends once: every member gets one item; a second trigger says so.
	var sent MorningBriefingResponse
	briefingCall(t, testHandler.TriggerMorningBriefing, http.MethodPost, "/api/morning-briefing/trigger").Want(http.StatusOK).JSON(&sent)
	if sent.AlreadySent || sent.SentAt == nil {
		t.Fatalf("first trigger = %+v", sent)
	}
	members := dbfx.Count(t, `SELECT COUNT(*) FROM member WHERE workspace_id = $1`, testWorkspaceID)
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'morning_briefing' AND created_at > now() - interval '1 minute'`, testWorkspaceID); n != members {
		t.Fatalf("briefing inbox items = %d, want one per member (%d)", n, members)
	}
	briefingCall(t, testHandler.TriggerMorningBriefing, http.MethodPost, "/api/morning-briefing/trigger").Want(http.StatusOK).JSON(&sent)
	if !sent.AlreadySent {
		t.Fatal("second trigger must report already_sent")
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'morning_briefing' AND created_at > now() - interval '1 minute'`, testWorkspaceID); n != members {
		t.Fatalf("items after the second trigger = %d, want unchanged", n)
	}
	// The scheduler sees the day as sent.
	if n, err := testHandler.SendDueMorningBriefings(t.Context(), time.Now()); err != nil || n != 0 {
		t.Fatalf("scheduler after a manual send moved %d (err %v), want 0", n, err)
	}
}

func TestMorningBriefingSchedulerWaitsForTheLocalHour(t *testing.T) {
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM morning_briefing_sent WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(t.Context(), `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'morning_briefing'`, testWorkspaceID)
	})
	// Disabled: nothing, whatever the hour.
	setBriefingSettings(t, false, 0, "UTC")
	if n, _ := testHandler.SendDueMorningBriefings(t.Context(), time.Now()); n != 0 {
		t.Fatalf("disabled workspace sent %d", n)
	}
	// Enabled at 09:00 Tokyo: 08:00 Tokyo is too early, 09:30 sends, 10:00 is a no-op.
	setBriefingSettings(t, true, 9, "Asia/Tokyo")
	tokyo, _ := time.LoadLocation("Asia/Tokyo")
	day := time.Date(2026, 9, 10, 8, 0, 0, 0, tokyo)
	if n, _ := testHandler.SendDueMorningBriefings(t.Context(), day); n != 0 {
		t.Fatalf("08:00 local sent %d, want 0", n)
	}
	if n, _ := testHandler.SendDueMorningBriefings(t.Context(), day.Add(90*time.Minute)); n < 1 {
		t.Fatalf("09:30 local sent %d, want at least this workspace", n)
	}
	var date string
	dbfx.QueryRow(t, `SELECT sent_for_date::text FROM morning_briefing_sent WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT 1`, testWorkspaceID).Scan(&date)
	if date != "2026-09-10" {
		t.Fatalf("sent_for_date = %q, want the Tokyo date", date)
	}
	if n, _ := testHandler.SendDueMorningBriefings(t.Context(), day.Add(2*time.Hour)); n != 0 {
		t.Fatalf("10:00 local sent %d again, want 0", n)
	}
	// Workspace deletion purges the log.
	ws := dbfx.Workspace(t, "Briefing purge", "briefing-purge-"+uuid.NewString())
	dbfx.Insert(t, "morning_briefing_sent", testutil.Cols{"workspace_id": ws, "sent_for_date": "2026-09-01"})
	if err := testHandler.Queries.DeleteWorkspaceIssueRoots(t.Context(), parseUUID(ws)); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM morning_briefing_sent WHERE workspace_id = $1`, ws); n != 0 {
		t.Fatalf("rows after workspace delete = %d", n)
	}
}
