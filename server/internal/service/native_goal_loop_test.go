package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	openai "github.com/openai/openai-go/v3"
)

// The judge's reply is parsed leniently on shape and strictly on vocabulary.
func TestParseNativeGoalVerdict(t *testing.T) {
	v, err := parseNativeGoalVerdict("Sure:\n```json\n{\"satisfied\": false, \"blocker\": \"goal_not_met_yet\", \"reason\": \"tests missing\"}\n```")
	if err != nil || v.Satisfied || v.Blocker != "goal_not_met_yet" || v.Reason != "tests missing" {
		t.Fatalf("fenced verdict = %+v, %v", v, err)
	}
	v, err = parseNativeGoalVerdict(`{"satisfied": true, "blocker": "whatever", "reason": "all done"}`)
	if err != nil || !v.Satisfied || v.Blocker != "" {
		t.Fatalf("satisfied verdict should drop the blocker: %+v, %v", v, err)
	}
	if _, err := parseNativeGoalVerdict(`{"satisfied": false, "blocker": "tired"}`); err == nil {
		t.Fatal("an unknown blocker must be refused")
	}
	if _, err := parseNativeGoalVerdict("I think it is fine."); err == nil {
		t.Fatal("prose without JSON must be refused")
	}
	if nativeGoalSignature("Done:  A, B") != nativeGoalSignature("done: a, b") {
		t.Fatal("signature must ignore case and whitespace")
	}
}

func TestNativeGoalLoopFromSettings(t *testing.T) {
	if got := NativeGoalLoopFromSettings(nil).MaxContinuations; got != nativeGoalDefaultMaxContinuations {
		t.Fatalf("default = %d", got)
	}
	if got := NativeGoalLoopFromSettings([]byte(`{"native_goal_loop":{"max_continuations":0}}`)).MaxContinuations; got != 0 {
		t.Fatalf("zero must disable, got %d", got)
	}
	if got := NativeGoalLoopFromSettings([]byte(`{"native_goal_loop":{"max_continuations":999}}`)).MaxContinuations; got != nativeGoalMaxMaxContinuations {
		t.Fatalf("out of range must clamp, got %d", got)
	}
}

type nativeGoalFixture struct {
	pool                                     *pgxpool.Pool
	fx                                       *testutil.Fixture
	workspaceID, runtimeID, agentID, issueID string
}

func newNativeGoalFixture(t *testing.T) nativeGoalFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("goal-owner-%d", suffix), fmt.Sprintf("goal-owner-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("goal-ws-%d", suffix), fmt.Sprintf("goal-ws-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{
		"runtime_mode": "native",
		"daemon_id":    "native",
		"provider":     "native",
	})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	issueID := fx.Issue(t, "Ship the goal loop", testutil.Cols{
		"description":   "Judge, continuation, state.",
		"assignee_type": "agent",
		"assignee_id":   agentID,
		"status":        "in_progress",
	})
	return nativeGoalFixture{pool: pool, fx: fx, workspaceID: ws, runtimeID: runtimeID, agentID: agentID, issueID: issueID}
}

// run claims the task and drives one native run through the scripted model.
func (f nativeGoalFixture) run(t *testing.T, taskID string, turns ...openai.ChatCompletion) *scriptedNativeLLM {
	t.Helper()
	ctx := context.Background()
	llm := &scriptedNativeLLM{turns: turns}
	tasks := NewTaskService(db.New(f.pool), f.pool, nil, events.New())
	issues := NewIssueService(db.New(f.pool), f.pool, events.New(), nil, tasks)
	svc := NewNativeAgentService(db.New(f.pool), tasks, issues, llm, events.New())
	claimed, err := tasks.claimTask(ctx, util.MustParseUUID(f.agentID), util.MustParseUUID(f.runtimeID), false)
	if err != nil || claimed == nil || util.UUIDToString(claimed.ID) != taskID {
		t.Fatalf("claim task %s: got %v (%v)", taskID, claimed, err)
	}
	svc.runTask(ctx, *claimed)
	return llm
}

