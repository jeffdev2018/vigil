package scheduler

import (
	"context"
	"fmt"
	"time"
)

const JobNameNativeAgentTick = "native_agent_tick"

// NativeAgentTickJob drives the in-server agent runtime: seed + heartbeat the
// native agent_runtime rows, then claim and dispatch queued tasks to the
// tool-calling loop. The tick itself only dispatches — the runs execute in
// their own goroutines with their own timeout, so a slow model call never
// holds the job lease.
func NativeAgentTickJob(tick func(ctx context.Context) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameNativeAgentTick,
		Cadence:           10 * time.Second,
		ScheduleDelay:     10 * time.Second,
		CatchUpMode:       CatchUpLatestOnly,
		RunTimeout:        30 * time.Second,
		StaleTimeout:      2 * time.Minute,
		HeartbeatInterval: 10 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       3,
		RetryBackoff:      []time.Duration{10 * time.Second, 30 * time.Second, time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			dispatched, err := tick(ctx)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("native_agent_tick: %w", err)
			}
			return HandlerResult{RowsAffected: int64(dispatched)}, nil
		},
	}
}
