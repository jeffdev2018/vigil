package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Skill Miner (K58): a member comment right after an agent comment is a
// correction signal (an unrelated member comment is not); three similar
// signals of one agent become a draft skill listing its sources, two do
// not; a draft cannot be attached to an agent until published; dismissing
// a draft keeps the signals.

func TestSkillMiner(t *testing.T) {
	ctx := context.Background()
	agent := dbfx.Agent(t, "miner agent "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	other := dbfx.Agent(t, "miner other "+uuid.NewString()[:8], handlerTestRuntimeID(t), testutil.Cols{"trust_mode": "autonomous"})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_correction_signal WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_skill WHERE skill_id IN (SELECT id FROM skill WHERE workspace_id = $1 AND name LIKE 'mined-%')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM skill WHERE workspace_id = $1 AND name LIKE 'mined-%'`, testWorkspaceID)
	})
	testPool.Exec(ctx, `DELETE FROM agent_correction_signal WHERE workspace_id = $1`, testWorkspaceID)

	correct := func(title, agentText, memberText string) string {
		issue := dbfx.Issue(t, title, testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
		task := dbfx.Task(t, agent, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
		testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(
			runRequest(agent, task, http.MethodPost, "/api/issues/"+issue+"/comments", map[string]any{"content": agentText, "type": "comment"}), "id", issue)).Want(http.StatusCreated)
		testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(
			newRequest(http.MethodPost, "/api/issues/"+issue+"/comments", map[string]any{"content": memberText, "type": "comment"}), "id", issue)).Want(http.StatusCreated)
		return issue
	}
	// A member comment with no agent comment before it is not a signal.
	lone := dbfx.Issue(t, "miner lone", testutil.Cols{"status": "todo"})
	testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issues/"+lone+"/comments", map[string]any{"content": "Please always add tests before marking done", "type": "comment"}), "id", lone)).Want(http.StatusCreated)
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_correction_signal WHERE issue_id = $1`, lone) != 0 {
		t.Fatal("no agent to correct, no signal")
	}
	// Two similar corrections: a signal each, no draft.
	correct("miner issue one", "Done, I updated the handler.", "Please always add unit tests before you mark the handler done")
	correct("miner issue two", "Implemented the endpoint.", "You forgot the unit tests again: add tests before marking done")
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_correction_signal WHERE workspace_id = $1 AND agent_id = $2`, testWorkspaceID, agent) != 2 {
		t.Fatal("each correction is a signal")
	}
	dbfx.Exec(t, `UPDATE agent_correction_signal SET detected_at = now() - interval '1 hour' WHERE workspace_id = $1`, testWorkspaceID)
	if n, err := testHandler.MineSkills(ctx, time.Now()); err != nil || n != 0 {
		t.Fatalf("two signals must not draft: n=%d err=%v", n, err)
	}
	// A third similar one, plus an unrelated correction of another agent: one draft, for this agent only.
	correct("miner issue three", "Fixed the bug.", "Add the unit tests before marking it done, please")
	otherIssue := dbfx.Issue(t, "miner other issue", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": other})
	otherTask := dbfx.Task(t, other, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": otherIssue, "status": "running"})
	testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(runRequest(other, otherTask, http.MethodPost, "/api/issues/"+otherIssue+"/comments", map[string]any{"content": "Sent the newsletter.", "type": "comment"}), "id", otherIssue)).Want(http.StatusCreated)
	testutil.Call(t, testHandler.CreateComment, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+otherIssue+"/comments", map[string]any{"content": "Wrong tone for the newsletter subject line", "type": "comment"}), "id", otherIssue)).Want(http.StatusCreated)
	dbfx.Exec(t, `UPDATE agent_correction_signal SET detected_at = now() - interval '1 hour' WHERE workspace_id = $1`, testWorkspaceID)
	if n, err := testHandler.MineSkills(ctx, time.Now()); err != nil || n != 1 {
		t.Fatalf("three similar signals draft one skill: n=%d err=%v", n, err)
	}
	var skillID, status, config string
	dbfx.QueryRow(t, `SELECT id::text, status, config::text FROM skill WHERE workspace_id = $1 AND name LIKE 'mined-%' ORDER BY created_at DESC LIMIT 1`, testWorkspaceID).Scan(&skillID, &status, &config)
	if status != "draft" || !strings.Contains(config, `"skill_miner"`) || !strings.Contains(config, agent) {
		t.Fatalf("draft = %s %s", status, config)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_correction_signal WHERE mined_skill_id = $1`, skillID) != 3 ||
		dbfx.Count(t, `SELECT COUNT(*) FROM agent_correction_signal WHERE agent_id = $1 AND mined_skill_id IS NULL`, other) != 1 {
		t.Fatal("the three signals are mined, the other agent's lone one is not")
	}
	// Drafts endpoint lists the draft with its sources.
	var drafts struct {
		Drafts []SkillDraftResponse `json:"drafts"`
	}
	testutil.Call(t, testHandler.ListSkillDrafts, newRequest(http.MethodGet, "/api/skill-miner/drafts", nil)).Want(http.StatusOK).JSON(&drafts)
	var found *SkillDraftResponse
	for i := range drafts.Drafts {
		if drafts.Drafts[i].ID == skillID {
			found = &drafts.Drafts[i]
		}
	}
	if found == nil || len(found.Sources) != 3 || found.Status != "draft" {
		t.Fatalf("drafts = %+v", drafts.Drafts)
	}
	// A draft cannot be attached; once published it can.
	testutil.Call(t, testHandler.AddAgentSkills, testutil.WithURLParams(newRequest(http.MethodPost, "/api/agents/"+agent+"/skills/add", map[string]any{"skill_ids": []string{skillID}}), "id", agent)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.UpdateSkill, testutil.WithURLParams(newRequest(http.MethodPut, "/api/skills/"+skillID, map[string]any{"status": "published"}), "id", skillID)).Want(http.StatusOK)
	testutil.Call(t, testHandler.AddAgentSkills, testutil.WithURLParams(newRequest(http.MethodPost, "/api/agents/"+agent+"/skills/add", map[string]any{"skill_ids": []string{skillID}}), "id", agent)).Want(http.StatusOK)
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, agent, skillID) != 1 {
		t.Fatal("a published draft attaches like any skill")
	}
	// Dismissing (deleting) a draft keeps its signals.
	testutil.Call(t, testHandler.DeleteSkill, testutil.WithURLParams(newRequest(http.MethodDelete, "/api/skills/"+skillID, nil), "id", skillID)).Want(http.StatusNoContent)
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_correction_signal WHERE mined_skill_id = $1`, skillID) != 3 {
		t.Fatal("dismissing a draft keeps the source signals")
	}
}