func (f nativeGoalFixture) pendingNotes(t *testing.T) []string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT COALESCE(handoff_note, '') FROM agent_task_queue WHERE issue_id = $1 AND status = 'queued'`, f.issueID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	defer rows.Close()
	var notes []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		notes = append(notes, n)
	}
	return notes
}

func (f nativeGoalFixture) outcome(t *testing.T, taskID string) string {
	t.Helper()
	var out string
	if err := f.pool.QueryRow(context.Background(), `SELECT COALESCE(result->'goal_loop'->>'outcome', '') FROM agent_task_queue WHERE id = $1`, taskID).Scan(&out); err != nil {
		t.Fatalf("read outcome: %v", err)
	}
	return out
}

func (f nativeGoalFixture) agentComments(t *testing.T) []string {
	t.Helper()
	rows, err := f.pool.Query(context.Background(), `SELECT content FROM comment WHERE issue_id = $1 AND author_id = $2 ORDER BY created_at`, f.issueID, f.agentID)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, c)
	}
	return out
}

func nativeJudgeTurn(satisfied bool, blocker, reason string) openai.ChatCompletion {
	return nativeTextTurn(fmt.Sprintf(`{"satisfied": %t, "blocker": %q, "reason": %q}`, satisfied, blocker, reason))
}

// A run whose status does not meet the goal queues the next run with a
// handoff note; the judge is offered no tools and sees the goal and status.
func TestNativeGoalLoopContinuesWhenGoalNotMet(t *testing.T) {
	f := newNativeGoalFixture(t)
	taskID := f.fx.Task(t, f.agentID, testutil.Cols{"issue_id": f.issueID, "runtime_id": f.runtimeID})

	llm := f.run(t, taskID,
		nativeTextTurn("Wrote the judge. Continuation and state remain."),
		nativeJudgeTurn(false, "goal_not_met_yet", "two of three parts remain"),
	)
	if llm.calls != 2 {
		t.Fatalf("model calls = %d, want run + judge", llm.calls)
	}
	if len(llm.last.Tools) != 0 {
		t.Fatal("the judge must not be offered tools")
	}
	judgePrompt := nativeLastUserMessage(t, llm.last)
	if !strings.Contains(judgePrompt, "Ship the goal loop") || !strings.Contains(judgePrompt, "Wrote the judge") {
		t.Fatalf("judge prompt lacks goal or status: %q", judgePrompt)
	}
	if got := f.outcome(t, taskID); got != "continued" {
		t.Fatalf("outcome = %q, want continued", got)
	}
	notes := f.pendingNotes(t)
	if len(notes) != 1 || !strings.HasPrefix(notes[0], "Continuation 1/8") || !strings.Contains(notes[0], "two of three parts remain") {
		t.Fatalf("pending continuation = %q", notes)
	}
	if c := f.agentComments(t); len(c) != 0 {
		t.Fatalf("a continued loop must not comment, got %q", c)
	}
	var system int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM task_message WHERE task_id = $1 AND type = 'system' AND content LIKE 'Goal check: continued%'`, taskID).Scan(&system); err != nil || system != 1 {
		t.Fatalf("transcript goal-check line = %d (%v), want 1", system, err)
	}
}

// Satisfied stops quietly; a blocker that needs a human stops with a comment.
func TestNativeGoalLoopStopsOnSatisfiedOrHumanBlocker(t *testing.T) {
	f := newNativeGoalFixture(t)
	first := f.fx.Task(t, f.agentID, testutil.Cols{"issue_id": f.issueID, "runtime_id": f.runtimeID})
	f.run(t, first, nativeTextTurn("All three parts shipped and tested."), nativeJudgeTurn(true, "", "everything is there"))
	if got := f.outcome(t, first); got != "satisfied" {
		t.Fatalf("outcome = %q, want satisfied", got)
	}
	if n := f.pendingNotes(t); len(n) != 0 {
		t.Fatalf("satisfied must not queue a run, got %q", n)
	}

	second := f.fx.Task(t, f.agentID, testutil.Cols{"issue_id": f.issueID, "runtime_id": f.runtimeID})
	f.run(t, second, nativeTextTurn("Which database should this use?"), nativeJudgeTurn(false, "needs_user_input", "the run asks which database to use"))
	if got := f.outcome(t, second); got != "stopped:needs_user_input" {
		t.Fatalf("outcome = %q, want stopped:needs_user_input", got)
	}
	if n := f.pendingNotes(t); len(n) != 0 {
		t.Fatalf("a question must not queue a run, got %q", n)
	}
	comments := f.agentComments(t)
	if len(comments) != 1 || !strings.Contains(comments[0], "needs_user_input") || !strings.Contains(comments[0], "which database") {
		t.Fatalf("stop comment = %q", comments)
	}
}

