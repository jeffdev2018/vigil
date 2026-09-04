package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// K42 debts: a ready shard is merged through the platform API without an
// agent run; a platform refusal is a conflict; when no platform can merge
// the agent run remains the fallback; the scheduler entry moves queues.

type fakeMerger struct {
	merged, conflict bool
	err              error
	calls            int
}

func (f *fakeMerger) MergeIssuePullRequest(context.Context, db.Issue) (bool, bool, string, error) {
	f.calls++
	return f.merged, f.conflict, "no way", f.err
}

func campaignFixture(t *testing.T, name string, shards int) (RefactorCampaignResponse, string, []string) {
	t.Helper()
	leader := dbfx.Agent(t, name+" leader", handlerTestRuntimeID(t))
	agents := make([]string, 0, shards)
	body := map[string]any{"issue_id": "", "name": name, "target_branch": "main", "leader_agent_id": leader, "shards": []map[string]any{}}
	for i := 0; i < shards; i++ {
		a := dbfx.Agent(t, fmt.Sprintf("%s agent %d", name, i+1), handlerTestRuntimeID(t))
		agents = append(agents, a)
		body["shards"] = append(body["shards"].([]map[string]any), map[string]any{"description": name + " shard", "assignee_id": a})
	}
	parent := dbfx.Issue(t, name+" parent", testutil.Cols{"status": "in_progress"})
	body["issue_id"] = parent
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE type = $1 AND workspace_id = $2`, InboxTypeMergeConflict, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id = ANY($1::uuid[])`, append(agents, leader))
		testPool.Exec(ctx, `DELETE FROM campaign_shard WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM refactor_campaign WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM fanout_batch_member WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM fanout_batch WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE parent_issue_id = $1`, parent)
	})
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID)
	var out struct{ Campaign *RefactorCampaignResponse }
	testutil.Call(t, testHandler.CreateRefactorCampaign, newRequest(http.MethodPost, "/api/refactor-campaigns", body)).Want(http.StatusCreated).JSON(&out)
	return *out.Campaign, parent, agents
}

func TestCampaignMergesThroughThePlatform(t *testing.T) {
	prev := testHandler.PRMerger
	merger := &fakeMerger{merged: true}
	testHandler.PRMerger = merger
	t.Cleanup(func() { testHandler.PRMerger = prev })
	c, _, _ := campaignFixture(t, "api merge", 2)
	conn := vcsConnection(t)
	ctx := context.Background()
	for i, s := range c.Shards {
		vcsPR(t, conn, s.ChildIssueID, 10+i, "success")
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, s.TaskID)
		testHandler.updateFanoutBarrier(ctx, mustTask(t, s.TaskID))
	}
	var out struct{ Campaign *RefactorCampaignResponse }
	testutil.Call(t, testHandler.GetRefactorCampaign, testutil.WithURLParams(newRequest(http.MethodGet, "/api/refactor-campaigns/"+c.ID, nil), "id", c.ID)).Want(http.StatusOK).JSON(&out)
	if out.Campaign.Status != "completed" || out.Campaign.Shards[0].MergeStatus != "merged" || out.Campaign.Shards[1].MergeStatus != "merged" || out.Campaign.Shards[0].MergeTaskID != nil {
		t.Fatalf("api-merged campaign = %s %+v", out.Campaign.Status, out.Campaign.Shards)
	}
	if merger.calls != 2 {
		t.Fatalf("merger calls = %d, want one per shard, in order", merger.calls)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND details->>'via' = 'api' AND details->>'campaign_id' = $2`, AuditCampaign, c.ID); n != 2 {
		t.Fatalf("api merge audit rows = %d", n)
	}
}

func TestCampaignPlatformRefusalIsConflictAndErrorFallsBackToAgent(t *testing.T) {
	prev := testHandler.PRMerger
	merger := &fakeMerger{conflict: true}
	testHandler.PRMerger = merger
	t.Cleanup(func() { testHandler.PRMerger = prev })
	c, _, agents := campaignFixture(t, "api conflict", 1)
	conn := vcsConnection(t)
	ctx := context.Background()
	s := c.Shards[0]
	vcsPR(t, conn, s.ChildIssueID, 21, "success")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, s.TaskID)
	testHandler.updateFanoutBarrier(ctx, mustTask(t, s.TaskID))
	var out struct{ Campaign *RefactorCampaignResponse }
	get := func() RefactorCampaignResponse {
		testutil.Call(t, testHandler.GetRefactorCampaign, testutil.WithURLParams(newRequest(http.MethodGet, "/api/refactor-campaigns/"+c.ID, nil), "id", c.ID)).Want(http.StatusOK).JSON(&out)
		return *out.Campaign
	}
	if g := get(); g.Shards[0].MergeStatus != "conflict" || len(g.Shards[0].Blockers) != 1 || g.Shards[0].Blockers[0].Kind != blockerMergeConflict {
		t.Fatalf("refused shard = %+v", g.Shards[0])
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2`, InboxTypeMergeConflict, s.ChildIssueID); n == 0 {
		t.Fatal("a platform refusal must reach a human")
	}
	// No platform: the agent merge run is the fallback.
	merger.conflict, merger.err = false, errors.New("no pull request")
	c2, _, _ := campaignFixture(t, "api fallback", 1)
	s2 := c2.Shards[0]
	vcsPR(t, conn, s2.ChildIssueID, 22, "success")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, s2.TaskID)
	testHandler.updateFanoutBarrier(ctx, mustTask(t, s2.TaskID))
	testutil.Call(t, testHandler.GetRefactorCampaign, testutil.WithURLParams(newRequest(http.MethodGet, "/api/refactor-campaigns/"+c2.ID, nil), "id", c2.ID)).Want(http.StatusOK).JSON(&out)
	if out.Campaign.Shards[0].MergeStatus != "rebasing" || out.Campaign.Shards[0].MergeTaskID == nil {
		t.Fatalf("fallback shard = %+v", out.Campaign.Shards[0])
	}
	_ = agents
}

func TestCampaignSweeperAdvancesQueues(t *testing.T) {
	prev := testHandler.PRMerger
	merger := &fakeMerger{merged: true}
	testHandler.PRMerger = merger
	t.Cleanup(func() { testHandler.PRMerger = prev })
	c, _, _ := campaignFixture(t, "api sweep", 1)
	conn := vcsConnection(t)
	ctx := context.Background()
	s := c.Shards[0]
	// The run finished before its PR existed: the settlement left it pending (no_pr).
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, s.TaskID)
	testHandler.updateFanoutBarrier(ctx, mustTask(t, s.TaskID))
	vcsPR(t, conn, s.ChildIssueID, 31, "success")
	moved, err := testHandler.AdvanceCampaignMergeQueues(ctx)
	if err != nil || moved < 1 {
		t.Fatalf("sweep moved %d err %v", moved, err)
	}
	var status string
	dbfx.QueryRow(t, `SELECT merge_status FROM campaign_shard WHERE id = $1`, s.ID).Scan(&status)
	if status != "merged" {
		t.Fatalf("after sweep shard = %s", status)
	}
}
