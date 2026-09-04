package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameMorningBriefing = "morning_briefing"

// MorningBriefingJob (K30) sends each enabled workspace its daily briefing
// once its local clock passes the configured hour. send is
// handler.SendDueMorningBriefings; the DB clock is the reference.
func MorningBriefingJob(pool *pgxpool.Pool, send func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameMorningBriefing,
		Cadence:           5 * time.Minute,
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
			sent, err := send(ctx, now)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("morning_briefing: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(sent)}, nil
		},
	}
}
