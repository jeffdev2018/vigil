package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameDocDriftCheck = "doc_drift_check"

// DocDriftCheckJob (K56) ticks every five minutes and starts one read-only
// drift check per indexed repository whose default branch moved since the last
// one. Unlike the code health sweep this job carries no clock: the trigger is
// the repo index's newest indexed commit differing from the commit that
// repository was last checked at, so the cadence only decides how soon after a
// merge the check happens, never whether it happens at all.
func DocDriftCheckJob(_ *pgxpool.Pool, check func(ctx context.Context) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameDocDriftCheck,
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
			started, err := check(ctx)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("doc_drift_check: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(started)}, nil
		},
	}
}
