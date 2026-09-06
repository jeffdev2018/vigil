package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameCodeHealthScan = "code_health_scan"

// CodeHealthScanJob (K22) ticks every five minutes and starts one scan per
// workspace whose autopilot is enabled and whose cron came due since its last
// scan. The cadence is the resolution of the schedule, not its frequency: a
// weekly cron fires once a week, this job just checks often enough that the
// occurrence is not missed by hours. Plan time comes from the DB clock like
// every other job here.
func CodeHealthScanJob(pool *pgxpool.Pool, scan func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameCodeHealthScan,
		Cadence:           5 * time.Minute,
		ScheduleDelay:     90 * time.Second,
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
				return HandlerResult{}, fmt.Errorf("code_health_scan: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(started)}, nil
		},
	}
}
