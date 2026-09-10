package scheduler

import (
	"context"
	"fmt"
	"time"
)

const JobNameIssueRecurrenceTick = "issue_recurrence_tick"

// IssueRecurrenceTickJob spawns the next occurrence of recurring issues:
// schedule rules past their next run and on_close rules whose current
// occurrence is done or cancelled (OS plan, table stakes).
func IssueRecurrenceTickJob(tick func(ctx context.Context) int) JobSpec {
	return JobSpec{
		Name:              JobNameIssueRecurrenceTick,
		Cadence:           time.Minute,
		ScheduleDelay:     45 * time.Second,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        5 * time.Minute,
		StaleTimeout:      10 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       2,
		RetryBackoff:      []time.Duration{time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			if tick == nil {
				return HandlerResult{}, fmt.Errorf("issue_recurrence_tick: no handler")
			}
			n := tick(ctx)
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(n)}, nil
		},
	}
}
