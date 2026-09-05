package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameSkillMiner = "skill_miner"

// SkillMinerJob (K58) clusters recent correction signals every 30 minutes and
// drafts a skill for every recurring pattern.
func SkillMinerJob(pool *pgxpool.Pool, mine func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameSkillMiner,
		Cadence:           30 * time.Minute,
		ScheduleDelay:     2 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     2 * time.Hour,
		RunTimeout:        10 * time.Minute,
		StaleTimeout:      20 * time.Minute,
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
			drafted, err := mine(ctx, now)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("skill_miner: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(drafted)}, nil
		},
	}
}
