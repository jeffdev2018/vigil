package scheduler

import (
	"context"
	"fmt"
	"time"
)

const JobNameRunPreviewStaleSweep = "run_preview_stale_sweep"

// RunPreviewStaleSweepJob (F12) moves previews whose daemon stopped
// heartbeating to `stale`.
//
// Nothing else can report it: the daemon that owned the dev server is the only
// party that knows it died, and a daemon that died is by definition not
// reporting. Without this pass a preview claims `ready` forever after a laptop
// closes, and the reviewer's link 502s with no explanation.
//
// The cadence only decides how soon after the reconnect grace the UI catches
// up; the staleness decision itself is the runtime heartbeat window the run
// sweeper already uses, so a preview and its run agree on when the machine left.
func RunPreviewStaleSweepJob(sweep func(ctx context.Context) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameRunPreviewStaleSweep,
		Cadence:           time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     10 * time.Minute,
		RunTimeout:        30 * time.Second,
		StaleTimeout:      2 * time.Minute,
		HeartbeatInterval: 15 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       2,
		RetryBackoff:      []time.Duration{30 * time.Second},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			n, err := sweep(ctx)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("%s: %w", JobNameRunPreviewStaleSweep, err)
			}
			return HandlerResult{RowsAffected: int64(n)}, nil
		},
	}
}
