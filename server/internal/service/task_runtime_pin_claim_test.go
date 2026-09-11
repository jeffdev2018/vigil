package service

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fixedRoutingFixture is the routing fixture with its agent switched to fixed
// routing: bound to runtime A, never allowed on B unless a row says so.
func fixedRoutingFixture(t *testing.T) (routingTestFixture, *testutil.Fixture) {
	t.Helper()
	fx := newRoutingTestFixture(t)
	dbfx := testutil.New(fx.pool, fx.workspace, fx.user)
	dbfx.Exec(t, `UPDATE agent SET runtime_routing = 'fixed' WHERE id = $1`, fx.agentID)
	return fx, dbfx
}

// A runtime-pinned row (a raced run-group attempt, or a resume child) is
// claimable by exactly its pinned runtime even when the agent is bound
// elsewhere. The Go pre-filter used to ignore runtime_pinned, so the only
// runtime allowed to run the task was refused on every poll.
func TestClaimTaskForRuntimeHonoursRuntimePinnedRow(t *testing.T) {
	fx, dbfx := fixedRoutingFixture(t)
	issueID := dbfx.Issue(t, "pinned attempt")
	taskID := dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id":     fx.runtimeB,
		"issue_id":       issueID,
		"runtime_pinned": true,
	})

	svc := NewTaskService(db.New(fx.pool), fx.pool, nil, events.New())
	claimed, err := svc.ClaimTaskForRuntime(context.Background(), util.MustParseUUID(fx.runtimeB))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed == nil || util.UUIDToString(claimed.ID) != taskID {
		t.Fatalf("pinned task %s was not claimed by its pinned runtime (claimed=%v)", taskID, claimed)
	}
}
