package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Learned competency (K43): the domain is the first label, else the most
// touched top-level directory; acceptance of an agent-assigned issue adds a
// success, reopening removes it, cancellation adds a plain attempt; a
// confirmed duel adds a win and a loss weighted twice; a small sample is
// flagged unreliable rather than shown as a bare percentage; the
// suggestion ranks reliable agents first; the tally is per workspace.

func TestCompetencyDomainKey(t *testing.T) {
	if k := competencyDomainKey([]string{" Backend "}, []string{"server/x.go"}); k != "label:backend" {
		t.Fatalf("label domain = %s", k)
	}
	if k := competencyDomainKey(nil, []string{"server/a.go", "packages/core/b.ts", "./server/c.go", "README.md"}); k != "path:server" {
		t.Fatalf("path domain = %s", k)
	}
	if k := competencyDomainKey(nil, []string{"b/x", "a/y"}); k != "path:a" {
		t.Fatalf("tie must be deterministic: %s", k)
	}
	if k := competencyDomainKey(nil, nil); k != competencyDomainGeneral {
		t.Fatalf("empty domain = %s", k)
	}
	if s := competencyScore(3, 4, 1, 1); s != (3+2)/float64(4+4) {
		t.Fatalf("score = %v", s)
	}
	if competencyScore(0, 0, 0, 0) != 0 {
		t.Fatal("empty score must be 0")
	}
}

