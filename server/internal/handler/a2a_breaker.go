package handler

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A2A circuit breakers (F19), in one place because they guard a behaviour, not
// an endpoint.
//
// An agent hands work to another agent two ways: the agent-messages endpoint,
// and a mention link written straight into a comment — which is the way the
// agent's own brief documents ("enqueues a new run for that agent"). Both
// stamp a2a_depth on the run they create. Only the endpoint used to check it,
// so the path the brief tells agents to use was the unguarded one, and a
// ping-pong between two agents on one issue had nothing to stop it.
//
// Both breakers answer from facts the caller already holds — its own chain
// depth, its own issue — so neither can be used to probe the recipient.
func (h *Handler) a2aBreakerBlocked(ctx context.Context, issueID pgtype.UUID, depth int32) DispatchReasonCode {
	if maxDepth := service.MaxA2ADepth(); depth > maxDepth {
		slog.Warn("a2a refused: depth exceeded",
			"issue_id", uuidToString(issueID), "depth", depth, "max_depth", maxDepth)
		return ReasonA2ADepthExceeded
	}
	window := service.A2ABudgetWindow()
	maxRuns := service.MaxA2ARunsPerIssuePerHour()
	used, err := h.Queries.CountA2ARunsForIssueSince(ctx, db.CountA2ARunsForIssueSinceParams{
		IssueID: issueID,
		Since:   pgtype.Timestamptz{Time: time.Now().Add(-window), Valid: true},
	})
	if err != nil {
		// Fail OPEN on an unreadable counter, the same call the endpoint
		// makes: the breaker is a safety net over an already-authorized hop,
		// not an authorization step, and refusing every hand-off in the
		// workspace because one COUNT failed trades a rare runaway for a
		// certain outage. Logged at Warn so it stays visible.
		slog.Warn("a2a budget count failed; allowing the hand-off", "issue_id", uuidToString(issueID), "error", err)
		return ""
	}
	if used >= maxRuns {
		slog.Warn("a2a refused: per-issue budget exceeded",
			"issue_id", uuidToString(issueID), "used", used, "max_runs", maxRuns, "window", window.String())
		return ReasonA2ABudgetExceeded
	}
	return ""
}
