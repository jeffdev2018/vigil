package scheduler

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameMcpBindingReview = "mcp_binding_review"

// McpBindingReviewJob (K77) proposes, once a month, the removal of the MCP
// tools no agent used for thirty days. The handler files inbox proposals and
// changes nothing itself.
func McpBindingReviewJob(pool *pgxpool.Pool, review func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameMcpBindingReview,
		Cadence:           30 * 24 * time.Hour,
		ScheduleDelay:     10 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     7 * 24 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: time.Minute,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			now, err := dbNow(ctx, pool)
			if err != nil {
				return HandlerResult{}, err
			}
			acted, err := review(ctx, now)
			if err != nil {
				return HandlerResult{}, err
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(acted)}, nil
		},
	}
}
