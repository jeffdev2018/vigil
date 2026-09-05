package scheduler

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// JobNameTriageStaleDigest files the "your triage queue is stalling" inbox
// digest.
const JobNameTriageStaleDigest = "triage_stale_digest"

// TriageStaleDigestJob tells each workspace's admins that its triage queue
// has items nobody has decided on for two days. run is
// handler.RunTriageStaleDigest.
//
// Hourly rather than daily for the same reason as the standup: cadence is a
// duration, not a calendar, and a job that only fires once a day would skip a
// whole day whenever its one slot fell in a restart or an outage. The handler
// dedups on the workspace's own calendar day, so a frequent cadence costs one
// count query per workspace with a backlog and files nothing twice.
func TriageStaleDigestJob(pool *pgxpool.Pool, run func(ctx context.Context, now time.Time) (int, error)) JobSpec {
	return digestJob(pool, JobNameTriageStaleDigest, time.Hour, run)
}
