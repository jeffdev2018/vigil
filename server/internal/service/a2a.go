package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Agent-to-agent messaging limits (F19 / JEF-32).
//
// Two circuit breakers, neither of which is an authorization decision:
// canInvokeAgent still judges every hop by the human at the head of the chain.
//
//   - DEPTH stops the SHORT loop. A speaks to B, B answers A, ... Each hop
//     increments agent_task_queue.a2a_depth, and the endpoint refuses past
//     MaxA2ADepth.
//
//   - The per-issue hourly BUDGET is the only net under the WIDE loop, which
//     depth cannot see: A speaks to B on issue 1, B answers on issue 2, and a
//     third path returns to issue 1 with the depth reset to zero because the
//     intermediate trigger was a human. Nothing links those runs, so only a
//     rate on the issue itself catches the fan-out.
//
// BOTH DEFAULTS ARE GUESSES. 4 hops and 20 runs/hour have no measurement
// behind them: 4 may well cut a legitimate squad conversation short, and 20
// was chosen because it is obviously above ordinary traffic and obviously
// below a runaway. They are overridable precisely so a deployment can measure
// its real distribution before anyone freezes a number:
//
//	MULTICA_A2A_MAX_DEPTH                    (default 4)
//	MULTICA_A2A_MAX_RUNS_PER_ISSUE_PER_HOUR  (default 20)
//	MULTICA_A2A_BUDGET_WINDOW                (default 1h, any time.ParseDuration value)
//
// Every refusal is logged at Warn with the count that produced it, so the
// distribution is readable from the logs before the numbers are tuned.
const (
	defaultMaxA2ADepth               = 4
	defaultMaxA2ARunsPerIssuePerHour = 20
	defaultA2ABudgetWindow           = time.Hour
)

// A2AIntents is the closed set the dedicated endpoint accepts. The COLUMN is a
// free string with no CHECK (migration 828) so an older build renders an intent
// it does not know as an ordinary comment instead of failing; this list is the
// write-side gate.
var A2AIntents = []string{"question", "review", "handoff"}

// IsValidA2AIntent reports whether intent is one the endpoint will write.
func IsValidA2AIntent(intent string) bool {
	for _, v := range A2AIntents {
		if v == intent {
			return true
		}
	}
	return false
}

// MaxA2ADepth is the deepest agent-to-agent hop that may be enqueued. A message
// whose resulting run would exceed it is refused with a2a_depth_exceeded.
func MaxA2ADepth() int32 {
	return int32(envPositiveInt("MULTICA_A2A_MAX_DEPTH", defaultMaxA2ADepth))
}

// MaxA2ARunsPerIssuePerHour caps A2A-triggered runs per issue per budget window.
func MaxA2ARunsPerIssuePerHour() int64 {
	return int64(envPositiveInt("MULTICA_A2A_MAX_RUNS_PER_ISSUE_PER_HOUR", defaultMaxA2ARunsPerIssuePerHour))
}

// A2ABudgetWindow is the sliding window the per-issue budget counts over.
func A2ABudgetWindow() time.Duration {
	raw := os.Getenv("MULTICA_A2A_BUDGET_WINDOW")
	if raw == "" {
		return defaultA2ABudgetWindow
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		slog.Warn("invalid MULTICA_A2A_BUDGET_WINDOW, using default", "value", raw, "default", defaultA2ABudgetWindow)
		return defaultA2ABudgetWindow
	}
	return parsed
}

// envPositiveInt reads a strictly positive override, or returns def. Zero and
// negative values are rejected rather than honored: a 0 here would silently
// disable the breaker, which is exactly the failure mode the breaker exists to
// prevent. Disabling one is a deployment decision, so it is spelled with a very
// large number, not with an easy typo.
func envPositiveInt(name string, def int) int {
	raw := os.Getenv(name)
	if raw == "" {
		return def
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		slog.Warn("invalid A2A limit override, using default", "env", name, "value", raw, "default", def)
		return def
	}
	return parsed
}

// A2ADepthForTriggerComment resolves the a2a_depth to stamp on the run this
// trigger comment is about to enqueue: 0 for every ordinary comment, and
// parent.a2a_depth + 1 when the comment carries an a2a_intent.
//
// pgx.ErrNoRows is the "not an A2A message" answer, so the ordinary path costs
// one indexed lookup and no branching. Any OTHER error is logged and also maps
// to 0 rather than to a refusal: an unreadable comment must not stop a run the
// caller was entitled to, and canInvokeAgent — not this counter — is what keeps
// the hop authorized.
func (s *TaskService) A2ADepthForTriggerComment(ctx context.Context, workspaceID, triggerCommentID pgtype.UUID) pgtype.Int4 {
	if s == nil || s.Queries == nil || !triggerCommentID.Valid || !workspaceID.Valid {
		return pgtype.Int4{}
	}
	depth, err := s.Queries.A2ADepthForTriggerComment(ctx, db.A2ADepthForTriggerCommentParams{
		CommentID:   triggerCommentID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("a2a depth lookup failed; enqueuing at depth 0",
				"comment_id", util.UUIDToString(triggerCommentID), "error", err)
		}
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: depth, Valid: true}
}
