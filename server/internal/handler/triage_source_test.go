package handler

// The per-source policy: the PATCH that writes it, and the three settings it
// unlocked — retention, the flood cap, and auto-accept.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/triage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// newPolicyTestSource registers a channel source to configure. Any kind would
// do; a channel is the one whose policy a human is most likely to touch.
func newPolicyTestSource(t *testing.T) db.TriageSource {
	t.Helper()
	refID := uuidToString(dbid.NewV7())
	cleanupTriageSourceKind(t, triage.SourceChannel, refID)
	src, err := testHandler.Queries.UpsertTriageSource(context.Background(), db.UpsertTriageSourceParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		Kind:        triage.SourceChannel,
		RefID:       parseUUID(refID),
		Name:        "Policy test channel",
		CreatedByID: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("seed source: %v", err)
	}
	return src
}

func patchTriageSource(t *testing.T, sourceID string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.UpdateTriageSourceSettings, testutil.WithURLParams(
		newRequest(http.MethodPatch, "/api/triage/sources/"+sourceID, body), "id", sourceID,
	))
}

func TestUpdateTriageSourceSettingsWritesTheWholePolicy(t *testing.T) {
	src := newPolicyTestSource(t)
	id := uuidToString(src.ID)

	var out TriageSourceResponse
	patchTriageSource(t, id, map[string]any{
		"mode": "gate", "auto_accept": true, "cap_per_hour": 25, "expiry_days": 3,
	}).Want(http.StatusOK).JSON(&out)

	if out.Mode != "gate" || !out.AutoAccept || out.CapPerHour != 25 || out.ExpiryDays != 3 {
		t.Fatalf("policy = %+v, want gate/auto/25/3", out)
	}

	// A partial patch must leave the rest alone: sending only `mode` used to be
	// the whole API, and it must not reset a cap somebody configured.
	patchTriageSource(t, id, map[string]any{"mode": "direct"}).Want(http.StatusOK).JSON(&out)
	if out.Mode != "direct" {
		t.Fatalf("mode = %q, want direct", out.Mode)
	}
	if out.CapPerHour != 25 || out.ExpiryDays != 3 || !out.AutoAccept {
		t.Fatalf("a mode-only patch reset the rest of the policy: %+v", out)
	}
}

func TestUpdateTriageSourceSettingsRejectsNonsense(t *testing.T) {
	id := uuidToString(newPolicyTestSource(t).ID)
	for _, body := range []map[string]any{
		{"mode": "bogus"},
		{"cap_per_hour": -1},
		{"expiry_days": 400},
	} {
		patchTriageSource(t, id, body).Want(http.StatusBadRequest)
	}
	patchTriageSource(t, uuidToString(dbid.NewV7()), map[string]any{"mode": "gate"}).Want(http.StatusNotFound)
}

