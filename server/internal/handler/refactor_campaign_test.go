package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Sharded refactoring campaigns (K42): N shards become N child issues on
// distinct branches with their own runs; a finished shard enters the queue
// only when F10 says it is merge-ready; the queue merges one shard at a
// time in position order; a failed shard is a conflict that files an inbox
// item and holds its position until a human skips it; skipping unblocks
// the rest; the campaign completes when every shard is merged or skipped.

func TestRefactorCampaignMergeQueue(t *testing.T) {
	leader := dbfx.Agent(t, "campaign leader", handlerTestRuntimeID(t))
	agentA := dbfx.Agent(t, "campaign agent a", handlerTestRuntimeID(t))
	agentB := dbfx.Agent(t, "campaign agent b", handlerTestRuntimeID(t))
	parent := dbfx.Issue(t, "Rename the API package", testutil.Cols{"status": "in_progress"})
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE type = $1 AND workspace_id = $2`, InboxTypeMergeConflict, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2, $3)`, leader, agentA, agentB)
		testPool.Exec(ctx, `DELETE FROM campaign_shard WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM refactor_campaign WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM fanout_batch_member WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM fanout_batch WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE parent_issue_id = $1`, parent)
	})
	ctx := context.Background()
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID)
	body := map[string]any{"issue_id": parent, "name": "API rename", "target_branch": "main", "leader_agent_id": leader, "shards": []map[string]any{{"description": "Rename in server/", "assignee_id": agentA}, {"description": "Rename in packages/", "assignee_id": agentB, "branch_name": "feat/rename-packages"}}}
	var out struct{ Campaign *RefactorCampaignResponse }
	testutil.Call(t, testHandler.CreateRefactorCampaign, newRequest(http.MethodPost, "/api/refactor-campaigns", map[string]any{"issue_id": parent, "name": "", "target_branch": "main", "leader_agent_id": leader})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.CreateRefactorCampaign, newRequest(http.MethodPost, "/api/refactor-campaigns", body)).Want(http.StatusCreated).JSON(&out)
	c := out.Campaign
	if c == nil || c.Status != "running" || len(c.Shards) != 2 || c.Shards[0].BranchName != "campaign/api-rename/shard-1" || c.Shards[1].BranchName != "feat/rename-packages" || c.Shards[1].MergePosition != 1 {
		t.Fatalf("campaign = %+v", c)
	}
	if res := testutil.Call(t, testHandler.CreateRefactorCampaign, newRequest(http.MethodPost, "/api/refactor-campaigns", body)).Want(http.StatusConflict); res.Map()["code"] != ErrCodeCampaignActive {
		t.Fatalf("second campaign = %v", res.Map())
	}
	// Each shard is a child issue with the branch in its brief and a queued run for its agent.
	var desc string
	dbfx.QueryRow(t, `SELECT description FROM issue WHERE id = $1`, c.Shards[1].ChildIssueID).Scan(&desc)
	if !strings.Contains(desc, "feat/rename-packages") || !strings.Contains(desc, "against `main`") {
		t.Fatalf("shard brief = %q", desc)
	}
	get := func() RefactorCampaignResponse {
		testutil.Call(t, testHandler.GetRefactorCampaign, testutil.WithURLParams(newRequest(http.MethodGet, "/api/refactor-campaigns/"+c.ID, nil), "id", c.ID)).Want(http.StatusOK).JSON(&out)
		return *out.Campaign
	}
	// Shard 2 finishes first with a green MR: ready, but it waits behind shard 1.
	conn := vcsConnection(t)
	vcsPR(t, conn, c.Shards[1].ChildIssueID, 2, "success")
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, c.Shards[1].TaskID)
	testHandler.updateFanoutBarrier(ctx, mustTask(t, c.Shards[1].TaskID))
	if g := get(); g.Shards[1].MergeStatus != "ready" || g.Shards[0].MergeStatus != "pending" || g.Status != "running" {
		t.Fatalf("shard 2 ready must wait for shard 1: %+v", g.Shards)
	}
	// Shard 1 finishes without a PR: not ready (no_pr), the queue holds.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, c.Shards[0].TaskID)
	testHandler.updateFanoutBarrier(ctx, mustTask(t, c.Shards[0].TaskID))
	g := get()
	if g.Shards[0].MergeStatus != "pending" || len(g.Shards[0].Blockers) == 0 || g.Shards[0].Blockers[0].Kind != blockerNoPR || g.Shards[1].MergeStatus != "ready" {
		t.Fatalf("shard 1 without PR = %+v", g.Shards[0])
	}
	// Its MR goes green later: the next read picks it up and starts its merge run; shard 2 stays ready (one at a time).
	vcsPR(t, conn, c.Shards[0].ChildIssueID, 1, "success")
	g = get()
	if g.Shards[0].MergeStatus != "rebasing" || g.Shards[0].MergeTaskID == nil || g.Shards[1].MergeStatus != "ready" || g.Status != "merging" {
		t.Fatalf("head shard must be rebasing alone: %+v", g.Shards)
	}
	mergeRun := mustTask(t, *g.Shards[0].MergeTaskID)
	if uuidToString(mergeRun.AgentID) != agentA || !strings.Contains(mergeRun.HandoffNote.String, "Rebase branch `campaign/api-rename/shard-1` onto the current `main`") {
		t.Fatalf("merge run = agent %s handoff %q", uuidToString(mergeRun.AgentID), mergeRun.HandoffNote.String)
	}
	if g = get(); g.Shards[1].MergeStatus != "ready" {
		t.Fatalf("a second read must not start a second merge: %+v", g.Shards)
	}
	// The merge run finishes: shard 1 merged, shard 2's merge run starts.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, uuidToString(mergeRun.ID))
	testHandler.updateCampaignMergeRun(ctx, mustTask(t, uuidToString(mergeRun.ID)))
	g = get()
	if g.Shards[0].MergeStatus != "merged" || g.Shards[1].MergeStatus != "rebasing" || g.Shards[1].MergeTaskID == nil {
		t.Fatalf("after merge 1 = %+v", g.Shards)
	}
	// Shard 2's merge run fails for good: conflict, inbox item, queue held, campaign not complete.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', failure_reason = 'agent_error', error = 'conflict in packages/core/index.ts', completed_at = now() WHERE id = $1`, *g.Shards[1].MergeTaskID)
	testHandler.updateCampaignMergeRun(ctx, mustTask(t, *g.Shards[1].MergeTaskID))
	g = get()
	if g.Shards[1].MergeStatus != "conflict" || g.Status != "merging" || len(g.Shards[1].Blockers) != 1 || g.Shards[1].Blockers[0].Kind != blockerMergeConflict {
		t.Fatalf("conflict = %+v status %s", g.Shards[1], g.Status)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE type = $1 AND issue_id = $2 AND recipient_id = $3`, InboxTypeMergeConflict, c.Shards[1].ChildIssueID, testUserID); n != 1 {
		t.Fatalf("conflict inbox items = %d", n)
	}
	testutil.Call(t, testHandler.SkipCampaignShard, testutil.WithURLParams(newRequest(http.MethodPost, "/api/campaign-shards/"+c.Shards[0].ID+"/skip", nil), "id", c.Shards[0].ID)).Want(http.StatusConflict)
	testutil.Call(t, testHandler.SkipCampaignShard, testutil.WithURLParams(newRequest(http.MethodPost, "/api/campaign-shards/"+c.Shards[1].ID+"/skip", nil), "id", c.Shards[1].ID)).Want(http.StatusOK).JSON(&out)
	if out.Campaign.Status != "completed" || out.Campaign.Shards[1].MergeStatus != "skipped" || out.Campaign.CompletedAt == nil {
		t.Fatalf("after skip = %s %+v", out.Campaign.Status, out.Campaign.Shards)
	}
	testutil.Call(t, testHandler.GetIssueRefactorCampaign, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+parent+"/refactor-campaign", nil), "id", parent)).Want(http.StatusOK).JSON(&out)
	if out.Campaign.ID != c.ID {
		t.Fatal("latest campaign for the issue")
	}
}

// A shard whose run failed for good is a conflict before it ever reaches the queue.
func TestRefactorCampaignFailedShardIsConflict(t *testing.T) {
	leader := dbfx.Agent(t, "campaign fail leader", handlerTestRuntimeID(t))
	agentA := dbfx.Agent(t, "campaign fail agent", handlerTestRuntimeID(t))
	parent := dbfx.Issue(t, "Failing campaign")
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE type = $1 AND workspace_id = $2`, InboxTypeMergeConflict, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, leader, agentA)
		testPool.Exec(ctx, `DELETE FROM campaign_shard WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM refactor_campaign WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM fanout_batch_member WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM fanout_batch WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE parent_issue_id = $1`, parent)
	})
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID)
	var out struct{ Campaign *RefactorCampaignResponse }
	testutil.Call(t, testHandler.CreateRefactorCampaign, newRequest(http.MethodPost, "/api/refactor-campaigns", map[string]any{"issue_id": parent, "name": "Solo", "target_branch": "develop", "leader_agent_id": leader, "shards": []map[string]any{{"description": "Only shard", "assignee_id": agentA}}})).Want(http.StatusCreated).JSON(&out)
	shard := out.Campaign.Shards[0]
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'failed', failure_reason = 'agent_error', completed_at = now() WHERE id = $1`, shard.TaskID)
	testHandler.updateFanoutBarrier(context.Background(), mustTask(t, shard.TaskID))
	testutil.Call(t, testHandler.GetRefactorCampaign, testutil.WithURLParams(newRequest(http.MethodGet, "/api/refactor-campaigns/"+out.Campaign.ID, nil), "id", out.Campaign.ID)).Want(http.StatusOK).JSON(&out)
	if out.Campaign.Shards[0].MergeStatus != "conflict" || out.Campaign.Shards[0].Blockers[0].Kind != campaignMergeBlockerRun || out.Campaign.Status != "running" {
		t.Fatalf("failed shard = %+v", out.Campaign.Shards[0])
	}
}
