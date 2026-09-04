package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/pricing"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	defaultBudgetEstimateFloorTicks int64 = 1_000_000_000 // $0.10
	defaultBudgetEstimateRunCount   int32 = 10
)

// BudgetScope identifies every policy that can govern one run.
type BudgetScope struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	AgentID     pgtype.UUID
}

// BudgetExceededError is the stable service-level refusal used by every admission path.
type BudgetExceededError struct {
	PolicyID      pgtype.UUID
	LimitTicks    int64
	SpentTicks    int64
	ReservedTicks int64
	PeriodEnd     time.Time
}

func (e *BudgetExceededError) Error() string { return "workspace budget exceeded" }

// BudgetStatus is the policy-neutral response model returned by the API.
type BudgetStatus struct {
	Policy            db.BudgetPolicy
	SpentTicks        int64
	ReservedTicks     int64
	PeriodStart       time.Time
	PeriodEnd         time.Time
	Reached           bool
	OverrideExpiresAt *time.Time
}

// BudgetService owns transactional reservation and settlement.
type BudgetService struct {
	Queries            *db.Queries
	TxStarter          TxStarter
	Bus                *events.Bus
	Now                func() time.Time
	EstimateFloorTicks int64
	EstimateRunCount   int32
}

func NewBudgetService(q *db.Queries, tx TxStarter, bus *events.Bus) *BudgetService {
	floor := defaultBudgetEstimateFloorTicks
	if raw := os.Getenv("MULTICA_BUDGET_MIN_ESTIMATE_USD_TICKS"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed >= 0 {
			floor = parsed
		}
	}
	return &BudgetService{
		Queries: q, TxStarter: tx, Bus: bus, Now: time.Now,
		EstimateFloorTicks: floor, EstimateRunCount: defaultBudgetEstimateRunCount,
	}
}

func (s *BudgetService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func budgetPeriodBounds(period string, now time.Time) (time.Time, time.Time, error) {
	now = now.UTC()
	switch period {
	case "daily":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 1), nil
	case "weekly":
		day := int(now.Weekday())
		if day == 0 {
			day = 7
		}
		start := time.Date(now.Year(), now.Month(), now.Day()-day+1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 0, 7), nil
	case "monthly":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start, start.AddDate(0, 1, 0), nil
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("unsupported budget period %q", period)
	}
}

