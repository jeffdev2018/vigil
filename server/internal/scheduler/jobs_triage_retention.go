package scheduler

import (
	"context"
	"fmt"
	"time"
)

const JobNameTriageRetentionSweep = "triage_retention_sweep"

// TriageRetentionSweepJob drives the triage queue's periodic maintenance:
// pending items past their expires_at become `expired`, and snoozes whose
// time has come are cleared so the item is announced again. `sweep` is
// handler.SweepTriageQueue, which bounds each run to one batch — so the
// job runs hourly rather than daily and a large backlog drains over
// consecutive runs instead of in one long transaction.
func TriageRetentionSweepJob(sweep func(ctx context.Context) (int64, error)) JobSpec {
	return JobSpec{
		Name:              JobNameTriageRetentionSweep,
		Cadence:           time.Hour,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     6 * time.Hour,
		RunTimeout:        5 * time.Minute,
		StaleTimeout:      10 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			n, err := sweep(ctx)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("triage_retention_sweep: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: n}, nil
		},
	}
}
