package scheduler

import (
	"context"
	"fmt"
	"time"
)

const JobNameBrainEmbeddingBackfill = "brain_embedding_backfill"

// BrainEmbeddingBackfillJob catches up the Brain search index (OS plan,
// vague B; passages since JEF-412): passages of notes whose index is stale,
// passages whose note is gone, then vectors missing or from another model.
// The write paths embed inline and searches index lazily; this job is what
// makes the ranked search whole after an outage, a model change, or a write
// path that has no embedder (curation, agent effects).
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
