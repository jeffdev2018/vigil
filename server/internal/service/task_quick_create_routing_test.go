package service

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Validated routing (JEF-275) on the quick-create paths: an agent bound to a
// runtime of another workspace would queue a quick-create no daemon here can
// ever claim. The issue enqueue refuses it; quick-create and its manual retry
// must too.
func TestQuickCreateRefusesAFatalRoutingProblem(t *testing.T) {
	fx, dbfx := fixedRoutingFixture(t)
	ctx := context.Background()
	parentID := seedFailedSourceContextQuickCreate(t, fx, dbfx)
	foreignWS := testutil.New(fx.pool, "", "").Workspace(t, "foreign "+fx.workspace, "foreign-"+fx.workspace)
	foreignRuntime := testutil.New(fx.pool, foreignWS, fx.user).Runtime(t, "foreign runtime")
	dbfx.Exec(t, `UPDATE agent SET runtime_id = $2 WHERE id = $1`, fx.agentID, foreignRuntime)
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE runtime_id = $1`, foreignRuntime)
	svc := NewTaskService(db.New(fx.pool), fx.pool, nil, events.New())

	_, err := svc.EnqueueQuickCreateTask(ctx, util.MustParseUUID(fx.workspace), util.MustParseUUID(fx.user),
		util.MustParseUUID(fx.agentID), pgtype.UUID{}, "create an issue", "none", "", pgtype.UUID{}, pgtype.UUID{}, nil)
	if err == nil || !strings.Contains(err.Error(), RoutingInvalidReason) {
		t.Fatalf("quick-create on a foreign runtime: err = %v, want a %s refusal", err, RoutingInvalidReason)
	}
	_, err = svc.RetrySourceContextQuickCreate(ctx, util.MustParseUUID(fx.workspace), util.MustParseUUID(fx.user),
		util.MustParseUUID(parentID), func(db.Agent) bool { return true })
	if err == nil || !strings.Contains(err.Error(), RoutingInvalidReason) {
		t.Fatalf("quick-create retry on a foreign runtime: err = %v, want a %s refusal", err, RoutingInvalidReason)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE runtime_id = $1`, foreignRuntime); n != 0 {
		t.Fatalf("refused quick-creates left %d tasks on the foreign runtime", n)
	}
}
