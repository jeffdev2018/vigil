package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// An automatic retry is the SAME piece of work: it keeps the runtime pin
// (a resume child's session lives on that runtime), the handoff note its run
// opens with, and its agent-to-agent hop distance. Losing the pin left a
// fixed-routing agent's retry stamped on a runtime the claim refuses.
func TestAutomaticRetryInheritsPinHandoffAndA2ADepth(t *testing.T) {
	fx, dbfx := fixedRoutingFixture(t)
	ctx := context.Background()
	issueID := dbfx.Issue(t, "resume on the pinned runtime", testutil.Cols{"assignee_type": "agent", "assignee_id": fx.agentID})
	parentID := dbfx.Task(t, fx.agentID, testutil.Cols{
		"runtime_id":     fx.runtimeB,
		"issue_id":       issueID,
		"status":         "running",
		"attempt":        1,
		"max_attempts":   2,
		"runtime_pinned": true,
		"handoff_note":   "Resumed after a pause: continue the migration.",
		"a2a_depth":      2,
		"started_at":     testutil.Raw("now()"),
		"dispatched_at":  testutil.Raw("now()"),
	})
	dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE parent_task_id = $1`, parentID)

	svc := NewTaskService(db.New(fx.pool), fx.pool, nil, events.New())
	if _, err := svc.FailTask(ctx, util.MustParseUUID(parentID), "runtime went offline", "", "", "", "runtime_offline", false, "", ""); err != nil {
		t.Fatalf("FailTask: %v", err)
	}

	var childID string
	var pinned bool
	var note pgtype.Text
	var depth int32
	dbfx.QueryRow(t, `SELECT id::text, runtime_pinned, handoff_note, a2a_depth FROM agent_task_queue WHERE parent_task_id = $1`, parentID).
		Scan(&childID, &pinned, &note, &depth)
	if !pinned || note.String != "Resumed after a pause: continue the migration." || depth != 2 {
		t.Fatalf("retry child pinned=%v note=%q a2a_depth=%d, want the parent's true / note / 2", pinned, note.String, depth)
	}

	dbfx.Exec(t, `UPDATE agent_task_queue SET fire_at = now() - interval '1 second' WHERE id = $1`, childID)
	claimed, err := svc.ClaimTaskForRuntime(ctx, util.MustParseUUID(fx.runtimeB))
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed == nil || util.UUIDToString(claimed.ID) != childID {
		t.Fatalf("pinned runtime could not claim the retry %s (claimed=%v)", childID, claimed)
	}
}
