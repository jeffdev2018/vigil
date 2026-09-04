package scheduler

import (
	"context"
	"time"

	"github.com/multica-ai/multica/server/internal/service"
)

const JobNameReconcileBudgetReservations = "reconcile_budget_reservations"

func BudgetReservationReconciliationJob(budgets *service.BudgetService) JobSpec {
	return JobSpec{
		Name: JobNameReconcileBudgetReservations, Cadence: 10 * time.Minute,
		CatchUpMode: CatchUpLatestOnly, CatchUpWindow: time.Hour,
		RunTimeout: 2 * time.Minute, StaleTimeout: 3 * time.Minute,
		HeartbeatInterval: 30 * time.Second, MaxAttempts: 3,
		RetryBackoff: []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		Scopes:       StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			count, err := budgets.ReconcileReservations(ctx, time.Now().UTC().Add(-15*time.Minute), 500)
			if err != nil {
				return HandlerResult{}, err
			}
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(count)}, nil
		},
	}
}
