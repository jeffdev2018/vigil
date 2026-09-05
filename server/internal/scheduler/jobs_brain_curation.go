package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameWorkspaceBrainCuration = "workspace_brain_curation"

// WorkspaceBrainCurationJob runs the daily Brain curation pass. curate is
// service.TaskService.CurateWorkspaceBrains, which walks every workspace whose
// notes changed since the previous pass and applies the LLM's merge / retitle
// / tag / archive plan. Without a configured LLM the pass is a logged no-op,
// so the job is safe to register unconditionally.
//
// CatchUpLatestOnly: the pass is idempotent in effect (a tidy Brain yields an
// empty plan) and its own 26-hour change window recovers a missed day, so
// replaying every skipped plan_time would only spend tokens.
func WorkspaceBrainCurationJob(pool *pgxpool.Pool, curate func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameWorkspaceBrainCuration,
		Cadence:           24 * time.Hour,
		ScheduleDelay:     time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     48 * time.Hour,
		RunTimeout:        30 * time.Minute,
		StaleTimeout:      45 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       2,
		RetryBackoff:      []time.Duration{5 * time.Minute, 30 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			now, err := dbNow(ctx, pool)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("read db clock: %w", err)
			}
			applied, err := curate(ctx, now)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("workspace_brain_curation: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(applied)}, nil
		},
	}
}
