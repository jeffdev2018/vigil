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

	first, _, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "Payment gateway timeout", triage.StatePending))
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(first.SourceID))

	// Same title with whitespace/case noise, second delivery: no new row,
	// the existing pending item absorbs it.
	second, _, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "  payment   GATEWAY\ttimeout ", triage.StatePending))
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

	first, _, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "Payment gateway timeout", triage.StatePending))
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(first.SourceID))

	second, _, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "Refund webhook 500", triage.StatePending))
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

	first, _, err := triage.Capture(context.Background(), testHandler.Queries, params)
	if err != nil {
		t.Fatalf("first drop capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(first.SourceID))

	second, _, err := triage.Capture(context.Background(), testHandler.Queries, params)
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

	first, _, err := triage.Capture(context.Background(), testHandler.Queries, shadowWebhookParams(refID, "first", triage.StatePending))
	if err != nil {
		t.Fatalf("first capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(first.SourceID))

	renamed := shadowWebhookParams(refID, "second", triage.StatePending)
	renamed.SourceName = "PagerDuty alerts"
	if _, _, err := triage.Capture(context.Background(), testHandler.Queries, renamed); err != nil {
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

// Retention sweep: triage.Capture has always stamped expires_at, but nothing
// ever read it. A pending item past its window leaves the queue as `expired`
// — not deleted: the resolved rows are the auto-classifier's examples (K61).
func TestExpireStaleTriageItems(t *testing.T) {
	sourceID := uuid.NewString()
	plant := func(title, state string, expiresAt time.Time) string {
		cols := testutil.Cols{
			"workspace_id":     testWorkspaceID,
			"source_id":        sourceID,
			"origin_type":      "autopilot",
			"title":            title,
			"normalized_title": title,
			"state":            state,
			"shadow":           false,
			"expires_at":       expiresAt,
		}
		if state != "pending" {
			cols["resolved_at"] = time.Now().UTC()
		}
		return dbfx.Insert(t, "triage_item", cols)
	}
	stale := plant("stale pending item", "pending", time.Now().Add(-time.Hour).UTC())
	fresh := plant("fresh pending item", "pending", time.Now().Add(time.Hour).UTC())
	resolved := plant("resolved item past its window", "dismissed", time.Now().Add(-time.Hour).UTC())

	n, err := testHandler.ExpireStaleTriageItems(context.Background())
	if err != nil {
		t.Fatalf("expire stale: %v", err)
	}
	if n == 0 {
		t.Fatal("the sweep reported no rows, want at least the stale pending item")
	}

	if got := triageState(t, stale); got != triage.StateExpired {
		t.Fatalf("stale item state = %s, want expired", got)
	}
	var reason string
	dbfx.QueryRow(t, `SELECT COALESCE(resolution_reason, '') FROM triage_item WHERE id = $1`, stale).Scan(&reason)
	if reason == "" {
		t.Fatal("an expired item must record why it left the queue")
	}
	if got := triageState(t, fresh); got != triage.StatePending {
		t.Fatalf("item inside its window = %s, want pending", got)
	}
	// Resolved history is the training set: the sweep must not touch it.
	if got := triageState(t, resolved); got != triage.StateDismissed {
		t.Fatalf("already-resolved item = %s, want dismissed", got)
	}
}

func TestGetTriageStatsCountsShadowAndDrops(t *testing.T) {
	// Stats are workspace-wide, so the shared test workspace would count
	// whatever other suites left pending; count in a workspace of our own.
	workspaceID := dbfx.Workspace(t, "Triage stats", "triage-stats-"+uuid.NewString())
	dbfx.Member(t, workspaceID, testUserID, "owner")
	refID := uuid.NewString()

	pendingParams := shadowWebhookParams(refID, "Payment gateway timeout", triage.StatePending)
	pendingParams.WorkspaceID = parseUUID(workspaceID)
	pendingItem, _, err := triage.Capture(context.Background(), testHandler.Queries, pendingParams)
	if err != nil {
		t.Fatalf("pending capture: %v", err)
	}
	cleanupTriageSource(t, util.UUIDToString(pendingItem.SourceID))

	dropped := shadowWebhookParams(refID, "", triage.StateDropped)
	dropped.WorkspaceID = parseUUID(workspaceID)
	dropped.DropReason = "issue_limit_reached"
	if _, _, err := triage.Capture(context.Background(), testHandler.Queries, dropped); err != nil {
		t.Fatalf("drop capture: %v", err)
	}

	var out TriageStatsResponse
	testutil.Call(t, testHandler.GetTriageStats,
		testutil.WithHeaders(newRequest(http.MethodGet, "/api/triage/stats", nil), "X-Workspace-ID", workspaceID)).
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

// A resolved item carries why it was resolved — "auto: 92% confidence …" for
// the auto-classifier, the rule title for a rule, the human's reason for a
// manual dismiss. The history tabs render it, so the list must expose it.
func TestListTriageItemsExposesResolutionReason(t *testing.T) {
	itemID := dbfx.Insert(t, "triage_item", testutil.Cols{
		"workspace_id":      testWorkspaceID,
		"source_id":         uuid.NewString(),
		"origin_type":       "autopilot",
		"title":             "auto-dismissed delivery",
		"normalized_title":  "auto-dismissed delivery",
		"state":             triage.StateDismissed,
		"shadow":            false,
		"resolved_at":       time.Now().UTC(),
		"resolution_reason": "auto: 92% confidence from 10 similar deliveries",
	})

	var out struct {
		Items []TriageItemResponse `json:"items"`
	}
	testutil.Call(t, testHandler.ListTriageItems,
		newRequest(http.MethodGet, "/api/triage/items?state=dismissed&limit=100", nil),
	).Want(http.StatusOK).JSON(&out)

	for _, item := range out.Items {
		if item.ID != itemID {
			continue
		}
		if item.ResolutionReason != "auto: 92% confidence from 10 similar deliveries" {
			t.Fatalf("resolution_reason = %q, want the stored reason", item.ResolutionReason)
		}
		return
	}
	t.Fatalf("dismissed item %s missing from state=dismissed listing", itemID)
}

// The queue view links a meeting-born item back at its meeting, so the item's
// origin_id has to leave the API — origin_type alone names the kind, not the
// object.
func TestListTriageItemsExposesOriginID(t *testing.T) {
	meetingID := uuid.NewString()
	itemID := dbfx.Insert(t, "triage_item", testutil.Cols{
		"workspace_id":     testWorkspaceID,
		"source_id":        uuid.NewString(),
		"origin_type":      "meeting",
		"origin_id":        meetingID,
		"title":            "meeting action item",
		"normalized_title": "meeting action item",
		"state":            triage.StatePending,
		"shadow":           false,
	})

	var out struct {
		Items []TriageItemResponse `json:"items"`
	}
	testutil.Call(t, testHandler.ListTriageItems,
		newRequest(http.MethodGet, "/api/triage/items?state=pending&limit=100", nil),
	).Want(http.StatusOK).JSON(&out)

	for _, item := range out.Items {
		if item.ID != itemID {
			continue
		}
		if item.OriginType != "meeting" || item.OriginID != meetingID {
			t.Fatalf("origin = %s/%s, want meeting/%s", item.OriginType, item.OriginID, meetingID)
		}
		return
	}
	t.Fatalf("pending item %s missing from the listing", itemID)
}
