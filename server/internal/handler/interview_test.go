package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Requirement Interview (K13): questions asked together park the issue and
// resume it as one, once every answer is in.

func askInterview(t *testing.T, issueID string, questions []map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/interview", map[string]any{"questions": questions})
	return testutil.Call(t, testHandler.AskRequirementInterview, testutil.WithURLParams(req, "id", issueID))
}

func interviewQuestion(text string) map[string]any {
	return map[string]any{
		"question": text,
		"options":  []map[string]any{{"id": "a", "label": "Option A"}, {"id": "b", "label": "Option B"}},
	}
}

func issueStatusOf(t *testing.T, issueID string) string {
	t.Helper()
	var status string
	dbfx.QueryRow(t, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status)
	return status
}

func cleanupInterview(t *testing.T, issueID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := t.Context()
		testPool.Exec(ctx, `DELETE FROM issue_decision WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
	})
}

func TestRequirementInterviewParksAndResumesAsOne(t *testing.T) {
	seedTestCatalog(t)
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, interviewStatusKey)
	})
	issue, _ := completedAgentRun(t, "interview")
	cleanupInterview(t, issue)
	runsBefore := countTasksOnIssue(t, issue)

	var out struct {
		Decisions []IssueDecisionResponse `json:"decisions"`
		Status    string                  `json:"status"`
	}
	askInterview(t, issue, []map[string]any{interviewQuestion("Which format?"), interviewQuestion("Include archived?"), interviewQuestion("Who reviews?")}).Want(http.StatusCreated).JSON(&out)
	if len(out.Decisions) != 3 || out.Status != interviewStatusKey {
		t.Fatalf("interview = %+v", out)
	}
	group := out.Decisions[0].InterviewGroupID
	for i, d := range out.Decisions {
		if d.InterviewGroupID != group || d.InterviewPosition != int32(i+1) {
			t.Fatalf("question %d = %+v, want group %s position %d", i, d, group, i+1)
		}
	}
	if got := issueStatusOf(t, issue); got != interviewStatusKey {
		t.Fatalf("issue status = %q, want parked", got)
	}
	var category string
	dbfx.QueryRow(t, `SELECT category FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, interviewStatusKey).Scan(&category)
	if category != "blocked" {
		t.Fatalf("Waiting for PM category = %q, want blocked", category)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE issue_id = $1 AND type = 'decision_request'`, issue); n != 3 {
		t.Fatalf("inbox items = %d, want one per question", n)
	}
	// A second interview while this one is pending is refused.
	askInterview(t, issue, []map[string]any{interviewQuestion("Again?")}).Want(http.StatusConflict)

	// Two answers: still parked, no run.
	respondDecision(t, issue, out.Decisions[0].ID, map[string]any{"option_id": "b"}).Want(http.StatusOK)
	respondDecision(t, issue, out.Decisions[2].ID, map[string]any{"modified_text": "The tech lead"}).Want(http.StatusOK)
	if got := issueStatusOf(t, issue); got != interviewStatusKey {
		t.Fatalf("status after partial answers = %q, want still parked", got)
	}
	if n := countTasksOnIssue(t, issue); n != runsBefore {
		t.Fatalf("runs after partial answers = %d, want %d", n, runsBefore)
	}

// The last answer restores the status and queues one run with every
	// answer in order.
	var last decisionEnvelope
	respondDecision(t, issue, out.Decisions[1].ID, map[string]any{"option_id": "a"}).Want(http.StatusOK).JSON(&last)
	if got := issueStatusOf(t, issue); got != "in_progress" {
		t.Fatalf("status after the last answer = %q, want in_progress restored", got)
	}
	if n := countTasksOnIssue(t, issue); n != runsBefore+1 {
		t.Fatalf("runs after the last answer = %d, want %d", n, runsBefore+1)
	}
	if last.Decision.ResumeTaskID == "" {
		t.Fatal("the last answer must link the resume run")
	}
	var note string
	dbfx.QueryRow(t, `SELECT handoff_note FROM agent_task_queue WHERE id = $1`, last.Decision.ResumeTaskID).Scan(&note)
	for _, want := range []string{"Requirement interview answered:", "1. Which format? — Option B (b)", "2. Include archived? — Option A (a)", "3. Who reviews? — The tech lead"} {
		if !strings.Contains(note, want) {
			t.Fatalf("handoff note = %q, want it to contain %q", note, want)
		}
	}
	if strings.Index(note, "1. Which") > strings.Index(note, "2. Include") {
		t.Fatalf("answers out of order: %q", note)
	}
}

func TestRequirementInterviewValidatesAndKeepsHumanIssuesParked(t *testing.T) {
	seedTestCatalog(t)
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, interviewStatusKey)
	})
	issue := dbfx.Issue(t, "interview validation", testutil.Cols{"status": "todo"})
	cleanupInterview(t, issue)
	askInterview(t, issue, nil).Want(http.StatusBadRequest)
	askInterview(t, issue, []map[string]any{interviewQuestion("1"), interviewQuestion("2"), interviewQuestion("3"), interviewQuestion("4")}).Want(http.StatusBadRequest)
	askInterview(t, issue, []map[string]any{{"question": "No options"}}).Want(http.StatusBadRequest)

	// A human-assigned issue parks and returns, without any run.
	var out struct {
		Decisions []IssueDecisionResponse `json:"decisions"`
	}
	askInterview(t, issue, []map[string]any{interviewQuestion("Only one?")}).Want(http.StatusCreated).JSON(&out)
	if got := issueStatusOf(t, issue); got != interviewStatusKey {
		t.Fatalf("status = %q, want parked", got)
	}
	respondDecision(t, issue, out.Decisions[0].ID, map[string]any{"option_id": "a"}).Want(http.StatusOK)
	if got := issueStatusOf(t, issue); got != "todo" {
		t.Fatalf("status after answer = %q, want todo restored", got)
	}
	if n := countTasksOnIssue(t, issue); n != 0 {
		t.Fatalf("runs on a human issue = %d, want 0", n)
	}
}

// TestInterviewAnswersMergeIntoPendingRun is the JEF-241 regression: when the
// issue's agent already occupies its pending slot (the assignment enqueue),
// the interview resume cannot create a second task (unique index) — the
// answers must merge into the waiting run's handoff note instead of being
// dropped with a logged warning.
func TestInterviewAnswersMergeIntoPendingRun(t *testing.T) {
	seedTestCatalog(t)
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM issue_status WHERE workspace_id = $1 AND key = $2`, testWorkspaceID, interviewStatusKey)
	})
	agentID := dbfx.Agent(t, "interview-merge agent", handlerTestRuntimeID(t), testutil.Cols{
		"instructions": "",
		"custom_env":   testutil.Raw("'{}'::jsonb"),
		"custom_args":  testutil.Raw("'[]'::jsonb"),
	})
	issue := dbfx.Issue(t, "interview-merge issue", testutil.Cols{
		"status":        "todo",
		"assignee_type": "agent",
		"assignee_id":   agentID,
	})
	pendingTask := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t),
		"issue_id":   issue,
		"status":     "queued",
	})
	cleanupInterview(t, issue)

	var out struct {
		Decisions []IssueDecisionResponse `json:"decisions"`
	}
	askInterview(t, issue, []map[string]any{interviewQuestion("Which cache?")}).Want(http.StatusCreated).JSON(&out)

	var last decisionEnvelope
	respondDecision(t, issue, out.Decisions[0].ID, map[string]any{"option_id": "a"}).Want(http.StatusOK).JSON(&last)

	// No second run: the pending slot is still held by the same task, and it
	// is the one the decision links as the resume run.
	if n := countTasksOnIssue(t, issue); n != 1 {
		t.Fatalf("runs after the answer = %d, want the single merged task", n)
	}
	if last.Decision.ResumeTaskID != pendingTask {
		t.Fatalf("resume task = %q, want the pending task %q", last.Decision.ResumeTaskID, pendingTask)
	}
	var note string
	dbfx.QueryRow(t, `SELECT handoff_note FROM agent_task_queue WHERE id = $1`, pendingTask).Scan(&note)
	if !strings.Contains(note, "Requirement interview answered:") || !strings.Contains(note, "Which cache?") {
		t.Fatalf("merged handoff note = %q, want the interview answers", note)
	}
	if got := issueStatusOf(t, issue); got != "todo" {
		t.Fatalf("status after the answer = %q, want todo restored", got)
	}
}
