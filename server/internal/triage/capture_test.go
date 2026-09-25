package triage

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	"github.com/multica-ai/multica/server/pkg/dbid"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func newCaptureTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://multica:multica@localhost:5432/multica?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("database unavailable: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// Regression test for the dedupe_key / normalized_title collision: two
// deliveries that share a dedupe_key (the transport-level identity, e.g. a
// webhook delivery id) but land on different normalized_title must fold
// into ONE pending item, not raise an unhandled unique_violation on
// uq_triage_item_dedupe and drop the second delivery.
func TestCaptureFoldsOnDedupeKeyAcrossDifferentTitles(t *testing.T) {
	pool := newCaptureTestPool(t)
	bootstrap := testutil.New(pool, "", "")
	suffix := time.Now().UnixNano()
	userID := bootstrap.User(t, fmt.Sprintf("capture-owner-%d", suffix), fmt.Sprintf("capture-owner-%d@example.com", suffix))
	workspaceID := bootstrap.Workspace(t, fmt.Sprintf("capture-%d", suffix), fmt.Sprintf("capture-%d", suffix))

	q := db.New(pool)
	ctx := context.Background()
	refID := dbid.NewV7()
	dedupeKey := fmt.Sprintf("delivery-%d", suffix)

	base := CaptureParams{
		WorkspaceID:     util.MustParseUUID(workspaceID),
		SourceKind:      "autopilot_webhook",
		SourceRefID:     refID,
		SourceName:      "test webhook",
		SourceCreatedBy: util.MustParseUUID(userID),
		OriginType:      "webhook",
		State:           StatePending,
		DedupeKey:       dedupeKey,
	}

	first := base
	first.Title = "Order #1 shipped"
	first.BodyMarkdown = "first delivery"
	item1, source1, err := Capture(ctx, q, first)
	if err != nil {
		t.Fatalf("first Capture: %v", err)
	}
	t.Cleanup(func() {
		pool.Exec(context.Background(), `DELETE FROM triage_item WHERE workspace_id = $1`, workspaceID)
		pool.Exec(context.Background(), `DELETE FROM triage_source WHERE workspace_id = $1`, workspaceID)
	})

	second := base
	second.Title = "Order #1 delayed" // different normalized_title, same dedupe_key
	second.BodyMarkdown = "second delivery, updated status"
	item2, source2, err := Capture(ctx, q, second)
	if err != nil {
		t.Fatalf("second Capture with the same dedupe_key but a different title must fold, not error: %v", err)
	}
	if uuidToStringT(source2.ID) != uuidToStringT(source1.ID) {
		t.Fatalf("source changed between captures: %s vs %s", uuidToStringT(source1.ID), uuidToStringT(source2.ID))
	}
	if uuidToStringT(item2.ID) != uuidToStringT(item1.ID) {
		t.Fatalf("second delivery created a new item (%s) instead of folding into the first (%s)", uuidToStringT(item2.ID), uuidToStringT(item1.ID))
	}
	if item2.CollapseCount != item1.CollapseCount+1 {
		t.Fatalf("collapse_count = %d, want %d (folded once)", item2.CollapseCount, item1.CollapseCount+1)
	}

	n := 0
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM triage_item WHERE workspace_id = $1`, workspaceID).Scan(&n); err != nil {
		t.Fatalf("count triage_item: %v", err)
	}
	if n != 1 {
		t.Fatalf("triage_item rows = %d, want exactly 1 (the second delivery must not create a stray row)", n)
	}
}

func uuidToStringT(id pgtype.UUID) string {
	return util.UUIDToString(id)
}
