package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	JobNameStandup     = "standup"
	JobNameWeeklyRetro = "weekly_retro"
)

// StandupJob (K34) files the standup questions for issues stale beyond each
// workspace's threshold. ask is handler.RunStandup; it deduplicates per day
// itself, so a frequent cadence is harmless.
func StandupJob(pool *pgxpool.Pool, ask func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return digestJob(pool, JobNameStandup, 30*time.Minute, ask)
}

// WeeklyRetroJob (K34) generates the retro of the last completed week once
// per workspace. generate is handler.GenerateDueWeeklyRetros.
func WeeklyRetroJob(pool *pgxpool.Pool, generate func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return digestJob(pool, JobNameWeeklyRetro, time.Hour, generate)
}

func digestJob(pool *pgxpool.Pool, name string, cadence time.Duration, run func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              name,
		Cadence:           cadence,
		ScheduleDelay:     time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     2 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
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
			n, err := run(ctx, now)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("%s: %w", name, err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(n)}, nil
		},
	}
}