// ReserveTaskInTx evaluates workspace, project, and agent policies under their
// period-row locks. The caller must create the task and commit the same transaction.
func (s *BudgetService) ReserveTaskInTx(ctx context.Context, q *db.Queries, scope BudgetScope, taskID pgtype.UUID) (bool, error) {
	policies, err := q.ListApplicableBudgetPolicies(ctx, db.ListApplicableBudgetPoliciesParams{
		WorkspaceID: scope.WorkspaceID, ProjectID: scope.ProjectID, AgentID: scope.AgentID,
	})
	if err != nil {
		return false, fmt.Errorf("list applicable budget policies: %w", err)
	}
	if len(policies) == 0 {
		return false, nil
	}
	estimate, err := s.estimateTaskTicks(ctx, q, scope.AgentID)
	if err != nil {
		return false, err
	}
	now := s.now()
	for _, policy := range policies {
		start, end, err := budgetPeriodBounds(policy.Period, now)
		if err != nil {
			return false, err
		}
		periodArgs := db.EnsureBudgetPeriodParams{
			PolicyID:    policy.ID,
			PeriodStart: pgtype.Timestamptz{Time: start, Valid: true},
			PeriodEnd:   pgtype.Timestamptz{Time: end, Valid: true},
		}
		period, err := q.EnsureBudgetPeriod(ctx, periodArgs)
		if err != nil {
			return false, fmt.Errorf("lock budget period: %w", err)
		}
		key := taskID.String()
		if _, err := q.GetBudgetReservationByKey(ctx, db.GetBudgetReservationByKeyParams{
			PolicyID: policy.ID, PeriodStart: periodArgs.PeriodStart,
			PeriodEnd: periodArgs.PeriodEnd, IdempotencyKey: key,
		}); err == nil {
			continue
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("load budget reservation: %w", err)
		}
		overridden := false
		if _, err := q.GetActiveBudgetOverride(ctx, db.GetActiveBudgetOverrideParams{
			WorkspaceID: scope.WorkspaceID, PolicyID: policy.ID,
			Now: pgtype.Timestamptz{Time: now, Valid: true},
		}); err == nil {
			overridden = true
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return false, fmt.Errorf("load budget override: %w", err)
		}
		current := period.SpentUsdTicks + period.ReservedUsdTicks
		reached := current >= policy.LimitUsdTicks || current+estimate > policy.LimitUsdTicks
		if reached && policy.Action == "enforce" && !overridden {
			return false, &BudgetExceededError{
				PolicyID: policy.ID, LimitTicks: policy.LimitUsdTicks,
				SpentTicks: period.SpentUsdTicks, ReservedTicks: period.ReservedUsdTicks,
				PeriodEnd: end,
			}
		}
		if _, err := q.CreateBudgetReservation(ctx, db.CreateBudgetReservationParams{
			ID: dbid.NewV7(), PolicyID: policy.ID,
			PeriodStart: periodArgs.PeriodStart, PeriodEnd: periodArgs.PeriodEnd,
			TaskID: taskID, EstimateUsdTicks: estimate, IdempotencyKey: key,
		}); err != nil {
			return false, fmt.Errorf("create budget reservation: %w", err)
		}
		if _, err := q.IncrementBudgetReserved(ctx, db.IncrementBudgetReservedParams{
			EstimateUsdTicks: estimate, PolicyID: policy.ID,
			PeriodStart: periodArgs.PeriodStart, PeriodEnd: periodArgs.PeriodEnd,
		}); err != nil {
			return false, fmt.Errorf("increment reserved budget: %w", err)
		}
	}
	return true, nil
}

func (s *BudgetService) estimateTaskTicks(ctx context.Context, q *db.Queries, agentID pgtype.UUID) (int64, error) {
	runCount := s.EstimateRunCount
	if runCount <= 0 {
		runCount = defaultBudgetEstimateRunCount
	}
	rows, err := q.ListRecentAgentTaskUsageForBudget(ctx, db.ListRecentAgentTaskUsageForBudgetParams{
		AgentID: agentID, RunLimit: runCount,
	})
	if err != nil {
		return 0, fmt.Errorf("load recent usage for budget estimate: %w", err)
	}
	perTask := make(map[pgtype.UUID]int64)
	for _, row := range rows {
		var authoritative *int64
		if row.CostUsdTicks.Valid {
			value := row.CostUsdTicks.Int64
			authoritative = &value
		}
		perTask[row.TaskID] += pricing.EstimateTicks(pricing.Usage{
			Provider: row.Provider, Model: row.Model,
			InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens,
			CostUSDTicks: authoritative,
		})
	}
	var total int64
	for _, cost := range perTask {
		total += cost
	}
	estimate := int64(0)
	if len(perTask) > 0 {
		estimate = total / int64(len(perTask))
	}
	floor := s.EstimateFloorTicks
	if floor < 0 {
		floor = 0
	}
	if estimate < floor {
		estimate = floor
	}
	return estimate, nil
}

// SettleTaskInTx consumes a completed run at actual cost or releases it.
func (s *BudgetService) SettleTaskInTx(ctx context.Context, q *db.Queries, taskID pgtype.UUID, consume bool) (bool, error) {
	reservations, err := q.ListReservedBudgetReservationsByTask(ctx, taskID)
	if err != nil {
		return false, fmt.Errorf("list task budget reservations: %w", err)
	}
	if len(reservations) == 0 {
		return false, nil
	}
	actual := int64(0)
	if consume {
		rows, err := q.ListTaskUsageForBudget(ctx, taskID)
		if err != nil {
			return false, fmt.Errorf("load task usage for budget settlement: %w", err)
		}
		for _, row := range rows {
			var authoritative *int64
			if row.CostUsdTicks.Valid {
				value := row.CostUsdTicks.Int64
				authoritative = &value
			}
			actual += pricing.EstimateTicks(pricing.Usage{
				Provider: row.Provider, Model: row.Model,
				InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
				CacheReadTokens: row.CacheReadTokens, CacheWriteTokens: row.CacheWriteTokens,
				CostUSDTicks: authoritative,
			})
		}
	}
	changed := false
	for _, reservation := range reservations {
		if consume {
			_, err = q.ConsumeBudgetReservation(ctx, db.ConsumeBudgetReservationParams{
				ActualUsdTicks: actual, ReservationID: reservation.ID,
			})
		} else {
			_, err = q.ReleaseBudgetReservation(ctx, reservation.ID)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return changed, fmt.Errorf("settle budget reservation: %w", err)
		}
		changed = true
	}
	return changed, nil
}

