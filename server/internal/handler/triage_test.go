package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// cleanupTriageSource removes a triage_source and everything captured
// against it. There are no FKs, so the order is irrelevant.
func cleanupTriageSource(t *testing.T, sourceID string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM triage_item WHERE source_id = $1`, sourceID)
	dbfx.Cleanup(t, `DELETE FROM triage_source WHERE id = $1`, sourceID)
}

func shadowWebhookParams(refID, title, state string) triage.CaptureParams {
	return triage.CaptureParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		SourceKind:     triage.SourceAutopilotWebhook,
		SourceRefID:    parseUUID(refID),
		SourceName:     "Sentry alerts",
		OriginType:     "autopilot",
		OriginID:       parseUUID(refID),
		Title:          title,
		TriggerPayload: []byte(`{"alert":"payment-gateway"}`),
		State:          state,
		Shadow:         true,
	}
}

func TestTriageCaptureCollapsesSameTitle(t *testing.T) {
	refID := uuid.NewString()

	first, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "Payment gateway timeout", triage.StatePending))
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(first.SourceID))

	// Same title with whitespace/case noise, second delivery: no new row,
	// the existing pending item absorbs it.
	second, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "  payment   GATEWAY\ttimeout ", triage.StatePending))
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if util.UUIDToString(second.ID) != util.UUIDToString(first.ID) {
		t.Fatalf("second capture created %s, want the existing item %s", second.ID, first.ID)
	}
	if second.CollapseCount != 2 {
		t.Fatalf("collapse_count = %d, want 2", second.CollapseCount)
	}
	if got := dbfx.Count(t, `SELECT COUNT(*) FROM triage_item WHERE source_id = $1`, util.UUIDToString(first.SourceID)); got != 1 {
		t.Fatalf("items for the source = %d, want 1 collapsed row", got)
	}
}

func TestTriageCaptureDistinctTitlesStaySeparate(t *testing.T) {
	refID := uuid.NewString()

	first, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "Payment gateway timeout", triage.StatePending))
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(first.SourceID))

	second, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "Refund webhook 500", triage.StatePending))
	if err != nil {
		t.Fatalf("second capture: %v", err)
	}
	if util.UUIDToString(second.ID) == util.UUIDToString(first.ID) {
		t.Fatal("distinct titles collapsed onto one item")
	}
}

func TestTriageDropCaptureNeverCollapses(t *testing.T) {
	refID := uuid.NewString()

	params := shadowWebhookParams(refID, "", triage.StateDropped)
	params.DropReason = "issue_limit_reached"

	first, err := triage.Capture(context.Background(), testHandler.Queries, params)
	if err != nil {
		t.Fatalf("first drop capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(first.SourceID))

	second, err := triage.Capture(context.Background(), testHandler.Queries, params)
	if err != nil {
		t.Fatalf("second drop capture: %v", err)
	}
	if util.UUIDToString(second.ID) == util.UUIDToString(first.ID) {
		t.Fatal("dropped deliveries must stay individual audit rows")
	}
	if !second.DropReason.Valid || second.DropReason.String != "issue_limit_reached" {
		t.Fatalf("drop_reason = %+v, want issue_limit_reached", second.DropReason)
	}
}

func TestTriageSourceUpsertRefreshesName(t *testing.T) {
	refID := uuid.NewString()

	first, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "first", triage.StatePending))
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(first.SourceID))

	renamed := shadowWebhookParams(refID, "second", triage.StatePending)
	renamed.SourceName = "PagerDuty alerts"
	if _, err := triage.Capture(context.Background(), testHandler.Queries, renamed); err != nil {
		t.Fatalf("second capture: %v", err)
	}

	sources, err := testHandler.Queries.ListTriageSources(context.Background(), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("list sources: %v", err)
	}
	var found []db.TriageSource
	for _, src := range sources {
		if util.UUIDToString(src.RefID) == refID {
			found = append(found, src)
		}
	}
	if len(found) != 1 {
		t.Fatalf("sources for ref %s = %d, want one upserted row", refID, len(found))
	}
	if found[0].Name != "PagerDuty alerts" {
		t.Fatalf("source name = %q, want the refreshed name", found[0].Name)
	}
}

func TestDeleteExpiredTriageItems(t *testing.T) {
	itemID := dbfx.Insert(t, "triage_item", testutil.Cols{
		"workspace_id":     testWorkspaceID,
		"source_id":        uuid.NewString(),
		"origin_type":      "autopilot",
		"title":            "expired item",
		"normalized_title": "expired item",
		"state":            "pending",
		"shadow":           true,
		"expires_at":       time.Now().Add(-time.Hour).UTC(),
	})

	n, err := testHandler.Queries.DeleteExpiredTriageItems(context.Background(), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n == 0 {
		t.Fatal("expired item was not purged")
	}
	if got := dbfx.Count(t, `SELECT COUNT(*) FROM triage_item WHERE id = $1`, itemID); got != 0 {
		t.Fatal("expired item row survived the purge")
	}
}

func TestGetTriageStatsCountsShadowAndDrops(t *testing.T) {
	refID := uuid.NewString()

	pendingItem, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "Payment gateway timeout", triage.StatePending))
	if err != nil {
		t.Fatalf("pending capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(pendingItem.SourceID))

	dropped := shadowWebhookParams(refID, "", triage.StateDropped)
	dropped.DropReason = "issue_limit_reached"
	if _, err := triage.Capture(context.Background(), testHandler.Queries, dropped); err != nil {
		t.Fatalf("drop capture: %v", err)
	}

	var out TriageStatsResponse
	testutil.Call(t, testHandler.GetTriageStats, newRequest(http.MethodGet, "/api/triage/stats", nil)).
		Want(http.StatusOK).JSON(&out)

	// M1 is shadow-only: nothing occupies the real queue yet.
	if out.Pending != 0 {
		t.Fatalf("pending = %d, want 0 while every capture is shadow", out.Pending)
	}
	if out.ShadowPending != 1 {
		t.Fatalf("shadow_pending = %d, want 1", out.ShadowPending)
	}
	if out.Dropped24h != 1 {
		t.Fatalf("dropped_24h = %d, want 1", out.Dropped24h)
	}
	if out.OldestPendingAgeSeconds != 0 {
		t.Fatalf("oldest_pending_age_seconds = %d, want 0 with no real pending", out.OldestPendingAgeSeconds)
	}
	if out.Sources == nil {
		t.Fatal("sources must serialize as [], never null")
	}

	var stats *TriageSourceStats
	for i := range out.Sources {
		if out.Sources[i].RefID == refID {
			stats = &out.Sources[i]
		}
	}
	if stats == nil {
		t.Fatalf("source %s missing from stats: %+v", refID, out.Sources)
	}
	if stats.Kind != triage.SourceAutopilotWebhook || stats.Mode != "direct" {
		t.Fatalf("source stats = %+v, want kind autopilot_webhook mode direct", stats)
	}
	if stats.Items24h != 2 || stats.Dropped24h != 1 {
		t.Fatalf("source 24h = items %d dropped %d, want 2 and 1", stats.Items24h, stats.Dropped24h)
	}
}
