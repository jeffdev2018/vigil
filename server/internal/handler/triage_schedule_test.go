package handler

// Scheduled autopilot runs are gated by their own triage source
// (autopilot_schedule), independently of the same autopilot's webhook source:
// unattended cron output is exactly the material a workspace wants to hold
// while a signed webhook stays direct.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/triage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func setTriageSourceModeForKind(t *testing.T, kind, refID, mode string) {
	t.Helper()
	ctx := context.Background()
	src, err := testHandler.Queries.UpsertTriageSource(ctx, db.UpsertTriageSourceParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Kind:        kind,
		RefID:       parseUUID(refID),
		Name:        kind + " test source",
	})
	if err != nil {
		t.Fatalf("upsert triage source: %v", err)
	}
	if _, err := testHandler.Queries.UpdateTriageSourceMode(ctx, db.UpdateTriageSourceModeParams{
		ID: src.ID, WorkspaceID: parseUUID(testWorkspaceID), Mode: mode,
	}); err != nil {
		t.Fatalf("set source mode %s: %v", mode, err)
	}
}

func cleanupTriageSourceKind(t *testing.T, kind, refID string) {
	t.Helper()
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM triage_item WHERE source_id IN (
				SELECT id FROM triage_source WHERE workspace_id = $1 AND kind = $2 AND ref_id = $3)`,
			testWorkspaceID, kind, refID)
		testPool.Exec(context.Background(),
			`DELETE FROM triage_source WHERE workspace_id = $1 AND kind = $2 AND ref_id = $3`,
			testWorkspaceID, kind, refID)
	})
}

func triageItemsForSource(t *testing.T, kind, refID string) []db.TriageItem {
	t.Helper()
	src, err := testHandler.Queries.GetTriageSourceByRef(context.Background(), db.GetTriageSourceByRefParams{
		WorkspaceID: parseUUID(testWorkspaceID), Kind: kind, RefID: parseUUID(refID),
	})
	if err != nil {
		t.Fatalf("load triage source %s/%s: %v", kind, refID, err)
	}
	rows, err := testPool.Query(context.Background(),
		`SELECT id, state, shadow, title FROM triage_item WHERE source_id = $1 ORDER BY first_seen_at`, src.ID)
	if err != nil {
		t.Fatalf("query items: %v", err)
	}
	defer rows.Close()
	var out []db.TriageItem
	for rows.Next() {
		var item db.TriageItem
		if err := rows.Scan(&item.ID, &item.State, &item.Shadow, &item.Title); err != nil {
			t.Fatalf("scan item: %v", err)
		}
		out = append(out, item)
	}
	return out
}

// createScheduleTriggerViaHandler publishes a cron trigger the same way the UI
// does, so the run's authority resolves to the acting member.
func createScheduleTriggerViaHandler(t *testing.T, autopilotID string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest(http.MethodPost, "/api/autopilots/"+autopilotID+"/triggers", map[string]any{
		"kind": "schedule", "cron_expression": "0 * * * *", "timezone": "UTC",
	})
	req = withURLParam(req, "id", autopilotID)
	testHandler.CreateAutopilotTrigger(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateAutopilotTrigger: expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	var resp AutopilotTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode trigger: %v", err)
	}
	return resp.ID
}

// scheduleTestAutopilot builds an autopilot with a real, owned schedule
// trigger: dispatch resolves the run's authority from the trigger's publisher,
// so a trigger-less dispatch is refused before it ever reaches triage.
func scheduleTestAutopilot(t *testing.T, name string) (apID, triggerID string) {
	t.Helper()
	agentID := createWebhookTestAgent(t, name+" Agent")
	apID = createWebhookTestAutopilot(t, agentID, "active", "create_issue")
	triggerID = createScheduleTriggerViaHandler(t, apID)
	cleanupTriageSourceKind(t, triage.SourceAutopilotSchedule, apID)
	cleanupAcceptedIssues(t, apID)
	return apID, triggerID
}

func dispatchScheduleRun(t *testing.T, apID, triggerID string) {
	t.Helper()
	ap, err := testHandler.Queries.GetAutopilotInWorkspace(context.Background(), db.GetAutopilotInWorkspaceParams{
		ID: parseUUID(apID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load autopilot: %v", err)
	}
	if _, err := testHandler.AutopilotService.DispatchAutopilot(context.Background(), ap, parseUUID(triggerID), "schedule", nil); err != nil {
		t.Fatalf("dispatch scheduled run: %v", err)
	}
}

func TestScheduledRunIsGatedByItsOwnTriageSource(t *testing.T) {
	apID, triggerID := scheduleTestAutopilot(t, "TriageSchedGate")
	setIssueTitleTemplate(t, apID, "Nightly backup verification")
	setTriageSourceModeForKind(t, triage.SourceAutopilotSchedule, apID, string(triage.ModeGate))

	dispatchScheduleRun(t, apID, triggerID)

	if got := countAutopilotIssues(t, apID); got != 0 {
		t.Fatalf("issues created by a gated scheduled run = %d, want 0 (the run must be parked)", got)
	}
	items := triageItemsForSource(t, triage.SourceAutopilotSchedule, apID)
	if len(items) != 1 {
		t.Fatalf("triage items for the schedule source = %d, want 1", len(items))
	}
	if items[0].State != triage.StatePending || items[0].Shadow {
		t.Fatalf("gated schedule item = state %q shadow %v, want pending and not shadow", items[0].State, items[0].Shadow)
	}
	if items[0].Title != "Nightly backup verification" {
		t.Fatalf("parked title = %q, want the interpolated issue title", items[0].Title)
	}
}

func TestScheduledRunInDirectModeStillCreatesTheIssue(t *testing.T) {
	apID, triggerID := scheduleTestAutopilot(t, "TriageSchedDirect")
	setIssueTitleTemplate(t, apID, "Direct scheduled sweep")
	setTriageSourceModeForKind(t, triage.SourceAutopilotSchedule, apID, string(triage.ModeDirect))

	dispatchScheduleRun(t, apID, triggerID)

	if got := countAutopilotIssues(t, apID); got != 1 {
		t.Fatalf("issues created by a direct scheduled run = %d, want 1", got)
	}
	items := triageItemsForSource(t, triage.SourceAutopilotSchedule, apID)
	if len(items) != 1 {
		t.Fatalf("triage items for the schedule source = %d, want 1 shadow measurement", len(items))
	}
	if !items[0].Shadow {
		t.Fatal("a direct-mode scheduled run must record a shadow item, not a queue entry")
	}
}

func TestScheduledRunIsBlockedWithoutTouchingTheWebhookSource(t *testing.T) {
	apID, triggerID := scheduleTestAutopilot(t, "TriageSchedBlocked")
	cleanupTriageSourceKind(t, triage.SourceAutopilotWebhook, apID)
	setIssueTitleTemplate(t, apID, "Blocked scheduled sweep")
	setTriageSourceModeForKind(t, triage.SourceAutopilotSchedule, apID, string(triage.ModeBlocked))
	// The same autopilot's webhook source stays direct: the two entry points
	// are gated independently.
	setTriageSourceModeForKind(t, triage.SourceAutopilotWebhook, apID, string(triage.ModeDirect))

	dispatchScheduleRun(t, apID, triggerID)

	if got := countAutopilotIssues(t, apID); got != 0 {
		t.Fatalf("issues created by a blocked scheduled run = %d, want 0", got)
	}
	items := triageItemsForSource(t, triage.SourceAutopilotSchedule, apID)
	if len(items) != 1 || items[0].State != triage.StateDropped {
		t.Fatalf("blocked schedule items = %+v, want one dropped audit row", items)
	}
	if len(triageItemsForSource(t, triage.SourceAutopilotWebhook, apID)) != 0 {
		t.Fatal("a scheduled run must not write to the webhook source")
	}
}
