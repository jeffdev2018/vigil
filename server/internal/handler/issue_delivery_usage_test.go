package handler

import (
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"math"
	"testing"
	"time"
)

func TestSnapshotDeliveryUsageCoverageAndExactAmounts(t *testing.T) {
	id := parseUUID("00000000-0000-4000-8000-000000000001")
	row := db.ListIssueDeliveryUsageRow{TaskID: id, UsageTaskID: id, Status: "completed", Provider: "fixture", Model: "unknown", CostUsdTicks: pgtype.Int8{Int64: math.MaxInt64, Valid: true}}
	result := snapshotDeliveryUsage([]db.ListIssueDeliveryUsageRow{row, row}, time.Now())
	if result.Status != "reported" || result.AvailableUSD == nil || *result.AvailableUSD != "1844674407.3709551614" || len(result.RunIDs) != 1 {
		t.Fatalf("exact ticks: %+v", result)
	}
	row.CostUsdTicks = pgtype.Int8{}
	row.Model = "gpt-5.4-mini"
	row.InputTokens = 1_000_000
	result = snapshotDeliveryUsage([]db.ListIssueDeliveryUsageRow{row}, time.Now())
	if result.Status != "estimated" || result.EstimatedUSD != "0.7500000000" || result.Slices[0].Price == nil {
		t.Fatalf("estimate: %+v", result)
	}
	missing := row
	missing.TaskID = parseUUID("00000000-0000-4000-8000-000000000002")
	missing.UsageTaskID = pgtype.UUID{}
	missing.Status = "running"
	unpriced := row
	unpriced.Model = "fixture-unpriced"
	result = snapshotDeliveryUsage([]db.ListIssueDeliveryUsageRow{row, missing, unpriced}, time.Now())
	if result.Status != "partial" || result.RunsWithoutUsage != 1 || result.NonterminalRuns != 1 || result.UnpricedSlices != 1 || len(result.RunIDs) != 2 {
		t.Fatalf("coverage: %+v", result)
	}
	for _, rows := range [][]db.ListIssueDeliveryUsageRow{nil, {missing}, {unpriced}} {
		result = snapshotDeliveryUsage(rows, time.Now())
		if result.Status != "unavailable" || result.AvailableUSD != nil {
			t.Fatalf("missing usage became free: %+v", result)
		}
	}
	row.CostUsdTicks = pgtype.Int8{Valid: true}
	row.InputTokens = 0
	result = snapshotDeliveryUsage([]db.ListIssueDeliveryUsageRow{row}, time.Now())
	if result.Status != "reported" || result.AvailableUSD == nil || *result.AvailableUSD != "0.0000000000" {
		t.Fatalf("reported zero lost: %+v", result)
	}
}
