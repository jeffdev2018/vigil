package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const JobNameDecisionSLAEscalation = "decision_sla_escalation"

// DecisionSLAEscalationJob (K35) steps overdue Decision Cards every few
// minutes: substitute first, workspace leads next. escalate is
// handler.EscalateOverdueDecisions; the plan time comes from the DB clock
// like every other job here, so a skewed server does not escalate early.
func DecisionSLAEscalationJob(pool *pgxpool.Pool, escalate func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return JobSpec{
		Name:              JobNameDecisionSLAEscalation,
		Cadence:           2 * time.Minute,
		ScheduleDelay:     30 * time.Second,
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
			now, err := dbNow(ctx, pool)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("read db clock: %w", err)
			}
			moved, err := escalate(ctx, now)
			if err != nil {
				return HandlerResult{}, fmt.Errorf("decision_sla_escalation: %w", err)
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(moved)}, nil
		},
	}
}
