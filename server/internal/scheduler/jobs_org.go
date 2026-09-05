package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameOrgTick = "org_tick"

// OrgTickJob (K75) sweeps the live org structures every five minutes:
// task forces end, breakers trip, restructurings are proposed. Plan time
// comes from the DB clock like every other job here.
func OrgTickJob(pool *pgxpool.Pool, tick func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameOrgTick,
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
			acted, err := tick(ctx, now)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("org_tick: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(acted)}, nil
		},
	}
}
