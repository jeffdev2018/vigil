package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func listTriageItemIDs(t *testing.T, query string) map[string]bool {
	t.Helper()
	var out struct {
		Items []TriageItemResponse `json:"items"`
	}
	testutil.Call(t, testHandler.ListTriageItems,
		newRequest(http.MethodGet, "/api/triage/items?"+query, nil),
	).Want(http.StatusOK).JSON(&out)
	ids := make(map[string]bool, len(out.Items))
	for _, item := range out.Items {
		ids[item.ID] = true
	}
	return ids
}

func TestTriageSnoozeHidesItemUntilDue(t *testing.T) {
	itemID := newPendingTriageItem(t, "snooze me "+uuid.NewString())
	until := time.Now().Add(2 * time.Hour).UTC()

	testutil.Call(t, testHandler.SnoozeTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/snooze",
			map[string]any{"until": until.Format(time.RFC3339)}),
		"id", itemID,
	)).Want(http.StatusOK)

	if listTriageItemIDs(t, "state=pending&limit=100")[itemID] {
		t.Fatal("snoozed item still in the default pending listing")
	}
	if !listTriageItemIDs(t, "state=pending&limit=100&include_snoozed=true")[itemID] {
		t.Fatal("snoozed item missing from ?include_snoozed=true")
	}

	var stats TriageStatsResponse
	testutil.Call(t, testHandler.GetTriageStats, newRequest(http.MethodGet, "/api/triage/stats", nil)).
		Want(http.StatusOK).JSON(&stats)
	if stats.Snoozed < 1 {
		t.Fatalf("stats.snoozed = %d, want at least the item just snoozed", stats.Snoozed)
	}

	// Due time passes: the listing stops hiding it and the sweep clears the
	// column so the item is announced again.
	dbfx.Exec(t, `UPDATE triage_item SET snoozed_until = now() - INTERVAL '1 minute' WHERE id = $1`, itemID)
	if !listTriageItemIDs(t, "state=pending&limit=100")[itemID] {
		t.Fatal("due item still hidden from the pending listing")
	}
	if _, err := testHandler.WakeDueSnoozedTriageItems(context.Background()); err != nil {
		t.Fatalf("wake snoozed: %v", err)
	}
	var snoozedUntil *time.Time
	if err := testPool.QueryRow(context.Background(),
		`SELECT snoozed_until FROM triage_item WHERE id = $1`, itemID).Scan(&snoozedUntil); err != nil {
		t.Fatalf("load woken item: %v", err)
	}
	if snoozedUntil != nil {
		t.Fatalf("snoozed_until = %v after the sweep, want NULL", snoozedUntil)
	}
}

func TestTriageSnoozeRejectsOutOfRangeTimes(t *testing.T) {
	itemID := newPendingTriageItem(t, "snooze bad "+uuid.NewString())
	for name, until := range map[string]string{
		"past":     time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		"too_far":  time.Now().Add(31 * 24 * time.Hour).UTC().Format(time.RFC3339),
		"nonsense": "tomorrow",
	} {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.SnoozeTriageItem, testutil.WithURLParams(
				newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/snooze",
					map[string]any{"until": until}),
				"id", itemID,
			)).Want(http.StatusBadRequest)
		})
	}
}
