package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameAgentScorecardRollup = "agent_scorecard_rollup"

// AgentScorecardRollupJob (K25) recomputes the last days of scorecards
// every quarter hour; rollup is handler.RollupAgentScorecards.
func AgentScorecardRollupJob(pool *pgxpool.Pool, rollup func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameAgentScorecardRollup,
		Cadence:           15 * time.Minute,
		ScheduleDelay:     2 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     6 * time.Hour,
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
			n, err := rollup(ctx, now)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("agent_scorecard_rollup: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(n)}, nil
		},
	}
}