func TestCompetencyFollowsIssueOutcomesAndDuels(t *testing.T) {
	agentA := dbfx.Agent(t, "competency agent a", handlerTestRuntimeID(t))
	agentB := dbfx.Agent(t, "competency agent b", handlerTestRuntimeID(t))
	rememberSettings(t)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM agent_domain_competency WHERE agent_id IN ($1, $2)`, agentA, agentB)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE agent_id IN ($1, $2)`, agentA, agentB)
		testPool.Exec(ctx, `DELETE FROM agent_duel WHERE workspace_id = $1`, testWorkspaceID)
	})
	issue := dbfx.Issue(t, "Fix server/internal/handler/issue.go and server/cmd/main.go", testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agentA})
	patch := func(id string, body map[string]any) {
		t.Helper()
		testutil.Call(t, testHandler.UpdateIssue, testutil.WithURLParams(newRequest(http.MethodPatch, "/api/issues/"+id, body), "id", id)).Want(http.StatusOK)
	}
	type competency struct {
		AgentID   string          `json:"agent_id"`
		MinSample int             `json:"min_sample"`
		Rows      []CompetencyRow `json:"rows"`
	}
	get := func(agent string) competency {
		t.Helper()
		var out competency
		testutil.Call(t, testHandler.GetAgentCompetency, testutil.WithURLParams(newRequest(http.MethodGet, "/api/agents/"+agent+"/competency", nil), "id", agent)).Want(http.StatusOK).JSON(&out)
		return out
	}
	// Accepted: one success in the server domain, sample too small to be reliable.
	patch(issue, map[string]any{"status": "done"})
	c := get(agentA)
	if len(c.Rows) != 1 || c.Rows[0].DomainKey != "path:server" || c.Rows[0].SuccessCount != 1 || c.Rows[0].TotalCount != 1 || c.Rows[0].Reliable || c.MinSample != competencyDefaultMinSample {
		t.Fatalf("after acceptance = %+v", c)
	}
	// Reopened: the success is taken back, the attempt stays.
	patch(issue, map[string]any{"status": "in_progress"})
	if c = get(agentA); c.Rows[0].SuccessCount != 0 || c.Rows[0].TotalCount != 1 {
		t.Fatalf("after reopening = %+v", c.Rows[0])
	}
	// Cancelled: a plain attempt.
	patch(issue, map[string]any{"status": "cancelled"})
	if c = get(agentA); c.Rows[0].SuccessCount != 0 || c.Rows[0].TotalCount != 2 {
		t.Fatalf("after cancellation = %+v", c.Rows[0])
	}
	// Sent back from review: a rejected attempt, no success.
	patch(issue, map[string]any{"status": "in_review"})
	patch(issue, map[string]any{"status": "in_progress"})
	if c = get(agentA); c.Rows[0].SuccessCount != 0 || c.Rows[0].TotalCount != 3 {
		t.Fatalf("after review rejection = %+v", c.Rows[0])
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND details->>'event' = 'review_rejected' AND entity_id = $2`, AuditCompetency, agentA); n != 1 {
		t.Fatalf("review_rejected audit rows = %d", n)
	}
	// A member-assigned issue moves nothing.
	other := dbfx.Issue(t, "Member issue on server/x.go", testutil.Cols{"status": "in_progress", "assignee_type": "member", "assignee_id": testUserID})
	patch(other, map[string]any{"status": "done"})
	if c = get(agentA); c.Rows[0].TotalCount != 3 {
		t.Fatal("a member's issue must not move an agent's tally")
	}
	// A confirmed duel on a server issue: A wins, B loses, weighted twice.
	duelIssue := dbfx.Issue(t, "Duel on server/internal/service/task.go", testutil.Cols{"status": "in_progress"})
	var out struct{ Duel AgentDuelResponse }
	testutil.Call(t, testHandler.StartAgentDuel, testutil.WithURLParams(newRequest(http.MethodPost, "/api/issues/"+duelIssue+"/duel", map[string]any{"agent_a_id": agentA, "agent_b_id": agentB}), "id", duelIssue)).Want(http.StatusCreated).JSON(&out)
	prev := testHandler.LLM
	testHandler.LLM = nil
	for _, id := range []string{out.Duel.A.TaskID, out.Duel.B.TaskID} {
		dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, id)
		testHandler.updateDuelBarrier(context.Background(), mustTask(t, id))
	}
	testHandler.LLM = prev
	testutil.Call(t, testHandler.ConfirmAgentDuel, testutil.WithURLParams(newRequest(http.MethodPost, "/api/duels/"+out.Duel.ID+"/confirm", map[string]any{"winner": "a"}), "id", out.Duel.ID)).Want(http.StatusOK)
	c = get(agentA)
	if c.Rows[0].DuelWins != 1 || c.Rows[0].SampleSize != 4 || c.Rows[0].Score != competencyScore(0, 3, 1, 0) {
		t.Fatalf("after duel win = %+v", c.Rows[0])
	}
	if b := get(agentB); len(b.Rows) != 1 || b.Rows[0].DuelLosses != 1 || b.Rows[0].DomainKey != "path:server" {
		t.Fatalf("after duel loss = %+v", b.Rows)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE action = $1 AND details->>'event' = 'duel_won' AND entity_id = $2`, AuditCompetency, agentA); n != 1 {
		t.Fatalf("duel audit rows = %d", n)
	}
	// A lower threshold makes A reliable; the suggestion ranks it first and carries the domain.
	testutil.Call(t, testHandler.PutCompetencySettings, newRequest(http.MethodPut, "/api/competency-settings", map[string]any{"min_sample": 0})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutCompetencySettings, newRequest(http.MethodPut, "/api/competency-settings", map[string]any{"min_sample": 3})).Want(http.StatusOK)
	next := dbfx.Issue(t, "Refactor server/pkg/db/queries/issue.sql")
	var sug struct {
		DomainKey  string          `json:"domain_key"`
		MinSample  int             `json:"min_sample"`
		Candidates []CompetencyRow `json:"candidates"`
	}
	testutil.Call(t, testHandler.GetAssigneeSuggestion, testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+next+"/assignee-suggestion", nil), "id", next)).Want(http.StatusOK).JSON(&sug)
	if sug.DomainKey != "path:server" || sug.MinSample != 3 || len(sug.Candidates) < 2 || sug.Candidates[0].AgentID != agentA || !sug.Candidates[0].Reliable || sug.Candidates[0].AgentName == "" {
		t.Fatalf("suggestion = %+v", sug)
	}
	for _, cand := range sug.Candidates {
		if cand.AgentID == agentB && cand.Reliable {
			t.Fatalf("B has one sample and must be unreliable: %+v", cand)
		}
	}
	// Per workspace: the same agent id in another workspace has no history there.
	var count int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM agent_domain_competency WHERE agent_id = $1 AND workspace_id <> $2`, agentA, testWorkspaceID).Scan(&count)
	if count != 0 {
		t.Fatal("competency leaked across workspaces")
	}
}
