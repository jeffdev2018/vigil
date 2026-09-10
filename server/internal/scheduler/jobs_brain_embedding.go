package scheduler

import (
	"context"
	"fmt"
	"time"
)

const JobNameBrainEmbeddingBackfill = "brain_embedding_backfill"

// BrainEmbeddingBackfillJob embeds the notes whose vector is missing, stale
// or from another model (OS plan, vague B). The write paths embed inline;
// this job is what makes the ranked search whole after an outage, a model
// change, or a write path that has no embedder (curation, agent effects).
func BrainEmbeddingBackfillJob(backfill func(ctx context.Context) int) JobSpec {
	return JobSpec{
		Name:              JobNameBrainEmbeddingBackfill,
		Cadence:           10 * time.Minute,
		ScheduleDelay:     2 * time.Minute,
		CatchUpMode:       CatchUpLatestOnly,
		CatchUpWindow:     time.Hour,
		RunTimeout:        8 * time.Minute,
		StaleTimeout:      15 * time.Minute,
		HeartbeatInterval: 30 * time.Second,
		AllowStaleReentry: true,
		MaxAttempts:       2,
		RetryBackoff:      []time.Duration{time.Minute},
		Scopes:            StaticScopes(ScopeGlobal),
		Handler: func(ctx context.Context, in HandlerInput) (HandlerResult, error) {
			if backfill == nil {
				return HandlerResult{}, fmt.Errorf("brain_embedding_backfill: no handler")
			}
			n := backfill(ctx)
			if in.Heartbeat != nil {
				_ = in.Heartbeat(ctx)
			}
			return HandlerResult{RowsAffected: int64(n)}, nil
		},
	}
}
