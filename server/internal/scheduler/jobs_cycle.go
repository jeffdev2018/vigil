package scheduler

import (
	"context"
	"fmt"
	"time"
)

const (
	JobNameCycleSnapshot = "cycle_snapshot"
	JobNameCycleRollover = "cycle_rollover"
)

// CycleSnapshotJob (F29) materializes one burndown point per open cycle per
// day.
//
// It has to be a job rather than a query because this schema keeps no status
// history: "how many issues were still open last Tuesday" is not answerable
// after the fact, so the only way to have a burndown is to write the day down
// while it is still today. The upsert is keyed on (cycle_id, snapshot_date),
// so an extra run is a no-op and a missed run leaves one flat day rather than
// a hole — the burndown endpoint carries the last known value forward.
//
// Hourly rather than daily: a daily cadence means one missed tick loses a day
// permanently, and the write is a single upsert per open cycle.
func CycleSnapshotJob(snapshot func(ctx context.Context) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameCycleSnapshot,
		Cadence:           time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     6 * time.Hour,
		RunTimeout:        5 * time.Minute,
		StaleTimeout:      10 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       2,
		RetryBackoff:      []time.Duration{time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			n, err := snapshot(ctx)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("%s: %w", JobNameCycleSnapshot, err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(n)}, nil
		},
	}
}

// CycleRolloverJob (F29) closes cycles whose end date has passed and moves
// their unfinished work into the next cycle of the same project.
//
// Nobody else can do it: a cycle ends by the calendar turning over, which is
// not a write anyone makes. Without this pass an ended cycle keeps collecting
// a burndown it can no longer influence, and its unfinished work stays
// attached to a plan that is over.
//
// Idempotent by construction: closing keeps the original closed_at and the
// sweep only picks up cycles that are still open, so a second run after a
// crash mid-batch resumes rather than moving work twice.
func CycleRolloverJob(rollover func(ctx context.Context) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameCycleRollover,
		Cadence:           time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     6 * time.Hour,
		RunTimeout:        5 * time.Minute,
		StaleTimeout:      10 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       2,
		RetryBackoff:      []time.Duration{time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			n, err := rollover(ctx)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("%s: %w", JobNameCycleRollover, err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(n)}, nil
		},
	}
}
