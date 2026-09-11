package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Budget admission: every path that creates a runnable task reserves budget
// in the transaction that creates it, so an exhausted enforced cap refuses it.

// exhaustBudget drops the fixture policy's limit below any estimate.
func (b budgetFixture) exhaustBudget(t *testing.T) {
	t.Helper()
	b.dbfx.Exec(t, `UPDATE budget_policy SET limit_usd_ticks = 1 WHERE id = $1`, b.policyID)
}

func TestBudgetRefusesManualQuickCreateRetry(t *testing.T) {
	b := newBudgetFixture(t)
	ctx := context.Background()
	sourceIssueID := b.dbfx.Issue(t, "branch point")
	contextID := dbid.NewV7()
	payload, err := json.Marshal(QuickCreateContext{
		Type: QuickCreateContextType, Prompt: "retry this", RequesterID: b.user,
		WorkspaceID: b.workspace, SourceContextID: util.UUIDToString(contextID),
	})
	if err != nil {
		t.Fatal(err)
	}
	parentID := b.dbfx.Task(t, b.agentID, testutil.Cols{
		"runtime_id": b.runtimeA, "status": "failed", "context": payload,
		"originator_user_id": b.user, "accountable_user_id": b.user,
	})
	b.dbfx.Exec(t, `INSERT INTO issue_source_context (
			id, workspace_id, origin_task_id, source_issue_id, anchor_comment_id,
			captured_by_user_id, snapshot_version, snapshot, capture_digest, state
		) VALUES ($1, $2, $3, $4, gen_random_uuid(), $5, 1, '{}'::jsonb, 'digest', 'pending')`,
		contextID, b.workspace, parentID, sourceIssueID, b.user)
	b.dbfx.Cleanup(t, `DELETE FROM issue_source_context WHERE id = $1`, contextID)
	b.dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE rerun_of_task_id = $1`, parentID)
	b.exhaustBudget(t)

	_, err = b.svc.RetrySourceContextQuickCreate(ctx, util.MustParseUUID(b.workspace), util.MustParseUUID(b.user),
		util.MustParseUUID(parentID), func(db.Agent) bool { return true })
	var exceeded *BudgetExceededError
	if !errors.As(err, &exceeded) {
		t.Fatalf("retry under an exhausted cap: err = %v, want *BudgetExceededError", err)
	}
	if n := b.dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE rerun_of_task_id = $1`, parentID); n != 0 {
		t.Fatalf("refused retry left %d tasks", n)
	}
}

func TestBudgetRefusesDirectChatMessage(t *testing.T) {
	b := newBudgetFixture(t)
	ctx := context.Background()
	sessionID := b.dbfx.ChatSession(t, b.agentID)
	session, err := b.svc.Queries.GetChatSession(ctx, util.MustParseUUID(sessionID))
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	agent, err := b.svc.Queries.GetAgent(ctx, util.MustParseUUID(b.agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	b.exhaustBudget(t)

	_, err = b.svc.SendDirectChatMessage(ctx, session, agent, util.MustParseUUID(b.user), "hello", nil, "member", util.MustParseUUID(b.user))
	var exceeded *BudgetExceededError
	if !errors.As(err, &exceeded) {
		t.Fatalf("direct chat under an exhausted cap: err = %v, want *BudgetExceededError", err)
	}
	if n := b.dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE chat_session_id = $1`, sessionID); n != 0 {
		t.Fatalf("refused chat send left %d tasks", n)
	}
}

// The channel /issue path creates the issue and its media-gated task in one
// transaction. Under an exhausted cap the issue still lands — like an ordinary
// assignment whose enqueue is refused — but no unreserved task does.
func TestBudgetRefusesDeferredChannelIssueTaskButKeepsTheIssue(t *testing.T) {
	b := newBudgetFixture(t)
	ctx := context.Background()
	b.dbfx.Cleanup(t, `DELETE FROM agent_task_queue WHERE agent_id = $1`, b.agentID)
	b.dbfx.Cleanup(t, `DELETE FROM issue WHERE workspace_id = $1 AND title = 'Channel issue over budget'`, b.workspace)
	b.exhaustBudget(t)
	issues := NewIssueService(b.svc.Queries, b.pool, events.New(), nil, b.svc)

	result, err := issues.Create(ctx, IssueCreateParams{
		WorkspaceID:  util.MustParseUUID(b.workspace),
		Title:        "Channel issue over budget",
		Status:       "todo",
		Priority:     "medium",
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   util.MustParseUUID(b.agentID),
		CreatorType:  "member",
		CreatorID:    util.MustParseUUID(b.user),
	}, IssueCreateOpts{AssignedAgentRunFireAt: time.Now().Add(time.Minute)})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if n := b.dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, util.UUIDToString(result.Issue.ID)); n != 0 {
		t.Fatalf("issue created under an exhausted cap carries %d unreserved tasks", n)
	}
}
