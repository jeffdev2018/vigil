package service

import (
	"testing"
	"time"
)

func TestBudgetPeriodBoundsUTC(t *testing.T) {
	now := time.Date(2026, time.March, 18, 15, 4, 5, 0, time.FixedZone("offset", 2*60*60))
	tests := []struct {
		period string
		start  string
		end    string
	}{
		{"daily", "2026-03-18T00:00:00Z", "2026-03-19T00:00:00Z"},
		{"weekly", "2026-03-16T00:00:00Z", "2026-03-23T00:00:00Z"},
		{"monthly", "2026-03-01T00:00:00Z", "2026-04-01T00:00:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			start, end, err := budgetPeriodBounds(tt.period, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := start.Format(time.RFC3339); got != tt.start {
				t.Fatalf("start = %s, want %s", got, tt.start)
			}
			if got := end.Format(time.RFC3339); got != tt.end {
				t.Fatalf("end = %s, want %s", got, tt.end)
			}
		})
	}
	if _, _, err := budgetPeriodBounds("yearly", now); err == nil {
		t.Fatal("unsupported period should fail")
	}
}

func TestThresholdTicksRoundsUp(t *testing.T) {
	tests := []struct {
		limit int64
		bps   int32
		want  int64
	}{
		{100, 8000, 80},
		{101, 8000, 81},
		{1, 1, 1},
		{9_000_000_000_000_000, 10_000, 9_000_000_000_000_000},
	}
	for _, tt := range tests {
		if got := thresholdTicks(tt.limit, tt.bps); got != tt.want {
			t.Fatalf("thresholdTicks(%d, %d) = %d, want %d", tt.limit, tt.bps, got, tt.want)
		}
	}
}
