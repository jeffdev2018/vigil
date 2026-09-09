package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/goalstate"
)

type issueGoalOut struct {
	Goal *goalstate.State `json:"goal"`
}

func goalCall(t *testing.T, h http.HandlerFunc, method, issueID, sub string, body any) issueGoalOut {
	t.Helper()
	var out issueGoalOut
	testutil.Call(t, h, withURLParam(newRequest(method, "/api/issues/"+issueID+"/goal"+sub, body), "id", issueID)).Want(http.StatusOK).JSON(&out)
	return out
}

// A member writes the goal and the ceiling, pauses and resumes the chain;
// the row is created on first contact and read back the same way.
func TestIssueGoalWritePauseResume(t *testing.T) {
	agent := dbfx.Agent(t, "goal agent "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "goal issue "+uuid.NewString()[:8], testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})

	if out := goalCall(t, testHandler.GetIssueGoal, http.MethodGet, issue, "", nil); out.Goal != nil {
		t.Fatalf("a fresh issue has no goal, got %+v", out.Goal)
	}
	out := goalCall(t, testHandler.SetIssueGoal, http.MethodPut, issue, "", map[string]any{"goal": "Three words posted, in order.", "max_continuations": 3})
	if out.Goal == nil || out.Goal.Goal != "Three words posted, in order." || out.Goal.MaxContinuations != 3 || out.Goal.Status != "active" || out.Goal.SetByType != "member" {
		t.Fatalf("set goal = %+v", out.Goal)
	}
	if out := goalCall(t, testHandler.GetIssueGoal, http.MethodGet, issue, "", nil); out.Goal == nil || out.Goal.MaxContinuations != 3 || len(out.Goal.Evidence) != 0 {
		t.Fatalf("get goal = %+v", out.Goal)
	}
	testutil.Call(t, testHandler.SetIssueGoal, withURLParam(newRequest(http.MethodPut, "/api/issues/"+issue+"/goal", map[string]any{"goal": "x", "max_continuations": 99}), "id", issue)).Want(http.StatusBadRequest)

	if out := goalCall(t, testHandler.PauseIssueGoal, http.MethodPost, issue, "/pause", nil); out.Goal == nil || out.Goal.Status != "paused" {
		t.Fatalf("pause = %+v", out.Goal)
	}
	// Resume queues a run for the agent from the goal state.
	out = goalCall(t, testHandler.ResumeIssueGoal, http.MethodPost, issue, "/resume", nil)
	if out.Goal == nil || out.Goal.Status != "active" || out.Goal.Continuation != 0 {
		t.Fatalf("resume = %+v", out.Goal)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued' AND leg_role = 'continuation'", issue); n != 1 {
		t.Fatalf("queued continuations after resume = %d, want 1", n)
	}
}

// Answering with nothing waiting is a conflict; a run asks through its
// machine credential, a member answers, the follow-up run is queued.
func TestIssueGoalQuestionAndAnswer(t *testing.T) {
	agent := dbfx.Agent(t, "goal agent "+uuid.NewString()[:8], handlerTestRuntimeID(t))
	issue := dbfx.Issue(t, "goal issue "+uuid.NewString()[:8], testutil.Cols{"status": "in_progress", "assignee_type": "agent", "assignee_id": agent})
	task := dbfx.Task(t, agent, testutil.Cols{"issue_id": issue, "runtime_id": handlerTestRuntimeID(t), "status": "running"})

	testutil.Call(t, testHandler.AnswerIssueGoal, withURLParam(newRequest(http.MethodPost, "/api/issues/"+issue+"/goal/answer", map[string]any{"answer": "Postgres"}), "id", issue)).Want(http.StatusConflict)

	// The run asks: a machine credential without X-Task-ID is refused; with
	// it, the question lands on the goal row.
	bare := newRequest(http.MethodPost, "/api/issues/"+issue+"/goal/question", map[string]any{"question": "Which database?"})
	bare.Header.Set("X-Actor-Source", "task_token")
	bare.Header.Set("X-Agent-ID", agent)
	testutil.Call(t, testHandler.AskIssueGoalQuestion, withURLParam(bare, "id", issue)).Want(http.StatusForbidden)

	var out issueGoalOut
	testutil.Call(t, testHandler.AskIssueGoalQuestion, withURLParam(runRequest(agent, task, http.MethodPost, "/api/issues/"+issue+"/goal/question", map[string]any{"question": "Which database?", "kind": "choice", "options": []string{"Postgres", "SQLite"}}), "id", issue)).Want(http.StatusOK).JSON(&out)
	if out.Goal == nil || out.Goal.Status != "waiting_user" || out.Goal.Question == nil || out.Goal.Question.Kind != "choice" || out.Goal.Question.RunID != task {
		t.Fatalf("question = %+v", out.Goal)
	}
	// A bad kind is refused.
	testutil.Call(t, testHandler.AskIssueGoalQuestion, withURLParam(runRequest(agent, task, http.MethodPost, "/api/issues/"+issue+"/goal/question", map[string]any{"question": "?", "kind": "form"}), "id", issue)).Want(http.StatusBadRequest)

	out = goalCall(t, testHandler.AnswerIssueGoal, http.MethodPost, issue, "/answer", map[string]any{"answer": "2"})
	if out.Goal == nil || out.Goal.Status != "active" || out.Goal.Question == nil || out.Goal.Question.Answer != "SQLite" {
		t.Fatalf("answer = %+v", out.Goal)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM comment WHERE issue_id = $1 AND author_type = 'member' AND content LIKE '%SQLite%'", issue); n != 1 {
		t.Fatalf("answer comments = %d, want 1", n)
	}
	if n := dbfx.Count(t, "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued' AND handoff_note LIKE '%SQLite%'", issue); n != 1 {
		t.Fatalf("follow-up runs after answer = %d, want 1", n)
	}
}