// Status returns every policy applicable to a scope without creating periods.
func (s *BudgetService) Status(ctx context.Context, scope BudgetScope) ([]BudgetStatus, error) {
	var policies []db.BudgetPolicy
	var err error
	if scope.ProjectID.Valid || scope.AgentID.Valid {
		policies, err = s.Queries.ListApplicableBudgetPolicies(ctx, db.ListApplicableBudgetPoliciesParams{
			WorkspaceID: scope.WorkspaceID, ProjectID: scope.ProjectID, AgentID: scope.AgentID,
		})
	} else {
		policies, err = s.Queries.ListBudgetPolicies(ctx, scope.WorkspaceID)
	}
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make([]BudgetStatus, 0, len(policies))
	for _, policy := range policies {
		start, end, err := budgetPeriodBounds(policy.Period, now)
		if err != nil {
			return nil, err
		}
		status := BudgetStatus{Policy: policy, PeriodStart: start, PeriodEnd: end}
		period, err := s.Queries.GetBudgetPeriod(ctx, db.GetBudgetPeriodParams{
			PolicyID:    policy.ID,
			PeriodStart: pgtype.Timestamptz{Time: start, Valid: true},
			PeriodEnd:   pgtype.Timestamptz{Time: end, Valid: true},
		})
		if err == nil {
			status.SpentTicks = period.SpentUsdTicks
			status.ReservedTicks = period.ReservedUsdTicks
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		status.Reached = status.SpentTicks+status.ReservedTicks >= policy.LimitUsdTicks
		override, err := s.Queries.GetActiveBudgetOverride(ctx, db.GetActiveBudgetOverrideParams{
			WorkspaceID: scope.WorkspaceID, PolicyID: policy.ID,
			Now: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err == nil {
			expires := override.ExpiresAt.Time.UTC()
			status.OverrideExpiresAt = &expires
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		out = append(out, status)
	}
	return out, nil
}

func (s *BudgetService) publishUpdated(workspaceID pgtype.UUID) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type: protocol.EventBudgetUpdated, WorkspaceID: workspaceID.String(),
		ActorType: "system", Payload: map[string]any{"workspace_id": workspaceID.String()},
	})
}

// ReconcileReservations releases orphan/failed work and consumes completed work.
func (s *BudgetService) ReconcileReservations(ctx context.Context, createdBefore time.Time, limit int32) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	reservations, err := s.Queries.ListRecoverableBudgetReservations(ctx, db.ListRecoverableBudgetReservationsParams{
		CreatedBefore: pgtype.Timestamptz{Time: createdBefore.UTC(), Valid: true}, RowLimit: limit,
	})
	if err != nil {
		return 0, err
	}
	settled := 0
	for _, reservation := range reservations {
		consume := false
		if task, err := s.Queries.GetAgentTask(ctx, reservation.TaskID); err == nil {
			consume = task.Status == "completed"
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return settled, err
		}
		changed, err := s.settleOne(ctx, reservation.TaskID, consume)
		if err != nil {
			return settled, err
		}
		if changed {
			settled++
		}
	}
	return settled, nil
}

func (s *BudgetService) settleOne(ctx context.Context, taskID pgtype.UUID, consume bool) (bool, error) {
	tx, err := s.TxStarter.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	changed, err := s.SettleTaskInTx(ctx, s.Queries.WithTx(tx), taskID, consume)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return changed, nil
}
