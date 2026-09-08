package handler

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
)

// The two A2A breakers guard a behaviour, not an endpoint: an agent hands work
// to another agent through the agent-messages endpoint AND through a mention
// link written into a comment, which is the way the agent's own brief
// documents. Both stamp a2a_depth; only the endpoint used to check it.
func TestA2ABreakerBlocked(t *testing.T) {
	ctx := context.Background()
	issue := parseUUID(dbfx.Issue(t, "a2a breaker issue"))

	if got := testHandler.a2aBreakerBlocked(ctx, issue, 1); got != "" {
		t.Errorf("a first hop must pass, got %q", got)
	}
	if got := testHandler.a2aBreakerBlocked(ctx, issue, service.MaxA2ADepth()); got != "" {
		t.Errorf("the cap itself is allowed, got %q", got)
	}
	if got := testHandler.a2aBreakerBlocked(ctx, issue, service.MaxA2ADepth()+1); got != ReasonA2ADepthExceeded {
		t.Errorf("past the cap = %q, want %q", got, ReasonA2ADepthExceeded)
	}

	// The per-issue budget is counted over a window of runs actually created,
	// so it needs the rows to exist; what is pinned here is that the depth
	// verdict is reached before the counter is read, which is what keeps a
	// deep chain from being answered by a database round trip.
	if got := testHandler.a2aBreakerBlocked(ctx, pgtype.UUID{}, service.MaxA2ADepth()+1); got != ReasonA2ADepthExceeded {
		t.Errorf("depth is answered from the caller's own chain, without a lookup, got %q", got)
	}
}
