package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameBranchGCTick = "branch_gc_tick"

// BranchGCTickJob (JEF-388) runs the dead run-branch sweep hourly: each
// workspace that opted in via settings.branch_gc gets a discard enqueued for
// every terminal run whose branch was neither promoted nor discarded within
// its TTL. The enqueues ride the JEF-255 branch-action channel, so a tick
// while every daemon is offline simply finds nothing actionable; catch-up is
// latest-only because the candidate set is re-derived from the task table on
// every run — a missed hour changes nothing but the age of the branches.
func BranchGCTickJob(pool *pgxpool.Pool, tick func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameBranchGCTick,
		Cadence:           time.Hour,
		ScheduleDelay:     2 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     6 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      20 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       2,
		RetryBackoff:      []time.Duration{5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			if tick == nil {
				return HandlerResult{}, fmt.Errorf("branch_gc_tick: no handler")
			}
			now, err := dbNow(ctx, pool)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("read db clock: %w", err)
			}
			enqueued, err := tick(ctx, now)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("branch_gc_tick: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(enqueued)}, nil
		},
	}
}
