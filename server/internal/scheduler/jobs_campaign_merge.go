package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameCampaignMergeQueue = "campaign_merge_queue"

// CampaignMergeQueueJob (K42) re-evaluates every active refactoring
// campaign's merge queue: a shard whose checks went green since the last
// run event enters the queue without anyone reading the board.
func CampaignMergeQueueJob(pool *pgxpool.Pool, advance func(ctx context.Context) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameCampaignMergeQueue,
		Cadence:           2 * time.Minute,
		ScheduleDelay:     time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     30 * time.Minute,
		RunTimeout:        5 * time.Minute,
		StaleTimeout:      10 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			n, err := advance(ctx)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("campaign_merge_queue: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(n)}, nil
		},
	}
}
