package handler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// The digest reaches the person who STOPPED opening the queue, so what it
// counts, who it reaches, and how often, are the whole feature.
func TestTriageStaleDigestNotifiesManagersOncePerDay(t *testing.T) {
	ws := dbfx.Workspace(t, "Triage digest", "triage-digest-"+uuid.NewString()[:8])
	owner := dbfx.User(t, "Digest Owner", "digest-owner-"+uuid.NewString()[:8]+"@example.com")
	dbfx.Member(t, ws, owner, "owner")
	plain := dbfx.User(t, "Digest Member", "digest-member-"+uuid.NewString()[:8]+"@example.com")
	dbfx.Member(t, ws, plain, "member")
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, ws)

	plant := func(title, state string, age time.Duration, shadow bool) {
		t.Helper()
		cols := testutil.Cols{
			"workspace_id":     ws,
			"source_id":        uuid.NewString(),
			"origin_type":      "autopilot",
			"title":            title,
			"normalized_title": title,
			"state":            state,
			"shadow":           shadow,
			"first_seen_at":    time.Now().Add(-age).UTC(),
		}
		// A resolved item carries the timestamp its CHECK constraint requires.
		if state != "pending" {
			cols["resolved_at"] = time.Now().UTC()
		}
		dbfx.Insert(t, "triage_item", cols)
	}
	plant("waiting since tuesday", "pending", 72*time.Hour, false)
	plant("waiting since wednesday", "pending", 60*time.Hour, false)
	// None of these count: too recent, already decided, or shadow (nobody is
	// waiting on a shadow item).
	plant("arrived this morning", "pending", time.Hour, false)
	plant("already dismissed", "dismissed", 90*time.Hour, false)
	plant("shadow item", "pending", 90*time.Hour, true)

	now := time.Now()
	if _, err := testHandler.RunTriageStaleDigest(context.Background(), now); err != nil {
		t.Fatalf("digest: %v", err)
	}

	var title, day string
	var count int
	dbfx.QueryRow(t, `SELECT title, details->>'day', (details->>'count')::int FROM inbox_item
	                  WHERE workspace_id = $1 AND recipient_id = $2 AND type = 'triage_stale'`, ws, owner).
		Scan(&title, &day, &count)
	if count != 2 {
		t.Fatalf("count = %d, want the 2 real pending items older than 48h (title %q)", count, title)
	}
	if day != now.UTC().Format("2006-01-02") {
		t.Fatalf("day = %q, want the workspace's calendar day", day)
	}
	// A plain member cannot act on the queue's sources, so the digest is not
	// their notification.
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE recipient_id = $1 AND type = 'triage_stale'`, plain); n != 0 {
		t.Fatalf("plain member got %d digests, want 0", n)
	}

	// An ignored queue is ignored every day; one reminder a day is a nudge,
	// one an hour is a reason to mute the inbox.
	if _, err := testHandler.RunTriageStaleDigest(context.Background(), now.Add(2*time.Hour)); err != nil {
		t.Fatalf("second digest: %v", err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'triage_stale'`, ws); n != 1 {
		t.Fatalf("digests filed = %d, want exactly 1 per day", n)
	}
}

func TestTriageStaleDigestSaysNothingAboutAFreshQueue(t *testing.T) {
	ws := dbfx.Workspace(t, "Fresh queue", "fresh-queue-"+uuid.NewString()[:8])
	owner := dbfx.User(t, "Fresh Owner", "fresh-owner-"+uuid.NewString()[:8]+"@example.com")
	dbfx.Member(t, ws, owner, "owner")
	dbfx.Cleanup(t, `DELETE FROM inbox_item WHERE workspace_id = $1`, ws)
	dbfx.Insert(t, "triage_item", testutil.Cols{
		"workspace_id": ws, "source_id": uuid.NewString(), "origin_type": "autopilot",
		"title": "just arrived", "normalized_title": "just arrived", "state": "pending",
		"shadow": false, "first_seen_at": time.Now().Add(-2 * time.Hour).UTC(),
	})
	if _, err := testHandler.RunTriageStaleDigest(context.Background(), time.Now()); err != nil {
		t.Fatalf("digest: %v", err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1`, ws); n != 0 {
		t.Fatalf("inbox items = %d, want none for a queue nobody is late on", n)
	}
}