// The chain is read off the predecessor's result: the eighth continuation
// is the last, and two identical statuses in a row stop the loop early.
func TestNativeGoalLoopBoundsContinuations(t *testing.T) {
	prior := func(f nativeGoalFixture, state string) {
		f.fx.Task(t, f.agentID, testutil.Cols{
			"issue_id":     f.issueID,
			"runtime_id":   f.runtimeID,
			"status":       "completed",
			"completed_at": testutil.Raw("now() - interval '1 minute'"),
			"result":       testutil.Raw(`'{"summary":"earlier","goal_loop":` + state + `}'::jsonb`),
		})
	}

	// Exhausted: predecessor was continuation 7, this run is the 8th.
	f := newNativeGoalFixture(t)
	prior(f, `{"continuation":7,"signature":"x","no_progress":0,"outcome":"continued"}`)
	eighth := f.fx.Task(t, f.agentID, testutil.Cols{"issue_id": f.issueID, "runtime_id": f.runtimeID, "handoff_note": "Continuation 8/8 on this issue's goal."})
	f.run(t, eighth, nativeTextTurn("Still one part left."), nativeJudgeTurn(false, "goal_not_met_yet", "one part left"))
	if got := f.outcome(t, eighth); got != "stopped:exhausted" {
		t.Fatalf("outcome = %q, want stopped:exhausted", got)
	}
	if n := f.pendingNotes(t); len(n) != 0 {
		t.Fatalf("exhausted must not queue, got %q", n)
	}

	if c := f.agentComments(t); len(c) != 1 || !strings.Contains(c[0], "exhausted") {
		t.Fatalf("stop comment = %q", c)
	}

	// Stagnation: the same status as the predecessor, twice in a row. Its
	// own issue, so the exhausted run above is not its predecessor.
	f = newNativeGoalFixture(t)
	status := "Blocked on the same thing."
	prior(f, `{"continuation":1,"signature":"`+nativeGoalSignature(status)+`","no_progress":1,"outcome":"continued"}`)
	stuck := f.fx.Task(t, f.agentID, testutil.Cols{"issue_id": f.issueID, "runtime_id": f.runtimeID, "handoff_note": "Continuation 2/8 on this issue's goal."})
	f.run(t, stuck, nativeTextTurn(status), nativeJudgeTurn(false, "goal_not_met_yet", "no progress"))
	if got := f.outcome(t, stuck); got != "stopped:stagnation" {
		t.Fatalf("outcome = %q, want stopped:stagnation", got)
	}
	if n := f.pendingNotes(t); len(n) != 0 {
		t.Fatalf("stagnation must not queue, got %q", n)
	}
	if c := f.agentComments(t); len(c) != 1 || !strings.Contains(c[0], "stagnation") {
		t.Fatalf("stop comment = %q", c)
	}
}

// A workspace that sets max_continuations to zero gets no judge at all, and a
// judge that answers garbage stops the loop instead of looping on it.
func TestNativeGoalLoopDisabledAndUnparseableJudge(t *testing.T) {
	f := newNativeGoalFixture(t)
	ctx := context.Background()
	if _, err := f.pool.Exec(ctx, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"native_goal_loop":{"max_continuations":0}}'::jsonb WHERE id = $1`, f.workspaceID); err != nil {
		t.Fatalf("disable loop: %v", err)
	}
	off := f.fx.Task(t, f.agentID, testutil.Cols{"issue_id": f.issueID, "runtime_id": f.runtimeID})
	llm := f.run(t, off, nativeTextTurn("Half done."))
	if llm.calls != 1 {
		t.Fatalf("model calls = %d, want the run only (loop disabled)", llm.calls)
	}
	if got := f.outcome(t, off); got != "" {
		t.Fatalf("disabled loop must leave no state, got %q", got)
	}

	if _, err := f.pool.Exec(ctx, `UPDATE workspace SET settings = settings - 'native_goal_loop' WHERE id = $1`, f.workspaceID); err != nil {
		t.Fatalf("enable loop: %v", err)
	}
	garbage := f.fx.Task(t, f.agentID, testutil.Cols{"issue_id": f.issueID, "runtime_id": f.runtimeID})
	f.run(t, garbage, nativeTextTurn("Half done."), nativeTextTurn("Looks fine to me!"))
	if got := f.outcome(t, garbage); got != "stopped:judge_unavailable" {
		t.Fatalf("outcome = %q, want stopped:judge_unavailable", got)
	}
	if n := f.pendingNotes(t); len(n) != 0 {
		t.Fatalf("an unparseable judge must not queue, got %q", n)
	}
	var status string
	if err := f.pool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, garbage).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("task status = %q (%v), want completed: the judge never fails the run", status, err)
	}
}
