package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// An agent filing an issue on itself must not spawn a run of itself on that
// filing: the native create_issue tool tells the model the issue "starts in the
// default status", and the run that create used to enqueue moved it straight to
// in_progress — and reached the same tool again, with nothing bounding the
// recursion.
func TestCreateWithSuppressRunFilesAssignedIssueWithoutStartingARun(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	q := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	agentUUID := util.MustParseUUID(agentID)

	bus := events.New()
	taskService := &TaskService{Queries: q, TxStarter: pool, Bus: bus, Wakeup: &stubWakeup{}}
	issueService := NewIssueService(q, pool, bus, nil, taskService)

	params := IssueCreateParams{
		WorkspaceID:  workspaceUUID,
		Title:        "Filed by the agent",
		Status:       "todo",
		Priority:     "none",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   agentUUID,
		CreatorType:  "agent",
		CreatorID:    agentUUID,
	}

	suppressed, err := issueService.Create(ctx, params, IssueCreateOpts{SuppressRun: true})
	if err != nil {
		t.Fatalf("Create with SuppressRun: %v", err)
	}
	if suppressed.AssignedTaskID.Valid {
		t.Fatalf("SuppressRun returned an assigned task: %s", util.UUIDToString(suppressed.AssignedTaskID))
	}
	var runs int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, suppressed.Issue.ID).Scan(&runs); err != nil {
		t.Fatalf("count runs on the suppressed issue: %v", err)
	}
	if runs != 0 {
		t.Fatalf("runs on the suppressed issue = %d, want 0", runs)
	}
	if suppressed.Issue.Status != "todo" {
		t.Fatalf("suppressed issue status = %q, want todo", suppressed.Issue.Status)
	}
	if !suppressed.Issue.AssigneeID.Valid || suppressed.Issue.AssigneeID != agentUUID {
		t.Fatalf("suppressed issue assignee = %v, want the filing agent", suppressed.Issue.AssigneeID)
	}

	// The same params without the knob still start the run: SuppressRun is the
	// caller's choice, not a change to the ordinary assign-on-create path.
	params.Title = "Assigned by a member"
	params.CreatorType = "member"
	params.CreatorID = util.MustParseUUID(userID)
	started, err := issueService.Create(ctx, params, IssueCreateOpts{})
	if err != nil {
		t.Fatalf("Create without SuppressRun: %v", err)
	}
	if !started.AssignedTaskID.Valid {
		t.Fatal("ordinary agent assignment on create started no run")
	}
}