func TestSourceExpiryDaysReplacesTheDefaultRetention(t *testing.T) {
	src := newPolicyTestSource(t)
	patchTriageSource(t, uuidToString(src.ID), map[string]any{"expiry_days": 2}).Want(http.StatusOK)

	item, _, err := triage.Capture(context.Background(), testHandler.Queries, triage.CaptureParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		SourceKind:  src.Kind,
		SourceRefID: src.RefID,
		SourceName:  src.Name,
		Title:       "expires in two days",
		State:       triage.StatePending,
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if !item.ExpiresAt.Valid {
		t.Fatal("capture must stamp expires_at")
	}
	got := time.Until(item.ExpiresAt.Time)
	if got < 47*time.Hour || got > 49*time.Hour {
		t.Fatalf("expires in %s, want ~48h from the source's expiry_days, not the 14d default", got)
	}
}

func TestSourceCapPerHourParksTheOverflowAsARateLimitedDrop(t *testing.T) {
	src := newPolicyTestSource(t)
	patchTriageSource(t, uuidToString(src.ID), map[string]any{"cap_per_hour": 1}).Want(http.StatusOK)

	capture := func(title string) db.TriageItem {
		t.Helper()
		item, _, err := triage.Capture(context.Background(), testHandler.Queries, triage.CaptureParams{
			WorkspaceID: parseUUID(testWorkspaceID),
			SourceKind:  src.Kind,
			SourceRefID: src.RefID,
			SourceName:  src.Name,
			Title:       title,
			State:       triage.StatePending,
		})
		if err != nil {
			t.Fatalf("capture %q: %v", title, err)
		}
		return item
	}

	if first := capture("under the cap"); first.State != triage.StatePending || first.Shadow {
		t.Fatalf("first item = state %q shadow %v, want a real pending row", first.State, first.Shadow)
	}
	second := capture("over the cap")
	if second.State != triage.StateDropped {
		t.Fatalf("second item state = %q, want dropped", second.State)
	}
	if second.DropReason.String != triage.DropReasonRateLimited {
		t.Fatalf("drop_reason = %q, want %q", second.DropReason.String, triage.DropReasonRateLimited)
	}
	if !second.Shadow {
		t.Fatal("a rate-limited item is an audit row, not a queue entry")
	}
}

func TestAutoAcceptResolvesTheItemIntoAnIssue(t *testing.T) {
	src := newPolicyTestSource(t)
	patchTriageSource(t, uuidToString(src.ID), map[string]any{"auto_accept": true}).Want(http.StatusOK)

	item, source, err := triage.Capture(context.Background(), testHandler.Queries, triage.CaptureParams{
		WorkspaceID: parseUUID(testWorkspaceID),
		SourceKind:  src.Kind,
		SourceRefID: src.RefID,
		SourceName:  src.Name,
		OriginType:  "slack_chat",
		Title:       "auto-accepted report",
		State:       triage.StatePending,
	})
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	testHandler.onTriageParked(context.Background(), item, source)

	var state string
	var issueID pgtype.UUID
	if err := testPool.QueryRow(context.Background(),
		`SELECT state, issue_id FROM triage_item WHERE id = $1`, item.ID).Scan(&state, &issueID); err != nil {
		t.Fatalf("reload item: %v", err)
	}
	if state != triage.StateAccepted || !issueID.Valid {
		t.Fatalf("item = state %q issue %v, want accepted with an issue", state, issueID)
	}
	testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
}

// The stats response is what the queue header renders, and it used to carry a
// source's mode and nothing else — no per-source pending count and none of the
// three policy fields the PATCH above writes, so the panel that explains a
// queue had no data to show.
func TestGetTriageStatsCarriesSourcePolicyAndPending(t *testing.T) {
	src := newPolicyTestSource(t)
	id := uuidToString(src.ID)
	patchTriageSource(t, id, map[string]any{
		"mode": "gate", "auto_accept": true, "cap_per_hour": 25, "expiry_days": 3,
	}).Want(http.StatusOK)

	// One due pending item and one parked by a snooze: only the due one is
	// waiting on a human.
	dbfx.Insert(t, "triage_item", testutil.Cols{
		"workspace_id": testWorkspaceID, "source_id": id, "origin_type": "channel",
		"title": "due delivery", "normalized_title": "due delivery " + id,
		"state": triage.StatePending, "shadow": false,
	})
	dbfx.Insert(t, "triage_item", testutil.Cols{
		"workspace_id": testWorkspaceID, "source_id": id, "origin_type": "channel",
		"title": "parked delivery", "normalized_title": "parked delivery " + id,
		"state": triage.StatePending, "shadow": false,
		"snoozed_until": time.Now().Add(48 * time.Hour).UTC(),
	})

	var out TriageStatsResponse
	testutil.Call(t, testHandler.GetTriageStats, newRequest(http.MethodGet, "/api/triage/stats", nil)).
		Want(http.StatusOK).JSON(&out)

	var stats *TriageSourceStats
	for i := range out.Sources {
		if out.Sources[i].ID == id {
			stats = &out.Sources[i]
		}
	}
	if stats == nil {
		t.Fatalf("source %s missing from stats: %+v", id, out.Sources)
	}
	if stats.Mode != "gate" || !stats.AutoAccept || stats.CapPerHour != 25 || stats.ExpiryDays != 3 {
		t.Fatalf("source policy in stats = %+v, want gate/auto/25/3", stats)
	}
	if stats.Pending != 1 {
		t.Fatalf("source pending = %d, want 1 — the snoozed item is parked, not waiting", stats.Pending)
	}
}
