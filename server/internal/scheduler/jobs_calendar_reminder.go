package scheduler

import (
	"context"
	"fmt"
	"time"
)

const JobNameCalendarReminder = "calendar_reminder"

// CalendarReminderJob files inbox reminders for events starting within the
// lead time (native calendar). Once a minute; a missed tick catches up on
// the next, because the query keys on reminded_at, not on the tick.
func CalendarReminderJob(remind func(ctx context.Context) int) JobSpec {
	return JobSpec{
		Name:              JobNameCalendarReminder,
		Cadence:           time.Minute,
		ScheduleDelay:     30 * time.Second,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        2 * time.Minute,
		StaleTimeout:      5 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       2,
		RetryBackoff:      []time.Duration{time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			if remind == nil {
				return HandlerResult{}, fmt.Errorf("calendar_reminder: no handler")
			}
			n := remind(ctx)
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(n)}, nil
		},
	}
}
