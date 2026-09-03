package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Decision Cards (K01): ask, answer once, hand the answer to a new run.

func askDecision(t *testing.T, issueID string, body map[string]any, headers ...string) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/decisions", body)
	if len(headers) > 0 {
		req = testutil.WithHeaders(req, headers...)
	}
	return testutil.Call(t, testHandler.AskIssueDecision, testutil.WithURLParams(req, "id", issueID))
}

func respondDecision(t *testing.T, issueID, decisionID string, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.RespondIssueDecision, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/issues/"+issueID+"/decisions/"+decisionID+"/respond", body),
		"id", issueID, "decisionId", decisionID,
	))
}

func decisionBody() map[string]any {
	return map[string]any{
		"question": "Drop the legacy table?",
		"options": []map[string]any{
			{"id": "drop", "label": "Drop it", "impact": "irreversible"},
			{"id": "keep", "label": "Keep it for now"},
		},
		"recommended_option_id": "keep",
		"urgency":               "high",
	}
}

type decisionEnvelope struct {
	Decision IssueDecisionResponse `json:"decision"`
}

func TestAskIssueDecisionValidates(t *testing.T) {
	issue := dbfx.Issue(t, "decision validation")
	bad := []map[string]any{
		{"question": "", "options": decisionBody()["options"]},
		{"question": "q", "options": []map[string]any{{"id": "a", "label": "A"}}},
		{"question": "q", "options": []map[string]any{{"id": "a", "label": "A"}, {"id": "a", "label": "B"}}},
		{"question": "q", "options": decisionBody()["options"], "recommended_option_id": "nope"},
		{"question": "q", "options": decisionBody()["options"], "urgency": "asap"},
	}
	for i, b := range bad {
		if resp := askDecision(t, issue, b); resp.Code != http.StatusBadRequest {
			t.Fatalf("case %d: status = %d, want 400 (%s)", i, resp.Code, resp.Text())
		}
	}
	foreign := dbfx.Workspace(t, "Decision foreign", "decision-foreign-"+uuid.NewString())
	foreignIssue := dbfx.Issue(t, "decision foreign issue", testutil.Cols{"workspace_id": foreign})
	askDecision(t, foreignIssue, decisionBody()).Want(http.StatusNotFound)
}

func TestIssueDecisionRoundTripQueuesResumeRun(t *testing.T) {
	issue, task := completedAgentRun(t, "decision round-trip")

	var created decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)
	d := created.Decision
	if d.Question != "Drop the legacy table?" || len(d.Options) != 2 || d.RecommendedOptionID != "keep" || d.Urgency != "high" || d.Response != nil {
		t.Fatalf("created = %+v, want the card as filed and pending", d)
	}
	if d.AskedByType != "member" {
		t.Fatalf("asked_by_type = %q, want member for a plain member request", d.AskedByType)
	}

	var listed struct {
		Decisions []IssueDecisionResponse `json:"decisions"`
	}
	testutil.Call(t, testHandler.ListIssueDecisions, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issue+"/decisions", nil), "id", issue,
	)).Want(http.StatusOK).JSON(&listed)
	if len(listed.Decisions) != 1 || listed.Decisions[0].ID != d.ID {
		t.Fatalf("listed = %+v, want the pending card", listed.Decisions)
	}

	before := countTasksOnIssue(t, issue)
	var answered decisionEnvelope
	respondDecision(t, issue, d.ID, map[string]any{"option_id": "drop"}).Want(http.StatusOK).JSON(&answered)
	a := answered.Decision
	if a.Response == nil || a.Response.OptionID != "drop" || a.RespondedAt == nil || a.RespondedByType != "member" {
		t.Fatalf("answered = %+v, want the recorded option", a)
	}
	if a.ResumeTaskID == "" || countTasksOnIssue(t, issue) != before+1 {
		t.Fatalf("answer must queue one resume run on an agent-assigned issue (resume=%q, tasks %d→%d)", a.ResumeTaskID, before, countTasksOnIssue(t, issue))
	}
	var note string
	dbfx.QueryRow(t, `SELECT handoff_note FROM agent_task_queue WHERE id = $1`, a.ResumeTaskID).Scan(&note)
	t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM agent_task_queue WHERE id = $1`, a.ResumeTaskID) })
	if !strings.Contains(note, "Drop the legacy table?") || !strings.Contains(note, `"drop"`) || !strings.Contains(note, "Drop it") {
		t.Fatalf("handoff note = %q, want the question and the chosen option", note)
	}
	_ = task

	// A second answer is refused without touching the record.
	resp := respondDecision(t, issue, d.ID, map[string]any{"option_id": "keep"}).Want(http.StatusConflict)
	if code, _ := resp.Map()["code"].(string); code != "already_decided" {
		t.Fatalf("409 code = %q, want already_decided", code)
	}
	if countTasksOnIssue(t, issue) != before+1 {
		t.Fatal("a refused answer must not queue another run")
	}
}

func TestIssueDecisionModifiedTextAndHumanAssignee(t *testing.T) {
	issue := dbfx.Issue(t, "decision human issue") // no agent assignee: no resume run
	var created decisionEnvelope
	askDecision(t, issue, decisionBody()).Want(http.StatusCreated).JSON(&created)

	respondDecision(t, issue, created.Decision.ID, map[string]any{"option_id": "drop", "modified_text": "both"}).Want(http.StatusBadRequest)
	respondDecision(t, issue, created.Decision.ID, map[string]any{}).Want(http.StatusBadRequest)
	respondDecision(t, issue, created.Decision.ID, map[string]any{"option_id": "unknown"}).Want(http.StatusBadRequest)

	var answered decisionEnvelope
	respondDecision(t, issue, created.Decision.ID, map[string]any{"modified_text": "Archive it instead"}).Want(http.StatusOK).JSON(&answered)
	if answered.Decision.Response == nil || answered.Decision.Response.ModifiedText != "Archive it instead" {
		t.Fatalf("answered = %+v, want the modified text", answered.Decision)
	}
	if answered.Decision.ResumeTaskID != "" || countTasksOnIssue(t, issue) != 0 {
		t.Fatal("a human-assigned issue must not queue a run")
	}
	respondDecision(t, issue, uuid.NewString(), map[string]any{"option_id": "drop"}).Want(http.StatusNotFound)
}

func TestAskIssueDecisionLinksTheAskingRun(t *testing.T) {
	issue, task := completedAgentRun(t, "decision run link")
	var agentID string
	dbfx.QueryRow(t, `SELECT agent_id FROM agent_task_queue WHERE id = $1`, task).Scan(&agentID)

	var created decisionEnvelope
	askDecision(t, issue, decisionBody(), "X-Agent-ID", agentID, "X-Task-ID", task).Want(http.StatusCreated).JSON(&created)
	if created.Decision.TaskID != task || created.Decision.AskedByType != "agent" || created.Decision.AskedByID != agentID {
		t.Fatalf("created = %+v, want the card attributed to the agent and linked to its run", created.Decision)
	}
}
