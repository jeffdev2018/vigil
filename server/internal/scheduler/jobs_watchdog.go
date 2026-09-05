package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameWatchdogScan = "watchdog_scan"

// WatchdogScanJob (K73) sweeps the enabled watchdogs every two minutes and
// starts one scan per tree at rest. Plan time comes from the DB clock like
// every other job here.
func WatchdogScanJob(pool *pgxpool.Pool, scan func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameWatchdogScan,
		Cadence:           2 * time.Minute,
		ScheduleDelay:     45 * time.Second,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        5 * time.Minute,
		StaleTimeout:      10 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			now, err := dbNow(ctx, pool)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("read db clock: %w", err)
			}
			started, err := scan(ctx, now)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("watchdog_scan: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(started)}, nil
		},
	}
}
