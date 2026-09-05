package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Goals with ancestry (K74): a mission is a root goal; an active goal needs a
// human owner; a goal never descends from itself; an issue names a goal or
// inherits its project's; progress rolls up the tree from done issues; the
// claim carries the chain mission-first; an agent only proposes an
// attachment and a human decides; deleting a goal detaches its issues and
// projects and is refused while sub-goals remain.

type goalOut struct {
	ID           string   `json:"id"`
	ParentGoalID *string  `json:"parent_goal_id"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	OwnerID      *string  `json:"owner_id"`
	IssueCount   int64    `json:"issue_count"`
	DoneCount    int64    `json:"done_count"`
	ProjectIDs   []string `json:"project_ids"`
}

func TestGoals(t *testing.T) {
	ctx := context.Background()
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM issue_decision WHERE workspace_id = $1 AND question LIKE 'Goal · %'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM project_goal WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM goal WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM decision_search_chunk WHERE source_type = 'goal' AND workspace_id = $1`, testWorkspaceID)
	})
	create := func(body map[string]any, want int) goalOut {
		var out goalOut
		res := testutil.Call(t, testHandler.CreateGoal, newRequest(http.MethodPost, "/api/goals", body)).Want(want)
		if want == http.StatusCreated {
			res.JSON(&out)
		}
		return out
	}
	update := func(id string, body map[string]any, want int) goalOut {
		var out goalOut
		res := testutil.Call(t, testHandler.UpdateGoal, testutil.WithURLParams(newRequest(http.MethodPut, "/api/goals/"+id, body), "id", id)).Want(want)
		if want == http.StatusOK {
			res.JSON(&out)
		}
		return out
	}
	list := func() map[string]goalOut {
		var out struct {
			Goals []goalOut `json:"goals"`
		}
		testutil.Call(t, testHandler.ListGoals, newRequest(http.MethodGet, "/api/goals", nil)).Want(http.StatusOK).JSON(&out)
		m := map[string]goalOut{}
		for _, g := range out.Goals {
			m[g.ID] = g
		}
		return m
	}

	// A mission without an owner stays a draft; activating it needs a member owner.
	create(map[string]any{"title": "   "}, http.StatusBadRequest)
	create(map[string]any{"title": "Be profitable", "status": "active"}, http.StatusBadRequest)
	create(map[string]any{"title": "Be profitable", "status": "active", "owner_id": uuid.NewString()}, http.StatusBadRequest)
	mission := create(map[string]any{"title": "Be profitable", "success_measure": "positive cash flow by Q4"}, http.StatusCreated)
	if mission.Status != "draft" || mission.OwnerID != nil {
		t.Fatalf("mission defaults: %+v", mission)
	}
	mission = update(mission.ID, map[string]any{"status": "active", "owner_id": testUserID}, http.StatusOK)
	if mission.Status != "active" || mission.OwnerID == nil || *mission.OwnerID != testUserID {
		t.Fatalf("activated mission: %+v", mission)
	}
	// Clearing the owner of an active goal is refused; dropping it first works.
	update(mission.ID, map[string]any{"owner_id": nil}, http.StatusBadRequest)

	sub := create(map[string]any{"title": "Ship billing", "parent_goal_id": mission.ID, "success_measure": "every seat invoiced", "status": "active", "owner_id": testUserID}, http.StatusCreated)
	if sub.ParentGoalID == nil || *sub.ParentGoalID != mission.ID {
		t.Fatalf("sub-goal parent: %+v", sub)
	}
	create(map[string]any{"title": "orphan", "parent_goal_id": uuid.NewString()}, http.StatusBadRequest)
	// The mission cannot hang under its own sub-goal.
	update(mission.ID, map[string]any{"parent_goal_id": sub.ID}, http.StatusBadRequest)
	update(sub.ID, map[string]any{"parent_goal_id": sub.ID}, http.StatusBadRequest)

	// A project serves the sub-goal; its issues inherit it. Another issue names the mission itself.
	project := dbfx.Project(t, "billing project "+uuid.NewString()[:8])
	var linked struct {
		GoalIDs []string `json:"goal_ids"`
	}
	testutil.Call(t, testHandler.SetProjectGoals, testutil.WithURLParams(newRequest(http.MethodPut, "/api/projects/"+project+"/goals", map[string]any{"goal_ids": []string{sub.ID}}), "id", project)).Want(http.StatusOK).JSON(&linked)
	if len(linked.GoalIDs) != 1 || linked.GoalIDs[0] != sub.ID {
		t.Fatalf("project goals: %+v", linked)
	}
	testutil.Call(t, testHandler.SetProjectGoals, testutil.WithURLParams(newRequest(http.MethodPut, "/api/projects/"+project+"/goals", map[string]any{"goal_ids": []string{uuid.NewString()}}), "id", project)).Want(http.StatusBadRequest)
	inherited := dbfx.Issue(t, "inherits goal "+uuid.NewString()[:8], testutil.Cols{"project_id": project})
	direct := dbfx.Issue(t, "names mission "+uuid.NewString()[:8])
	var updated struct {
		GoalID *string `json:"goal_id"`
	}
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(newRequest(http.MethodPut, "/api/issues/"+direct, map[string]any{"goal_id": mission.ID}), "id", direct)).Want(http.StatusOK).JSON(&updated)
	if updated.GoalID == nil || *updated.GoalID != mission.ID {
		t.Fatalf("issue goal after update: %+v", updated)
	}
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(newRequest(http.MethodPut, "/api/issues/"+direct, map[string]any{"goal_id": uuid.NewString()}), "id", direct)).Want(http.StatusBadRequest)
	dbfx.Exec(t, `UPDATE workspace SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) WHERE id = $1`, testWorkspaceID)
	var created struct {
		ID     string  `json:"id"`
		GoalID *string `json:"goal_id"`
	}
	testutil.Call(t, testHandler.CreateIssue, newRequest(http.MethodPost, "/api/issues", map[string]any{"title": "created with goal", "goal_id": sub.ID})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, created.ID) })
	if created.GoalID == nil || *created.GoalID != sub.ID {
		t.Fatalf("issue created with goal: %+v", created)
	}

	// Progress: the sub-goal counts its inherited and named issues; the mission rolls them up with its own.
	goals := list()
	if goals[sub.ID].IssueCount != 2 || goals[sub.ID].DoneCount != 0 || goals[mission.ID].IssueCount != 3 {
		t.Fatalf("progress before done: sub=%+v mission=%+v", goals[sub.ID], goals[mission.ID])
	}
	if len(goals[sub.ID].ProjectIDs) != 1 || goals[sub.ID].ProjectIDs[0] != project {
		t.Fatalf("sub-goal projects: %+v", goals[sub.ID].ProjectIDs)
	}
	dbfx.Exec(t, `UPDATE issue SET status = 'done' WHERE id = $1`, inherited)
	goals = list()
	if goals[sub.ID].DoneCount != 1 || goals[mission.ID].DoneCount != 1 {
		t.Fatalf("progress after done: sub=%+v mission=%+v", goals[sub.ID], goals[mission.ID])
	}
	var detail struct {
		Goal   goalOut `json:"goal"`
		Issues []struct {
			ID string `json:"id"`
		} `json:"issues"`
	}
	testutil.Call(t, testHandler.GetGoal, testutil.WithURLParams(newRequest(http.MethodGet, "/api/goals/"+sub.ID, nil), "id", sub.ID)).Want(http.StatusOK).JSON(&detail)
	ids := map[string]bool{}
	for _, i := range detail.Issues {
		ids[i.ID] = true
	}
	if !ids[inherited] || !ids[created.ID] || ids[direct] {
		t.Fatalf("goal issues: %v", ids)
	}
	// The issue list filters by goal with the same inheritance.
	var listed struct {
		Issues []struct {
			ID     string  `json:"id"`
			GoalID *string `json:"goal_id"`
		} `json:"issues"`
	}
	testutil.Call(t, testHandler.ListIssues, newRequest(http.MethodGet, "/api/issues?goal_id="+sub.ID, nil)).Want(http.StatusOK).JSON(&listed)
	ids = map[string]bool{}
	for _, i := range listed.Issues {
		ids[i.ID] = true
	}
	if !ids[inherited] || !ids[created.ID] || ids[direct] {
		t.Fatalf("issue list by goal: %v", ids)
	}

	// The claim carries the chain mission-first, with success measures.
	chain, err := testHandler.resolveClaimMissionChain(ctx, mustIssue(t, inherited))
	if err != nil || len(chain) != 2 || chain[0].ID != mission.ID || chain[1].ID != sub.ID || chain[1].SuccessMeasure != "every seat invoiced" || chain[0].Depth != 1 {
		t.Fatalf("mission chain: %+v err=%v", chain, err)
	}
	if chain, _ := testHandler.resolveClaimMissionChain(ctx, mustIssue(t, dbfx.Issue(t, "no goal"))); len(chain) != 0 {
		t.Fatalf("an issue without goal carries no chain: %+v", chain)
	}
	// The briefing names the active goals with their progress.
	found := false
	for _, g := range testHandler.briefingGoals(ctx, parseUUID(testWorkspaceID)) {
		if g.ID == mission.ID && g.IssueCount == 3 && g.DoneCount == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("briefing goals miss the mission")
	}
	// The why index knows the goal.
	if dbfx.Count(t, `SELECT COUNT(*) FROM decision_search_chunk WHERE source_type = 'goal' AND source_id = $1`, mission.ID) != 1 {
		t.Fatal("goal not indexed for why search")
	}

	// An agent proposes an attachment; only a human decision lands it.
	agent := dbfx.Agent(t, "goal agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	proposed := dbfx.Issue(t, "proposed "+uuid.NewString()[:8], testutil.Cols{"assignee_type": "agent", "assignee_id": agent})
	task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": proposed, "status": "running"})
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue_decision WHERE issue_id = $1`, proposed) })
	testutil.Call(t, testHandler.ProposeIssueGoal, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+proposed+"/goal-proposal", map[string]any{"goal_id": sub.ID, "reason": "billing work"}), "id", proposed)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.ProposeIssueGoal, testutil.WithURLParams(runRequest(agent, task, http.MethodPost, "/api/issues/"+proposed+"/goal-proposal", map[string]any{"goal_id": sub.ID, "reason": ""}), "id", proposed)).Want(http.StatusBadRequest)
	// An agent writing goal_id straight onto the issue is ignored.
	testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(runRequest(agent, task, http.MethodPut, "/api/issues/"+proposed, map[string]any{"goal_id": sub.ID}), "id", proposed)).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE id = $1 AND goal_id IS NOT NULL`, proposed) != 0 {
		t.Fatal("an agent must not set the goal directly")
	}
	var filed struct {
		Decision struct {
			ID      string `json:"id"`
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		} `json:"decision"`
	}
	testutil.Call(t, testHandler.ProposeIssueGoal, testutil.WithURLParams(runRequest(agent, task, http.MethodPost, "/api/issues/"+proposed+"/goal-proposal", map[string]any{"goal_id": sub.ID, "reason": "billing work"}), "id", proposed)).Want(http.StatusCreated).JSON(&filed)
	testutil.Call(t, testHandler.ProposeIssueGoal, testutil.WithURLParams(runRequest(agent, task, http.MethodPost, "/api/issues/"+proposed+"/goal-proposal", map[string]any{"goal_id": sub.ID, "reason": "again"}), "id", proposed)).Want(http.StatusConflict)
	if len(filed.Decision.Options) != 2 || !strings.HasPrefix(filed.Decision.Options[0].ID, "goal:") {
		t.Fatalf("proposal options: %+v", filed.Decision.Options)
	}
	respondDecision(t, proposed, filed.Decision.ID, map[string]any{"option_id": "goal_keep"}).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE id = $1 AND goal_id IS NOT NULL`, proposed) != 0 {
		t.Fatal("keep must leave the issue alone")
	}
	testutil.Call(t, testHandler.ProposeIssueGoal, testutil.WithURLParams(runRequest(agent, task, http.MethodPost, "/api/issues/"+proposed+"/goal-proposal", map[string]any{"goal_id": sub.ID, "reason": "billing work"}), "id", proposed)).Want(http.StatusCreated).JSON(&filed)
	respondDecision(t, proposed, filed.Decision.ID, map[string]any{"option_id": filed.Decision.Options[0].ID}).Want(http.StatusOK)
	var attached string
	dbfx.QueryRow(t, `SELECT goal_id::text FROM issue WHERE id = $1`, proposed).Scan(&attached)
	if attached != sub.ID {
		t.Fatalf("attached goal: %q", attached)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, proposed) != 1 {
		t.Fatal("a goal decision resumes no run")
	}

	// Deleting: refused while sub-goals remain; a leaf goal detaches its issues and projects.
	testutil.Call(t, testHandler.DeleteGoal, testutil.WithURLParams(newRequest(http.MethodDelete, "/api/goals/"+mission.ID, nil), "id", mission.ID)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.DeleteGoal, testutil.WithURLParams(newRequest(http.MethodDelete, "/api/goals/"+sub.ID, nil), "id", sub.ID)).Want(http.StatusNoContent)
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue WHERE goal_id = $1`, sub.ID) != 0 || dbfx.Count(t, `SELECT COUNT(*) FROM project_goal WHERE goal_id = $1`, sub.ID) != 0 {
		t.Fatal("deleting a goal detaches issues and projects")
	}
	if _, ok := list()[sub.ID]; ok {
		t.Fatal("deleted goal still listed")
	}
	testutil.Call(t, testHandler.DeleteGoal, testutil.WithURLParams(newRequest(http.MethodDelete, "/api/goals/"+mission.ID, nil), "id", mission.ID)).Want(http.StatusNoContent)
}
